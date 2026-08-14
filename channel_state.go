package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type channelStateStore struct {
	mu     sync.Mutex
	path   string
	config *ChannelConfig
}

func newChannelStateStore(path string, cfg *ChannelConfig) *channelStateStore {
	return &channelStateStore{
		path:   path,
		config: cfg,
	}
}

func (s *channelStateStore) Snapshot() []Channel {
	if s == nil || s.config == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return snapshotChannels(s.config)
}

func (s *channelStateStore) ReplaceConfig(cfg *ChannelConfig) {
	if s == nil || cfg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

func (s *channelStateStore) ProbeCandidates() []Channel {
	if s == nil || s.config == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Channel
	for _, ch := range s.config.Channels {
		if ch.Weight > 0 && ch.ErrorCount > 0 {
			out = append(out, ch)
		}
	}
	return out
}

func (s *channelStateStore) RecordResult(channelName string, status int, err error) error {
	if s == nil || s.config == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for i := range s.config.Channels {
		ch := &s.config.Channels[i]
		if ch.Name != channelName {
			continue
		}
		nextErrorCount := ch.ErrorCount
		nextAuthErrorCount := ch.AuthErrorCount
		switch classifyChannelResult(status, err) {
		case channelResultSuccess:
			nextErrorCount = 0
			nextAuthErrorCount = 0
		case channelResultTemporaryError:
			nextErrorCount++
		case channelResultAuthError:
			nextAuthErrorCount++
		case channelResultIgnored:
		}
		if nextErrorCount != ch.ErrorCount {
			ch.ErrorCount = nextErrorCount
			changed = true
		}
		if nextAuthErrorCount != ch.AuthErrorCount {
			ch.AuthErrorCount = nextAuthErrorCount
			changed = true
		}
		break
	}
	if !changed {
		return nil
	}
	if writeErr := writeChannelConfigFile(s.path, s.config); writeErr != nil {
		return writeErr
	}
	return nil
}

func snapshotChannels(cfg *ChannelConfig) []Channel {
	if cfg == nil {
		return nil
	}
	out := make([]Channel, len(cfg.Channels))
	copy(out, cfg.Channels)
	return out
}

func writeChannelConfigFile(path string, cfg *ChannelConfig) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal channel config: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp channel config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp channel config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp channel config: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old channel config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace channel config: %w", err)
	}
	return nil
}

type channelResultClass int

const (
	channelResultIgnored channelResultClass = iota
	channelResultSuccess
	channelResultTemporaryError
	channelResultAuthError
)

func classifyChannelResult(status int, err error) channelResultClass {
	if err != nil {
		return channelResultTemporaryError
	}
	if status >= 200 && status < 400 {
		return channelResultSuccess
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return channelResultAuthError
	case http.StatusTooManyRequests, 529:
		return channelResultTemporaryError
	}
	if status >= 500 {
		return channelResultTemporaryError
	}
	return channelResultIgnored
}
