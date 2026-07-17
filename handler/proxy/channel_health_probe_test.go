package proxyhandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/scheduler"
	"github.com/tokendancelab/metapi-go/store"
)

func TestChannelHealthProbeExecutor_ModelsSuccess(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer probe-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-probe"}]}`))
	}))
	t.Cleanup(upstream.Close)

	db := openProbeTestDB(t)
	channelID := seedProbeChannel(t, db, upstream.URL, "gpt-probe", "probe-token")

	probe := NewChannelHealthProbeExecutor(&config.Config{})
	probe.SetDB(db)
	out, err := probe.ProbeChannel(context.Background(), scheduler.ProbeTarget{
		ChannelID: channelID,
		ModelName: "gpt-probe",
	})
	if err != nil {
		t.Fatalf("ProbeChannel: %v", err)
	}
	if out.Status != "success" {
		t.Fatalf("status = %q error=%q", out.Status, out.ErrorText)
	}
	if out.HTTPStatus != 200 {
		t.Fatalf("http status = %d", out.HTTPStatus)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestChannelHealthProbeExecutor_Models404FallsBackToChat(t *testing.T) {
	var modelsHits, chatHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			modelsHits.Add(1)
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			chatHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl_probe","choices":[{"message":{"content":"ok"}}]}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(upstream.Close)

	db := openProbeTestDB(t)
	channelID := seedProbeChannel(t, db, upstream.URL, "gpt-probe", "probe-token")

	probe := NewChannelHealthProbeExecutor(&config.Config{})
	probe.SetDB(db)
	out, err := probe.ProbeChannel(context.Background(), scheduler.ProbeTarget{
		ChannelID: channelID,
		ModelName: "gpt-probe",
	})
	if err != nil {
		t.Fatalf("ProbeChannel: %v", err)
	}
	if out.Status != "success" {
		t.Fatalf("status = %q error=%q", out.Status, out.ErrorText)
	}
	if modelsHits.Load() != 1 || chatHits.Load() != 1 {
		t.Fatalf("models=%d chat=%d", modelsHits.Load(), chatHits.Load())
	}
}

func TestChannelHealthProbeExecutor_Upstream5xxIsFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)

	db := openProbeTestDB(t)
	channelID := seedProbeChannel(t, db, upstream.URL, "gpt-probe", "probe-token")

	probe := NewChannelHealthProbeExecutor(&config.Config{})
	probe.SetDB(db)
	out, err := probe.ProbeChannel(context.Background(), scheduler.ProbeTarget{
		ChannelID: channelID,
		ModelName: "gpt-probe",
	})
	if err != nil {
		t.Fatalf("ProbeChannel: %v", err)
	}
	if out.Status != "failure" {
		t.Fatalf("status = %q, want failure", out.Status)
	}
	if out.HTTPStatus != 502 {
		t.Fatalf("http status = %d", out.HTTPStatus)
	}
	if !strings.Contains(out.ErrorText, "502") {
		t.Fatalf("error = %q", out.ErrorText)
	}
}

func openProbeTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func seedProbeChannel(t *testing.T, db *store.DB, upstreamURL, model, token string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"probe-site", upstreamURL, "anyrouter", "active", now, now)
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
