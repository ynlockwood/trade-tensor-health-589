package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type keyAuth struct {
	mu         sync.RWMutex
	namesByKey map[string]string
}

func newKeyAuth(tokens []ClientToken) *keyAuth {
	return &keyAuth{namesByKey: makeNamesByKey(tokens)}
}

func makeNamesByKey(tokens []ClientToken) map[string]string {
	namesByKey := make(map[string]string, len(tokens))
	for _, token := range tokens {
		namesByKey[token.APIKey] = token.Name
	}
	return namesByKey
}

func (a *keyAuth) Replace(tokens []ClientToken) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.namesByKey = makeNamesByKey(tokens)
}

func (a *keyAuth) authenticate(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	a.mu.RLock()
	defer a.mu.RUnlock()
	name, ok := a.namesByKey[key]
	return name, ok
}

type channelPicker struct {
	mu             sync.Mutex
	state          *channelStateStore
	staticChannels []Channel
	current        map[string]float64
}

func newChannelPicker(channels []Channel, state *channelStateStore) (*channelPicker, error) {
	p := &channelPicker{
		state:          state,
		staticChannels: append([]Channel(nil), channels...),
		current:        make(map[string]float64, len(channels)),
	}
	for _, ch := range channels {
		if ch.Weight < 0 {
			return nil, fmt.Errorf("channel %q has invalid weight %d", ch.Name, ch.Weight)
		}
	}
	if len(p.activeChannels()) == 0 {
		return nil, errors.New("no selectable channels with weight > 0")
	}
	return p, nil
}

func (p *channelPicker) Pick() Channel {
	p.mu.Lock()
	defer p.mu.Unlock()

	channels := p.activeChannels()
	if len(channels) == 0 {
		return Channel{}
	}

	totalEffective := 0.0
	bestIndex := 0
	bestCurrent := math.Inf(-1)
	activeNames := make(map[string]struct{}, len(channels))
	for i, ch := range channels {
		activeNames[ch.Name] = struct{}{}
		effective := effectiveChannelWeight(ch)
		totalEffective += effective
		next := p.current[ch.Name] + effective
		p.current[ch.Name] = next
		if next > bestCurrent {
			bestCurrent = next
			bestIndex = i
		}
	}
	for name := range p.current {
		if _, ok := activeNames[name]; !ok {
			delete(p.current, name)
		}
	}
	selected := channels[bestIndex]
	p.current[selected.Name] -= totalEffective
	return selected
}

func (p *channelPicker) activeChannels() []Channel {
	channels := p.staticChannels
	if p.state != nil {
		channels = p.state.Snapshot()
	}
	out := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.Weight > 0 {
			out = append(out, ch)
		}
	}
	return out
}

func effectiveChannelWeight(ch Channel) float64 {
	if ch.Weight <= 0 {
		return 0
	}
	penalty := 1.0
	if ch.ErrorCount > 0 {
		penalty += math.Log2(float64(ch.ErrorCount) + 1)
	}
	return float64(ch.Weight) / penalty
}

type proxyServer struct {
	auth         *keyAuth
	picker       *channelPicker
	channelState *channelStateStore
	logger       *slog.Logger
	transport    http.RoundTripper
}

func newProxyServer(tokens []ClientToken, channels []Channel, state *channelStateStore, logger *slog.Logger) (*proxyServer, error) {
	picker, err := newChannelPicker(channels, state)
	if err != nil {
		return nil, err
	}
	server := &proxyServer{
		auth:         newKeyAuth(tokens),
		picker:       picker,
		channelState: state,
		logger:       logger,
		transport:    proxyTransport(),
	}
	return server, nil
}

func proxyTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 0,
	}
}

func (s *proxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	clientName, ok := s.auth.authenticate(r)
	if !ok {
		s.logger.Warn("客户端鉴权失败",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"remote", r.RemoteAddr,
		)
		writeJSONError(w, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	channel := s.picker.Pick()
	if channel.Name == "" {
		s.logger.Error("没有可用渠道",
			"client", clientName,
		)
		writeJSONError(w, http.StatusServiceUnavailable, "upstream_error", "No selectable upstream channel")
		return
	}
	target, err := buildTargetURL(channel.BaseURL, r.URL)
	if err != nil {
		s.logger.Error("渠道地址无效",
			"client", clientName,
			"channel", channel.Name,
			"base_url", channel.BaseURL,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "upstream_error", "Invalid upstream channel")
		return
	}
	s.logger.Info("请求开始",
		"event", "request_start",
		"client", clientName,
		"method", r.Method,
		"path", r.URL.RequestURI(),
		"channel", channel.Name,
		"upstream", redactURL(target.String()),
		"remote", r.RemoteAddr,
		"upgrade", isWebSocketUpgrade(r),
	)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = target.Path
			pr.Out.URL.RawPath = target.RawPath
			pr.Out.URL.RawQuery = target.RawQuery
			pr.Out.Host = target.Host
			pr.Out.Header.Set("Authorization", "Bearer "+channel.APIKey)
			pr.Out.Header.Del("Proxy-Connection")
		},
		Transport:     s.transport,
		FlushInterval: -1,
		BufferPool:    &bufferPool{},
		ModifyResponse: func(resp *http.Response) error {
			if err := s.channelState.RecordResult(channel.Name, resp.StatusCode, nil); err != nil {
				s.logger.Warn("保存渠道状态失败",
					"channel", channel.Name,
					"status", resp.StatusCode,
					"error", err,
				)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if persistErr := s.channelState.RecordResult(channel.Name, http.StatusBadGateway, err); persistErr != nil {
				s.logger.Warn("保存渠道状态失败",
					"channel", channel.Name,
					"status", http.StatusBadGateway,
					"error", persistErr,
				)
			}
			s.logger.Error("连接上游失败",
				"event", "upstream_network_error",
				"client", clientName,
				"channel", channel.Name,
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "upstream_error", "Failed to reach upstream service")
		},
	}
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusSwitchingProtocols}
	proxy.ServeHTTP(rec, r)
	s.logger.Info("请求完成",
		"event", "request_complete",
		"client", clientName,
		"method", r.Method,
		"path", r.URL.RequestURI(),
		"channel", channel.Name,
		"status", rec.status,
		"duration", time.Since(start),
	)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerContainsToken(r.Header.Get("Connection"), "upgrade")
}

func headerContainsToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func buildTargetURL(baseURL string, incoming *url.URL) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("baseURL must include scheme and host: %s", baseURL)
	}
	target := *base
	suffix := incoming.EscapedPath()
	switch {
	case suffix == "":
		suffix = "/"
	case suffix == "/v1":
		suffix = ""
	case strings.HasPrefix(suffix, "/v1/"):
		suffix = strings.TrimPrefix(suffix, "/v1")
	}
	target.Path = joinURLPath(base.EscapedPath(), suffix)
	target.RawPath = ""
	target.RawQuery = incoming.RawQuery
	return &target, nil
}

func joinURLPath(basePath, suffix string) string {
	basePath = strings.TrimRight(basePath, "/")
	suffix = "/" + strings.TrimLeft(suffix, "/")
	if basePath == "" {
		return suffix
	}
	if suffix == "/" {
		return basePath
	}
	return basePath + suffix
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, fmt.Sprintf(`{"error":{"type":"%s","code":"%s","message":"%s"}}`, code, code, message))
}

func redactURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		u.User = nil
		return u.String()
	}
	return raw
}

type bufferPool struct {
	pool sync.Pool
}

func (p *bufferPool) Get() []byte {
	if b, ok := p.pool.Get().([]byte); ok {
		return b
	}
	return make([]byte, 32*1024)
}

func (p *bufferPool) Put(b []byte) {
	p.pool.Put(b)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}
