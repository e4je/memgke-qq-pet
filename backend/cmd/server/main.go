package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"qq-pet/backend/internal/automation"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/managed"
	"qq-pet/backend/internal/petapi"
	"qq-pet/backend/internal/server"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("插件退出", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg := config.Load()
	runtime, err := managed.Load(&cfg)
	if err != nil {
		return err
	}
	if runtime == nil {
		return errors.New("仅支持萌卡 NT 插件托管模式，请在框架中导入并启动")
	}
	store, err := automation.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	conn := cfg.ManagedConnection
	client := petapi.New(conn.Host, conn.Port, conn.Token, cfg.Version)
	connectionDone := make(chan struct{})
	go func() { defer close(connectionDone); client.Run(ctx) }()
	defer func() { stop(); <-connectionDone }()
	engine := automation.New(client, store)
	handler := runtime.Wrap(server.New(engine, cfg.Version, client.Connected), client.Connected, engine.Start, engine.Stop)
	httpServer := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
	failed := make(chan error, 1)
	go func() {
		slog.Info("QQ 宠物插件已启动", "version", cfg.Version, "address", cfg.Addr)
		failed <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-failed:
	}
	runtime.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
