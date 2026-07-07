package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func TestRuntimeDatabaseGetRuntimeReportsActivePostgres(t *testing.T) {
	db := setupBackupTestDB(t)
	cfg := config.Load(map[string]string{
		"DB_TYPE":    "postgresql",
		"DB_URL":     "postgres://user:secret-pass@example.invalid:5432/metapi?sslmode=require",
		"DB_SSLMODE": "verify-full",
	})
	handler := &databaseHandler{db: db.DB, cfg: cfg}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/database/runtime", nil)
	rec := httptest.NewRecorder()

	handler.getRuntime(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("response leaked postgres password: %s", rec.Body.String())
	}

	var payload struct {
		Success bool `json:"success"`
		Active  struct {
			Dialect    string `json:"dialect"`
			Connection string `json:"connection"`
			Ssl        bool   `json:"ssl"`
		} `json:"active"`
		RestartRequired bool `json:"restartRequired"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("success = false, want true")
	}
	if payload.Active.Dialect != "postgres" {
		t.Fatalf("active dialect = %q, want postgres", payload.Active.Dialect)
	}
	if !payload.Active.Ssl {
		t.Fatalf("active ssl = false, want true")
	}
	if strings.Contains(payload.Active.Connection, "secret-pass") || !strings.Contains(payload.Active.Connection, "example.invalid:5432") {
		t.Fatalf("active connection = %q, want masked postgres host", payload.Active.Connection)
	}
	if payload.RestartRequired {
		t.Fatalf("restartRequired = true, want false with no saved override")
	}
}

func TestRuntimeDatabaseSaveRuntimeKeepsActiveDatabaseSeparateFromSavedOverride(t *testing.T) {
	db := setupBackupTestDB(t)
	cfg := config.Load(map[string]string{
		"DB_TYPE": "sqlite",
	})
	handler := &databaseHandler{db: db.DB, cfg: cfg}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/database/runtime", strings.NewReader(`{
		"dialect": "postgres",
		"connectionString": "postgres://user:future-pass@example.invalid:5432/metapi",
		"ssl": true
	}`))
	rec := httptest.NewRecorder()

	handler.saveRuntime(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "future-pass") {
		t.Fatalf("response leaked saved postgres password: %s", rec.Body.String())
	}

	var payload struct {
		Success bool `json:"success"`
		Active  struct {
			Dialect string `json:"dialect"`
		} `json:"active"`
		Saved struct {
			Dialect    string `json:"dialect"`
			Connection string `json:"connection"`
			Ssl        bool   `json:"ssl"`
		} `json:"saved"`
		RestartRequired bool `json:"restartRequired"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("success = false, want true")
	}
	if payload.Active.Dialect != "sqlite" {
		t.Fatalf("active dialect = %q, want current sqlite until restart", payload.Active.Dialect)
	}
	if payload.Saved.Dialect != "postgres" {
		t.Fatalf("saved dialect = %q, want postgres", payload.Saved.Dialect)
	}
	if !payload.Saved.Ssl {
		t.Fatalf("saved ssl = false, want true")
	}
	if strings.Contains(payload.Saved.Connection, "future-pass") || !strings.Contains(payload.Saved.Connection, "example.invalid:5432") {
		t.Fatalf("saved connection = %q, want masked postgres host", payload.Saved.Connection)
	}
	if !payload.RestartRequired {
		t.Fatalf("restartRequired = false, want true after saved override")
	}
}

func TestRuntimeDatabaseRejectsUnsupportedDialect(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &databaseHandler{db: db.DB}

	cases := []struct {
		name   string
		method string
		path   string
		call   http.HandlerFunc
	}{
		{
			name:   "save runtime",
			method: http.MethodPut,
			path:   "/api/settings/database/runtime",
			call:   handler.saveRuntime,
		},
		{
			name:   "test connection",
			method: http.MethodPost,
			path:   "/api/settings/database/test-connection",
			call:   handler.testConnection,
		},
		{
			name:   "migrate",
			method: http.MethodPost,
			path:   "/api/settings/database/migrate",
			call:   handler.migrate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{
				"dialect": "mysql",
				"connectionString": "mysql://user:pass@example.invalid:3306/metapi"
			}`))
			rec := httptest.NewRecorder()

			tc.call(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "sqlite") || !strings.Contains(rec.Body.String(), "postgres") {
				t.Fatalf("body = %s, want supported dialect message", rec.Body.String())
			}
		})
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", "db_type"); err != nil {
		t.Fatalf("count db_type: %v", err)
	}
	if count != 0 {
		t.Fatalf("unsupported dialect was persisted, db_type rows=%d", count)
	}
}

func TestRuntimeDatabaseNormalizesPostgresqlAlias(t *testing.T) {
	dialect, ok := normalizeRuntimeDatabaseDialect(" postgresql ")
	if !ok {
		t.Fatal("postgresql alias was rejected")
	}
	if dialect != "postgres" {
		t.Fatalf("dialect = %q, want postgres", dialect)
	}
}

func TestApplyPostgresTestConnectTimeout(t *testing.T) {
	withDefault := applyPostgresTestConnectTimeout("postgres://user:pass@example.invalid:5432/metapi?sslmode=disable")
	if !strings.Contains(withDefault, "connect_timeout=5") {
		t.Fatalf("dsn = %q, want default connect_timeout", withDefault)
	}
	if !strings.Contains(withDefault, "sslmode=disable") {
		t.Fatalf("dsn = %q, want existing query preserved", withDefault)
	}

	withExisting := applyPostgresTestConnectTimeout("postgres://user:pass@example.invalid/metapi?connect_timeout=2")
	if !strings.Contains(withExisting, "connect_timeout=2") || strings.Contains(withExisting, "connect_timeout=5") {
		t.Fatalf("dsn = %q, want existing connect_timeout preserved", withExisting)
	}

	keyword := applyPostgresTestConnectTimeout("host=example.invalid user=metapi dbname=metapi")
	if !strings.Contains(keyword, "connect_timeout=5") {
		t.Fatalf("keyword dsn = %q, want default connect_timeout", keyword)
	}
}

func TestRuntimeDatabaseTestConnectionSQLiteSuccess(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &databaseHandler{db: db.DB}
	target := filepath.Join(t.TempDir(), "target.db")

	req := httptest.NewRequest(http.MethodPost, "/api/settings/database/test-connection", strings.NewReader(`{
		"dialect": "sqlite",
		"connectionString": "`+strings.ReplaceAll(target, `\`, `\\`)+`"
	}`))
	rec := httptest.NewRecorder()

	handler.testConnection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"dialect":"sqlite"`) {
		t.Fatalf("body = %s, want sqlite dialect", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("body = %s, want success", rec.Body.String())
	}
}

func TestRuntimeDatabaseTestConnectionPostgresFailureDoesNotLeakPassword(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &databaseHandler{db: db.DB}

	const dsn = "postgres://user:secret-pass@127.0.0.1:1/metapi?sslmode=disable&connect_timeout=1"
	req := httptest.NewRequest(http.MethodPost, "/api/settings/database/test-connection", strings.NewReader(`{
		"dialect": "postgres",
		"connectionString": "`+dsn+`"
	}`))
	rec := httptest.NewRecorder()

	handler.testConnection(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") || strings.Contains(rec.Body.String(), dsn) {
		t.Fatalf("response leaked connection secret: %s", rec.Body.String())
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Success {
		t.Fatalf("success = true, want false")
	}
	if !strings.Contains(payload.Message, "数据库测试连接失败") {
		t.Fatalf("message = %q, want connection failure message", payload.Message)
	}
}

func TestRuntimeDatabaseMigrateIsNotImplemented(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &databaseHandler{db: db.DB}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/database/migrate", strings.NewReader(`{
		"dialect": "postgres",
		"connectionString": "postgres://user:pass@example.invalid:5432/metapi"
	}`))
	rec := httptest.NewRecorder()

	handler.migrate(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool              `json:"success"`
		Message string            `json:"message"`
		Dialect string            `json:"dialect"`
		Rows    map[string]uint64 `json:"rows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Success {
		t.Fatalf("success = true, want false")
	}
	if payload.Dialect != "postgres" {
		t.Fatalf("dialect = %q, want postgres", payload.Dialect)
	}
	if payload.Rows != nil {
		t.Fatalf("rows = %#v, want omitted rows for unimplemented migration", payload.Rows)
	}
	if !strings.Contains(payload.Message, "metapi-migrate") {
		t.Fatalf("message = %q, want CLI migration guidance", payload.Message)
	}
}
