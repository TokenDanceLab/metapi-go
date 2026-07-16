package app

import (
	"context"
	"log/slog"
	"strings"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/internal/redisx"
	"github.com/tokendancelab/metapi-go/routing"
)

// ConfigureSharedState wires optional Redis-backed multi-instance state (#118):
//   - downstream-key RPM/TPM admission counters (auth.GlobalKeyAdmission)
//   - soft channel cooldown markers (routing soft filter)
//
// Empty REDIS_URL leaves process-local defaults. Redis is never a hard dependency:
// connect/parse failures log a warning and continue with in-process state.
// Runtime Redis command errors fail-open (documented in docs/analysis/redis-shared-state.md).
func ConfigureSharedState(cfg *config.Config) {
	if cfg == nil {
		return
	}
	url := strings.TrimSpace(cfg.RedisURL)
	if url == "" {
		auth.ConfigureKeyAdmissionCounter(nil)
		routing.ConfigureSoftCooldown(nil)
		return
	}

	client, err := redisx.NewClient(url)
	if err != nil {
		slog.Warn("redis shared state disabled: invalid REDIS_URL", "error", err)
		auth.ConfigureKeyAdmissionCounter(nil)
		routing.ConfigureSoftCooldown(nil)
		return
	}

	// Best-effort connectivity probe; do not abort startup on failure.
	if err := client.Ping(context.Background()); err != nil {
		slog.Warn("redis PING failed at startup; shared state will fail-open until reachable",
			"error", err,
		)
	}

	auth.ConfigureKeyAdmissionCounter(redisx.NewRedisCounterFromClient(client))
	routing.ConfigureSoftCooldown(redisx.NewRedisCooldown(client))
	slog.Info("redis shared state enabled",
		"admission", true,
		"soft_cooldown", true,
	)
}
