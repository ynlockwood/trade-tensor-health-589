package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type AppConfig struct {
	Listen      string      `json:"listen"`
	TokenFile   string      `json:"tokenFile"`
	ChannelFile string      `json:"channelFile"`
	Probe       ProbeConfig `json:"probe"`
}

type ProbeConfig struct {
	Enabled          bool   `json:"enabled"`
	IntervalSeconds  int    `json:"intervalSeconds"`
	TimeoutSeconds   int    `json:"timeoutSeconds"`
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	UserAgent        string `json:"userAgent"`
	OpenAIBeta       string `json:"openaiBeta"`
	Originator       string `json:"originator"`
	Version          string `json:"version"`
	RequireStreaming bool   `json:"requireStreaming"`
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		Listen:      ":1921",
		TokenFile:   "token.json",
		ChannelFile: "channel.json",
		Probe: ProbeConfig{
			Enabled:          true,
			IntervalSeconds:  300,
			TimeoutSeconds:   120,
			Model:            "gpt-5.5",
			Prompt:           "你好，你是什么大模型？",
			UserAgent:        "codex-tui/0.125.0 (Windows 10.0; x86_64) xterm-256color (codex-tui; 0.125.0)",
			OpenAIBeta:       "responses=experimental",
			Originator:       "codex_cli_rs",
			Version:          "0.125.0",
			RequireStreaming: true,
		},
	}
}

func loadAppConfig(path string) (AppConfig, error) {
	cfg := defaultAppConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	normalizeAppConfig(&cfg)
	return cfg, nil
}

func normalizeAppConfig(cfg *AppConfig) {
	defaults := defaultAppConfig()
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	if cfg.Listen == "" {
		cfg.Listen = defaults.Listen
	}
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	if cfg.TokenFile == "" {
		cfg.TokenFile = defaults.TokenFile
	}
	cfg.ChannelFile = strings.TrimSpace(cfg.ChannelFile)
	if cfg.ChannelFile == "" {
		cfg.ChannelFile = defaults.ChannelFile
	}
	if cfg.Probe.IntervalSeconds <= 0 {
		cfg.Probe.IntervalSeconds = defaults.Probe.IntervalSeconds
	}
	if cfg.Probe.TimeoutSeconds <= 0 {
		cfg.Probe.TimeoutSeconds = defaults.Probe.TimeoutSeconds
	}
	cfg.Probe.Model = strings.TrimSpace(cfg.Probe.Model)
	if cfg.Probe.Model == "" {
		cfg.Probe.Model = defaults.Probe.Model
	}
	cfg.Probe.Prompt = strings.TrimSpace(cfg.Probe.Prompt)
	if cfg.Probe.Prompt == "" {
		cfg.Probe.Prompt = defaults.Probe.Prompt
	}
	cfg.Probe.UserAgent = strings.TrimSpace(cfg.Probe.UserAgent)
	if cfg.Probe.UserAgent == "" {
		cfg.Probe.UserAgent = defaults.Probe.UserAgent
	}
	cfg.Probe.OpenAIBeta = strings.TrimSpace(cfg.Probe.OpenAIBeta)
	if cfg.Probe.OpenAIBeta == "" {
		cfg.Probe.OpenAIBeta = defaults.Probe.OpenAIBeta
	}
	cfg.Probe.Originator = strings.TrimSpace(cfg.Probe.Originator)
	if cfg.Probe.Originator == "" {
		cfg.Probe.Originator = defaults.Probe.Originator
	}
	cfg.Probe.Version = strings.TrimSpace(cfg.Probe.Version)
	if cfg.Probe.Version == "" {
		cfg.Probe.Version = defaults.Probe.Version
	}
}

func (c ProbeConfig) interval() time.Duration {
	return time.Duration(c.IntervalSeconds) * time.Second
}

func (c ProbeConfig) timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}
