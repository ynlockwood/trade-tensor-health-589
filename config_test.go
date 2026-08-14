package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadChannelConfigSkipsZeroWeightChannels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.json")
	err := os.WriteFile(path, []byte(`{
  "channels": [
    {
      "name": "disabled",
      "baseURL": "http://127.0.0.1:1/v1",
      "apiKey": "sk-disabled",
      "weight": 0
    },
    {
      "name": "enabled",
      "baseURL": "http://127.0.0.1:2/v1",
      "apiKey": "sk-enabled",
      "weight": 2
    }
  ]
}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := loadChannelConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("channels len = %d, want 2", len(cfg.Channels))
	}
	if cfg.Channels[0].Name != "disabled" {
		t.Fatalf("first channel = %q, want disabled", cfg.Channels[0].Name)
	}
	if cfg.Channels[0].Weight != 0 {
		t.Fatalf("disabled weight = %d, want 0", cfg.Channels[0].Weight)
	}
	if cfg.Channels[1].Name != "enabled" {
		t.Fatalf("second channel = %q, want enabled", cfg.Channels[1].Name)
	}
	if cfg.Channels[1].Weight != 2 {
		t.Fatalf("enabled weight = %d, want 2", cfg.Channels[1].Weight)
	}
}

func TestLoadChannelConfigRejectsNegativeWeight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.json")
	err := os.WriteFile(path, []byte(`{
  "channels": [
    {
      "name": "negative",
      "baseURL": "http://127.0.0.1:1/v1",
      "apiKey": "sk-negative",
      "weight": -1
    }
  ]
}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := loadChannelConfig(path); err == nil {
		t.Fatal("expected negative weight to fail")
	}
}

func TestLoadAppConfigUsesConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{
  "listen": ":1999",
  "tokenFile": "tokens.test.json",
  "channelFile": "channels.test.json",
  "probe": {
    "enabled": true,
    "intervalSeconds": 60,
    "timeoutSeconds": 30,
    "model": "gpt-test",
    "prompt": "probe prompt",
    "userAgent": "ua",
    "openaiBeta": "beta",
    "originator": "origin",
    "version": "1",
    "requireStreaming": false
  }
}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":1999" || cfg.TokenFile != "tokens.test.json" || cfg.ChannelFile != "channels.test.json" {
		t.Fatalf("unexpected file config: %+v", cfg)
	}
	if cfg.Probe.Model != "gpt-test" || cfg.Probe.Prompt != "probe prompt" || cfg.Probe.IntervalSeconds != 60 {
		t.Fatalf("unexpected probe config: %+v", cfg.Probe)
	}
	if cfg.Probe.RequireStreaming {
		t.Fatal("requireStreaming should be false from config")
	}
}
