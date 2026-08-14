package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type TokenConfig struct {
	Tokens []ClientToken `json:"tokens"`
}

type ClientToken struct {
	Name   string `json:"name"`
	APIKey string `json:"apiKey"`
}

type ChannelConfig struct {
	Channels []Channel `json:"channels"`
}

type Channel struct {
	Name           string `json:"name"`
	Remark         string `json:"remark"`
	BaseURL        string `json:"baseURL"`
	APIKey         string `json:"apiKey"`
	Weight         int    `json:"weight"`
	ErrorCount     int    `json:"errorCount"`
	AuthErrorCount int    `json:"authErrorCount,omitempty"`
}

func loadTokenConfig(path string) (*TokenConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg TokenConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := make(map[string]struct{}, len(cfg.Tokens))
	out := cfg.Tokens[:0]
	for _, token := range cfg.Tokens {
		token.Name = strings.TrimSpace(token.Name)
		token.APIKey = strings.TrimSpace(token.APIKey)
		if token.APIKey == "" {
			continue
		}
		if _, ok := seen[token.APIKey]; ok {
			continue
		}
		seen[token.APIKey] = struct{}{}
		out = append(out, token)
	}
	cfg.Tokens = out
	if len(cfg.Tokens) == 0 {
		return nil, errors.New("token config has no usable tokens")
	}
	return &cfg, nil
}

func loadChannelConfig(path string) (*ChannelConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := cfg.Channels[:0]
	for _, channel := range cfg.Channels {
		channel.Name = strings.TrimSpace(channel.Name)
		channel.Remark = strings.TrimSpace(channel.Remark)
		channel.BaseURL = strings.TrimRight(strings.TrimSpace(channel.BaseURL), "/")
		channel.APIKey = strings.TrimSpace(channel.APIKey)
		if channel.Name == "" || channel.BaseURL == "" || channel.APIKey == "" {
			continue
		}
		if channel.Weight < 0 {
			return nil, fmt.Errorf("channel %q has invalid negative weight %d", channel.Name, channel.Weight)
		}
		out = append(out, channel)
	}
	cfg.Channels = out
	if len(cfg.Channels) == 0 {
		return nil, errors.New("channel config has no usable channels")
	}
	return &cfg, nil
}
