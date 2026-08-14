package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelStateStoreErrorCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.json")
	cfg := &ChannelConfig{Channels: []Channel{
		{Name: "ch", BaseURL: "http://127.0.0.1:1/v1", APIKey: "sk", Weight: 1},
	}}
	store := newChannelStateStore(path, cfg)

	if err := store.RecordResult("ch", 502, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 1 {
		t.Fatalf("error count after 502 = %d, want 1", cfg.Channels[0].ErrorCount)
	}
	if err := store.RecordResult("ch", 500, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 2 {
		t.Fatalf("error count after second error = %d, want 2", cfg.Channels[0].ErrorCount)
	}
	if err := store.RecordResult("ch", 200, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 0 {
		t.Fatalf("error count after success = %d, want 0", cfg.Channels[0].ErrorCount)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"errorCount": 0`) {
		t.Fatalf("persisted channel config missing reset errorCount: %s", string(raw))
	}
}

func TestChannelStateStoreClassifiesStatusForChannelPenalty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.json")
	cfg := &ChannelConfig{Channels: []Channel{
		{Name: "ch", BaseURL: "http://127.0.0.1:1/v1", APIKey: "sk", Weight: 1},
	}}
	store := newChannelStateStore(path, cfg)

	if err := store.RecordResult("ch", 400, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 0 || cfg.Channels[0].AuthErrorCount != 0 {
		t.Fatalf("400 should not affect channel counters: %+v", cfg.Channels[0])
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ignored 400 should not persist config, stat err = %v", err)
	}

	if err := store.RecordResult("ch", 401, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 0 {
		t.Fatalf("401 errorCount = %d, want 0", cfg.Channels[0].ErrorCount)
	}
	if cfg.Channels[0].AuthErrorCount != 1 {
		t.Fatalf("401 authErrorCount = %d, want 1", cfg.Channels[0].AuthErrorCount)
	}

	if err := store.RecordResult("ch", 403, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].AuthErrorCount != 2 {
		t.Fatalf("403 authErrorCount = %d, want 2", cfg.Channels[0].AuthErrorCount)
	}

	if err := store.RecordResult("ch", 429, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 1 {
		t.Fatalf("429 errorCount = %d, want 1", cfg.Channels[0].ErrorCount)
	}

	if err := store.RecordResult("ch", 529, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 2 {
		t.Fatalf("529 errorCount = %d, want 2", cfg.Channels[0].ErrorCount)
	}

	if err := store.RecordResult("ch", 200, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Channels[0].ErrorCount != 0 || cfg.Channels[0].AuthErrorCount != 0 {
		t.Fatalf("success should clear channel counters: %+v", cfg.Channels[0])
	}
}
