package main

import (
	"fmt"
	"log/slog"
	"net/http"
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
	// ---- Healthcheck subcommand (for Docker HEALTHCHECK without curl) ----
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

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

	if err := bootstrapRuntime(cfg); err != nil {
		slog.Error("startup bootstrap failed", "error", err)
		os.Exit(1)
	}
	if err := app.ConfigureProxyUpstream(cfg); err != nil {
		slog.Error("proxy upstream wiring failed", "error", err)
		os.Exit(1)
	}

	// ---- 12. Create HTTP router ----
	r := router.New(cfg, web.Dist)

	// Override /health handler with actual implementation
	// (router.New registers a placeholder; in production we'd pass the handler in)
	// For now, the router.New already registers a valid /health handler.

	// ---- 17. Start background services (stubs) ----
	app.StartBackgroundServices()

	// ---- 18. Start pprof debug server (port 6060, only with -tags debug) ----
	app.StartDebugServer(6060)

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
}

func bootstrapRuntime(cfg *config.Config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("startup bootstrap panicked: %v", r)
		}
	}()

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("create data directory %q: %w", cfg.DataDir, err)
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		return fmt.Errorf("ensure runtime database: %w", err)
	}
	if err := store.LoadRuntimeSettings(cfg); err != nil {
		return fmt.Errorf("load runtime settings: %w", err)
	}
	if err := store.Migrate(cfg); err != nil {
		return fmt.Errorf("run runtime migrations: %w", err)
	}

	slog.Info("bootstrap complete")
	return nil
}

func runHealthcheck() int {
	target := os.Getenv("METAPI_HEALTHCHECK_URL")
	if target == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "4000"
		}
		path := os.Getenv("METAPI_HEALTHCHECK_PATH")
		if path == "" {
			path = "/ready"
		}
		target = "http://127.0.0.1:" + port + path
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0
	}
	return 1
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
