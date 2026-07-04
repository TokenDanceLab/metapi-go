package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// App is the top-level application struct that holds all runtime components.
type App struct {
	Config *config.Config
	Router http.Handler
	Server *http.Server

	onCloseFns []func()
}

// New creates a new App with the given config and router.
func New(cfg *config.Config, router http.Handler) *App {
	return &App{
		Config: cfg,
		Router: router,
	}
}

// RegisterOnClose adds a cleanup function to be called during shutdown,
// in FIFO order.
func (a *App) RegisterOnClose(fn func()) {
	a.onCloseFns = append(a.onCloseFns, fn)
}

// OnClose executes all registered onClose functions in FIFO order.
func (a *App) OnClose() {
	for _, fn := range a.onCloseFns {
		fn()
	}
}

// Start begins listening on the configured host:port and blocks until
// a shutdown signal (SIGINT/SIGTERM) is received, then performs a
// graceful shutdown with a 5-second timeout.
func (a *App) Start() error {
	addr := fmt.Sprintf("%s:%d", a.Config.ListenHost, a.Config.Port)
	a.Server = &http.Server{
		Addr:         addr,
		Handler:      a.Router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Channel to capture listen errors
	errCh := make(chan error, 1)

	go func() {
		slog.Info("listening", "addr", addr)
		if err := a.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for signal or listen error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		slog.Error("server listen failed", "error", err)
		return err
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig.String())
	}

	// Graceful shutdown with 5s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.OnClose()
	if err := a.Server.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		return err
	}

	if err := store.CloseDatabase(); err != nil {
		slog.Warn("failed to close database", "error", err)
	}

	slog.Info("server stopped")
	return nil
}

// Shutdown performs a graceful shutdown of the HTTP server with the given context.
func (a *App) Shutdown(ctx context.Context) error {
	if a.Server == nil {
		return nil
	}
	a.OnClose()
	return a.Server.Shutdown(ctx)
}
