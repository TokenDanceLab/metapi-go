package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func TestRunHealthcheckUsesConfiguredURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			t.Fatalf("healthcheck path = %q, want /ready", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Setenv("METAPI_HEALTHCHECK_URL", ts.URL+"/ready")
	if code := runHealthcheck(); code != 0 {
		t.Fatalf("runHealthcheck healthy exit = %d, want 0", code)
	}
}

func TestRunHealthcheckFailsOnUnreadyStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	t.Setenv("METAPI_HEALTHCHECK_URL", ts.URL+"/ready")
	if code := runHealthcheck(); code != 1 {
		t.Fatalf("runHealthcheck unready exit = %d, want 1", code)
	}
}

func TestBootstrapRuntimeFailsOnUnavailablePostgres(t *testing.T) {
	_ = store.CloseDatabase()
	t.Cleanup(func() { _ = store.CloseDatabase() })

	cfg := config.Load(map[string]string{
		"DB_TYPE":      "postgres",
		"DATABASE_URL": "postgres://user:pass@127.0.0.1:1/metapi?sslmode=disable",
		"DATA_DIR":     t.TempDir(),
	})

	err := bootstrapRuntime(cfg)
	if err == nil {
		t.Fatal("bootstrapRuntime succeeded with unavailable PostgreSQL")
	}
	if !strings.Contains(err.Error(), "ensure runtime database") {
		t.Fatalf("bootstrapRuntime error = %v, want ensure runtime database context", err)
	}
}

func TestStartupValidationRejectsDefaultTokens(t *testing.T) {
	cfg := config.Load(map[string]string{})

	var critical []string
	for _, err := range cfg.Validate() {
		if config.IsCritical(err) {
			critical = append(critical, err.Error())
		}
	}
	got := strings.Join(critical, "\n")
	if !strings.Contains(got, "auth_token") {
		t.Fatalf("startup validation critical errors = %q, want auth_token", got)
	}
	if !strings.Contains(got, "proxy_token") {
		t.Fatalf("startup validation critical errors = %q, want proxy_token", got)
	}
}

func TestBootstrapRuntimeInitializesSQLite(t *testing.T) {
	_ = store.CloseDatabase()

	dataDir := t.TempDir()
	t.Cleanup(func() { _ = store.CloseDatabase() })
	cfg := config.Load(map[string]string{
		"DB_TYPE":  "sqlite",
		"DB_URL":   filepath.Join(dataDir, "bootstrap.db"),
		"DATA_DIR": dataDir,
	})

	if err := bootstrapRuntime(cfg); err != nil {
		t.Fatalf("bootstrapRuntime sqlite: %v", err)
	}
	if db := store.GetDB(); db == nil {
		t.Fatal("bootstrapRuntime did not initialize active DB")
	}
}
