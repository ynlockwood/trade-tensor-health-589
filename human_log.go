package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type humanLogHandler struct {
	mu    sync.Mutex
	out   io.Writer
	level slog.Level
	attrs []slog.Attr
}

func newHumanLogHandler(out io.Writer, level slog.Level) *humanLogHandler {
	return &humanLogHandler{out: out, level: level}
}

type multiLogHandler struct {
	handlers []slog.Handler
}

func newMultiLogHandler(handlers ...slog.Handler) *multiLogHandler {
	return &multiLogHandler{handlers: handlers}
}

func (h *multiLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiLogHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, r.Level) {
			continue
		}
		if err := handler.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *multiLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithAttrs(attrs))
	}
	return &multiLogHandler{handlers: next}
}

func (h *multiLogHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithGroup(name))
	}
	return &multiLogHandler{handlers: next}
}

type auditLogHandler struct {
	base *humanLogHandler
}

func newAuditLogHandler(out io.Writer, level slog.Level) *auditLogHandler {
	return &auditLogHandler{base: newHumanLogHandler(out, level)}
}

func (h *auditLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *auditLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if !recordHasAttr(r, "event", "request_complete") {
		return nil
	}
	return h.base.Handle(ctx, r)
}

func (h *auditLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &auditLogHandler{base: h.base.WithAttrs(attrs).(*humanLogHandler)}
}

func (h *auditLogHandler) WithGroup(name string) slog.Handler {
	return &auditLogHandler{base: h.base.WithGroup(name).(*humanLogHandler)}
}

func recordHasAttr(r slog.Record, key, value string) bool {
	found := false
	r.Attrs(func(attr slog.Attr) bool {
		if attr.Key == key && attr.Value.Kind() == slog.KindString && attr.Value.String() == value {
			found = true
			return false
		}
		return true
	})
	return found
}

type dailyLogWriter struct {
	mu      sync.Mutex
	dir     string
	day     string
	current *os.File
}

func newDailyLogWriter(dir string) (*dailyLogWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &dailyLogWriter{dir: dir}, nil
}

func (w *dailyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	day := time.Now().Format("2006-01-02")
	if w.current == nil || w.day != day {
		if err := w.rotate(day); err != nil {
			return 0, err
		}
	}
	return w.current.Write(p)
}

func (w *dailyLogWriter) rotate(day string) error {
	if w.current != nil {
		_ = w.current.Close()
		w.current = nil
	}
	path := filepath.Join(w.dir, day+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.day = day
	w.current = file
	return nil
}

func (w *dailyLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		return nil
	}
	err := w.current.Close()
	w.current = nil
	return err
}

func (h *humanLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *humanLogHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(r.Time.Format("15:04:05"))
	b.WriteString("] ")
	if r.Level >= slog.LevelError {
		b.WriteString("ERROR ")
	} else if r.Level >= slog.LevelWarn {
		b.WriteString("WARN ")
	}
	b.WriteString(r.Message)

	attrs := append([]slog.Attr(nil), h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Key)
		b.WriteByte('=')
		b.WriteString(formatLogValue(attr.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *humanLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *humanLogHandler) WithGroup(_ string) slog.Handler {
	return h
}

func formatLogValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindDuration:
		return v.Duration().Round(time.Millisecond).String()
	case slog.KindString:
		s := v.String()
		if strings.ContainsAny(s, " \t\r\n") {
			return fmt.Sprintf("%q", s)
		}
		return s
	default:
		return v.String()
	}
}
