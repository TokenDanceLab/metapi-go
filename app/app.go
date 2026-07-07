package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	closeOnce  sync.Once
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("shutdown hook panicked", "panic", r)
				}
			}()
			fn()
		}()
	}
}

// Start begins listening on the configured host:port and blocks until
// a shutdown signal (SIGINT/SIGTERM) is received, then performs a
// graceful shutdown with a 5-second timeout.
func (a *App) Start() error {
	addr := fmt.Sprintf("%s:%d", a.Config.ListenHost, a.Config.Port)
	a.Server = newHTTPServer(addr, a.Router)

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
		return errors.Join(err, a.cleanup())
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig.String())
	}

	// Graceful shutdown with 5s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		return err
	}

	slog.Info("server stopped")
	return nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Shutdown performs a graceful shutdown of the HTTP server with the given context.
func (a *App) Shutdown(ctx context.Context) error {
	return a.shutdown(ctx)
}

func (a *App) shutdown(ctx context.Context) error {
	markReadinessDraining(true)
	defer markReadinessDraining(false)

	var shutdownErr error
	if a.Server != nil {
		shutdownErr = a.Server.Shutdown(ctx)
	}
	return errors.Join(shutdownErr, a.cleanup())
}

func (a *App) cleanup() error {
	var cleanupErr error
	a.closeOnce.Do(func() {
		a.OnClose()
		if err := store.CloseDatabase(); err != nil {
			slog.Warn("failed to close database", "error", err)
			cleanupErr = err
		}
	})
	return cleanupErr
}
