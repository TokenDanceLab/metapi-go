package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func TestReadyReportsDrainingWhenShutdownStarts(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		DbType:  store.DialectSQLite,
		DbUrl:   filepath.Join(dataDir, "draining.db"),
		DataDir: dataDir,
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	t.Cleanup(func() {
		_ = store.CloseDatabase()
		setReadinessDrainingForTest(false)
	})

	setReadinessDrainingForTest(true)
	rec := httptest.NewRecorder()
	Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready while draining: got %d, want %d body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var body HealthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ready body: %v", err)
	}
	if body.Status != "draining" || body.Database != "ok" {
		t.Fatalf("ready body while draining = %+v, want status=draining database=ok", body)
	}
}
