package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"github.com/tokendancelab/metapi-go/app"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/router"
	"github.com/tokendancelab/metapi-go/store"
	"github.com/tokendancelab/metapi-go/web"
)

func main() {
	// ---- 0. Load .env file (silently skip if not found) ----
	_ = godotenv.Load()

	// Build env map from os.Environ()
	env := environMap()

	// ---- 1. Load config ----
	cfg := config.Load(env)
	config.Set(cfg)

	// ---- 1a. Validate config at startup ----
	errs := cfg.Validate()
	hasCritical := false
	for _, err := range errs {
		if config.IsCritical(err) {
			slog.Error("config validation", "error", err)
			hasCritical = true
		} else {
			slog.Warn("config validation", "error", err)
		}
	}
	if hasCritical {
		os.Exit(1)
	}

	// Normalize DataDir (E11: trailing slash / Windows backslash)
	cfg.DataDir = filepath.Clean(cfg.DataDir)

	// ---- Steps 2-11: wrapped in try/catch → failures only warn ----
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("startup bootstrap panicked", "panic", r)
			}
		}()

		// E5: ensure data directory exists
		if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
			slog.Warn("failed to create data directory", "dir", cfg.DataDir, "error", err)
		}

		// ---- 2. Bootstrap: ensureRuntimeDatabaseReady ----
		if err := store.EnsureRuntimeDatabase(cfg); err != nil {
			slog.Warn("bootstrap: ensureRuntimeDatabase failed", "error", err)
		}

		// ---- 3. Load settings from DB ----
		if err := store.LoadRuntimeSettings(cfg); err != nil {
			slog.Warn("settings: failed to load runtime settings", "error", err)
		}

		// ---- 4. Switch DB if settings differ (stub) ----
		// P1: read savedDbConfig, compare, switch if different, rollback on failure

		// ---- 5-6. Schema migrations ----
		if err := store.Migrate(cfg); err != nil {
			slog.Warn("migration failed", "error", err)
		}

		// ---- 7-11. Remaining startup steps (P1+) ----
		// reload settings, apply runtime overrides, log cleanup config,
		// remaining migrations, rebuild routes, ensure OAuth provider sites

		slog.Info("bootstrap complete")
	}()

	// ---- 12. Create HTTP router ----
	r := router.New(cfg, web.Dist)

	// Override /health handler with actual implementation
	// (router.New registers a placeholder; in production we'd pass the handler in)
	// For now, the router.New already registers a valid /health handler.

	// ---- 17. Start background services (stubs) ----
	app.StartBackgroundServices()

	// ---- 20. Register onClose hooks ----
	a := app.New(cfg, r)
	a.RegisterOnClose(func() {
		app.StopBackgroundServices()
	})

	// ---- 21-23. Listen + shutdown ----
	if err := a.Start(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}

	// Ensure any remaining cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.Shutdown(ctx)
}

// environMap converts os.Environ() to a map[string]string.
func environMap() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				env[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return env
}
