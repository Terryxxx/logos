// Command logos-server runs the local HTTP+WS server that backs the Logos
// desktop app. It is intended to be spawned as a Tauri sidecar in production
// and run standalone (`go run ./cmd/logos-server`) in development.
//
// Listening contract:
//   - Binds 127.0.0.1 only (never 0.0.0.0). Refuses external connections.
//   - Picks LOGOS_PORT or 7878 by default; if busy, increments until free.
//   - Writes <data-dir>/runtime.json containing {port, token, pid}.
//     The Tauri main process reads this file and injects token+url into the
//     webview as window.__LOGOS__.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/logos-app/logos/server/internal/agent"
	"github.com/logos-app/logos/server/internal/auth"
	"github.com/logos-app/logos/server/internal/config"
	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/handler"
	"github.com/logos-app/logos/server/internal/realtime"
	"github.com/logos-app/logos/server/internal/service"
	"github.com/logos-app/logos/server/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	slog.Info("logos starting",
		"data_dir", cfg.DataDir,
		"port_pref", cfg.PreferredPort,
	)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		slog.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}

	tok, err := auth.LoadOrCreateToken(st)
	if err != nil {
		slog.Error("token init failed", "error", err)
		os.Exit(1)
	}

	bus := events.NewBus()
	hub := realtime.NewHub()
	go hub.Run()
	bus.Subscribe(func(e events.Event) { hub.Broadcast(e.Type, e.Payload) })

	// Auto-detect locally-installed agent CLIs and seed agent_runtime rows.
	if err := agent.DetectAndRegisterAll(context.Background(), st); err != nil {
		slog.Warn("agent detection had errors (server continues)", "error", err)
	}

	taskSvc := service.NewTaskService(st, bus)
	commentSvc := service.NewCommentService(st, taskSvc, bus)
	runner := service.NewRunner(st, taskSvc, commentSvc, agent.RegistryDefault(), filepath.Join(cfg.DataDir, "workspaces"))
	go runner.Run(context.Background())

	h := handler.New(st, taskSvc, commentSvc, bus, tok)
	h.SetRunner(runner)
	r := handler.NewRouter(h, hub, tok)

	addr, ln, err := config.BindLocal(cfg.PreferredPort)
	if err != nil {
		slog.Error("bind failed", "error", err)
		os.Exit(1)
	}

	if err := cfg.WriteRuntimeFile(addr, tok); err != nil {
		slog.Error("runtime.json write failed", "error", err)
		os.Exit(1)
	}
	slog.Info("listening", "addr", "http://"+addr, "runtime_file", cfg.RuntimeFilePath())

	srv := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	runner.Stop()
	slog.Info("bye")
}
