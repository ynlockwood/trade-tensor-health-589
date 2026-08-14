package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.json", "app config path")
	listenOverride := flag.String("listen", "", "override listen address")
	tokenOverride := flag.String("tokens", "", "override client token config path")
	channelOverride := flag.String("channels", "", "override upstream channel config path")
	flag.Parse()

	logger := slog.New(newHumanLogHandler(os.Stdout, slog.LevelInfo))
	slog.SetDefault(logger)

	appCfg, err := loadAppConfig(*configPath)
	if err != nil {
		logger.Error("failed to load app config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	if *listenOverride != "" {
		appCfg.Listen = *listenOverride
	}
	if *tokenOverride != "" {
		appCfg.TokenFile = *tokenOverride
	}
	if *channelOverride != "" {
		appCfg.ChannelFile = *channelOverride
	}

	tokenCfg, err := loadTokenConfig(appCfg.TokenFile)
	if err != nil {
		logger.Error("failed to load token config", "path", appCfg.TokenFile, "error", err)
		os.Exit(1)
	}
	channelCfg, err := loadChannelConfig(appCfg.ChannelFile)
	if err != nil {
		logger.Error("failed to load channel config", "path", appCfg.ChannelFile, "error", err)
		os.Exit(1)
	}

	channelState := newChannelStateStore(appCfg.ChannelFile, channelCfg)
	proxy, err := newProxyServer(tokenCfg.Tokens, channelCfg.Channels, channelState, logger)
	if err != nil {
		logger.Error("failed to initialize proxy", "error", err)
		os.Exit(1)
	}
	appCtx, stopApp := context.WithCancel(context.Background())
	defer stopApp()
	newConfigReloader(appCfg.TokenFile, appCfg.ChannelFile, proxy.auth, channelState, logger).Start(appCtx)
	if appCfg.Probe.Enabled {
		newChannelHealthProbe(channelState, logger, appCfg.Probe).Start(appCtx)
	}

	mux := http.NewServeMux()
	mux.Handle("/", proxy)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              appCfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	logger.Info("代理已启动",
		"listen", appCfg.Listen,
		"tokens", len(tokenCfg.Tokens),
		"channels", len(channelCfg.Channels),
		"probe", appCfg.Probe.Enabled,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("收到停止信号", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
	stopApp()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("代理已停止")
}
