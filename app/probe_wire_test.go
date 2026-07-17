package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/scheduler"
	"github.com/tokendancelab/metapi-go/store"
)

func TestWireModelProbeScheduler_ProbeMutatesHealth(t *testing.T) {
	// Create temp dir first so its cleanup is registered early (runs last = after DB close).
	dataDir := t.TempDir()

	_ = store.CloseDatabase()
	routing.ResetSiteRuntimeHealthState()
	// Close DB before TempDir removal (LIFO: this runs before TempDir cleanup).
	t.Cleanup(func() {
		_ = store.CloseDatabase()
		proxyhandler.SetUpstreamConfig(nil)
		rememberProbeRouter(nil, nil)
		scheduler.SetGlobalModelProbeScheduler(nil)
		routing.ResetSiteRuntimeHealthState()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-wire"}]}`))
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{
		AuthToken:                        "admin-token",
		ProxyToken:                       "downstream-token",
		DataDir:                          dataDir,
		DbType:                           store.DialectSQLite,
		DbUrl:                            filepath.Join(dataDir, "metapi.db"),
		RequestBodyLimit:                 1 << 20,
		ProxyMaxChannelAttempts:          3,
		ProxyFirstByteTimeoutSec:         90,
		TokenRouterCacheTtlMs:            60_000,
		RoutingFallbackUnitCost:          1,
		TokenRouterFailureCooldownMaxSec: 3600,
		ModelAvailabilityProbeEnabled:    false, // ticker off; manual probe still works
		ModelAvailabilityProbeTimeoutMs:  5000,
		RoutingWeights: config.RoutingWeights{
			BaseWeightFactor: 1,
			ValueScoreFactor: 1,
			CostWeight:       1,
			BalanceWeight:    1,
			UsageWeight:      1,
		},
	}
	config.Set(cfg)

	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	db := store.GetDB()

	// Seed cooling channel so success clears cooldown + stamps probe status.
	now := time.Now().UTC().Format(time.RFC3339)
	until := "2099-01-01T00:00:00Z"
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"wire-site", upstream.URL, "anyrouter", "active", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO accounts (site_id, access_token, api_token, status, balance, quota, value_score, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		siteID, "wire-token", "wire-token", "active", 10.0, 100.0, 1.0, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"gpt-wire", "pattern", "weighted", true, now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO route_channels (
		route_id, account_id, source_model, priority, weight, enabled,
		fail_count, consecutive_fail_count, cooldown_level, cooldown_until, last_fail_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		routeID, accountID, "gpt-wire", 0, 10, true,
		2, 2, 1, until, now)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, _ := res.LastInsertId()

	if err := ConfigureProxyUpstream(cfg); err != nil {
		t.Fatalf("ConfigureProxyUpstream: %v", err)
	}

	s := scheduler.NewModelProbeScheduler(cfg)
	WireModelProbeScheduler(s)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	results, available, unavailable := s.ProbeSite(siteID)
	if available != 1 || unavailable != 0 {
		t.Fatalf("ProbeSite available=%d unavailable=%d results=%+v", available, unavailable, results)
	}
	if len(results) != 1 || results[0].Status != "success" || results[0].ChannelID != channelID {
		t.Fatalf("ProbeSite results = %+v", results)
	}

	var cooldown *string
	var consecutive int64
	if err := db.QueryRow(`SELECT cooldown_until, consecutive_fail_count FROM route_channels WHERE id = ?`, channelID).
		Scan(&cooldown, &consecutive); err != nil {
		t.Fatalf("read channel: %v", err)
	}
	if cooldown != nil {
		t.Fatalf("cooldown_until still set after probe success: %v", *cooldown)
	}
	if consecutive != 0 {
		t.Fatalf("consecutive_fail_count = %d, want 0", consecutive)
	}

	probeStatus := routing.GetSiteProbeStatus(siteID)
	if probeStatus.Status != "success" {
		t.Fatalf("site probe status = %q, want success", probeStatus.Status)
	}
	if probeStatus.ChannelID == nil || *probeStatus.ChannelID != channelID {
		t.Fatalf("probe channel stamp = %v", probeStatus.ChannelID)
	}
}
