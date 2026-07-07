package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

var _ routing.ChannelSelectorDB = (*proxyRoutingStore)(nil)
var _ routing.ChannelLoadSnapshotProvider = proxyLoadProvider{}
var _ proxy.TokenRouterInterface = (*routing.TokenRouter)(nil)

func TestConfigureProxyUpstreamWiresRealSQLiteRouter(t *testing.T) {
	_ = store.CloseDatabase()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer upstream-token" {
			t.Fatalf("Authorization = %q, want upstream token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_real_route","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	cfg := testProxyConfig(t)
	t.Cleanup(func() {
		proxyhandler.SetUpstreamConfig(nil)
		_ = store.CloseDatabase()
	})
	config.Set(cfg)
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	db := store.GetDB()
	channelID := seedProxyRoute(t, db, upstream.URL, "gpt-real", "upstream-token")

	if err := ConfigureProxyUpstream(cfg); err != nil {
		t.Fatalf("ConfigureProxyUpstream: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.ProxyAuth(cfg))
		proxyhandler.RegisterProxyRoutes(r)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-real","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer downstream-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "chatcmpl_real_route") {
		t.Fatalf("body = %q, want upstream response", body)
	}

	var lastSelected string
	if err := db.QueryRow(`SELECT last_selected_at FROM route_channels WHERE id = ?`, channelID).Scan(&lastSelected); err != nil {
		t.Fatalf("read last_selected_at: %v", err)
	}
	if strings.TrimSpace(lastSelected) == "" {
		t.Fatal("last_selected_at was not updated by real router")
	}
}

func testProxyConfig(t *testing.T) *config.Config {
	t.Helper()
	dataDir := t.TempDir()
	return &config.Config{
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
		RoutingWeights: config.RoutingWeights{
			BaseWeightFactor: 1,
			ValueScoreFactor: 1,
			CostWeight:       1,
			BalanceWeight:    1,
			UsageWeight:      1,
		},
	}
}

func seedProxyRoute(t *testing.T, db *store.DB, upstreamURL, model, token string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"upstream-site", upstreamURL, "anyrouter", "active", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	res, err = db.Exec(`INSERT INTO accounts (site_id, access_token, api_token, status, balance, quota, value_score, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		siteID, token, token, "active", 10.0, 100.0, 1.0, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}
	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		model, "pattern", "weighted", true, now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("route LastInsertId: %v", err)
	}
	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		routeID, accountID, model, 0, 10, true)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("channel LastInsertId: %v", err)
	}
	return channelID
}

func TestProxyRoutingStoreSelectsSeededChannel(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	channelID := seedProxyRoute(t, db, "https://example.invalid", "gpt-seeded", "seed-token")
	router := routing.NewTokenRouter(newProxyRoutingStore(db), testProxyConfig(t), nil, nil)

	selected, err := router.SelectChannel(context.Background(), "gpt-seeded", routing.EmptyDownstreamRoutingPolicy)
	if err != nil {
		t.Fatalf("SelectChannel: %v", err)
	}
	if selected == nil {
		t.Fatal("SelectChannel returned nil")
	}
	if selected.Channel.ID != channelID || selected.TokenValue != "seed-token" || selected.Site.URL != "https://example.invalid" {
		t.Fatalf("selected = %+v", selected)
	}
}
