package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupAuthSettingsTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		db.Close()
		t.Fatalf("AutoMigrate: %v", err)
	}

	cfg := &config.Config{
		AuthToken: "admin-auth-settings-token",
	}
	r := chi.NewRouter()
	RegisterAuthSettingsRoutes(r, db.DB, cfg)
	return db, r, cfg
}

func TestAuthSettingsChange_WrongOldTokenForbidden(t *testing.T) {
	_, r, cfg := setupAuthSettingsTest(t)

	resp := doPostJSON(t, r, "/api/settings/auth/change", map[string]any{
		"oldToken": "wrong-old-token-value",
		"newToken": "brand-new-token-1",
	})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
	if body["message"] != "旧 Token 验证失败" {
		t.Fatalf("message = %q, want 旧 Token 验证失败", body["message"])
	}
	if cfg.AuthToken != "admin-auth-settings-token" {
		t.Fatalf("AuthToken mutated to %q", cfg.AuthToken)
	}
}

func TestAuthSettingsChange_WrongOldTokenDifferentLengthForbidden(t *testing.T) {
	_, r, cfg := setupAuthSettingsTest(t)

	resp := doPostJSON(t, r, "/api/settings/auth/change", map[string]any{
		"oldToken": "short",
		"newToken": "brand-new-token-2",
	})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", resp.Code, resp.Body.String())
	}
	if cfg.AuthToken != "admin-auth-settings-token" {
		t.Fatalf("AuthToken mutated to %q", cfg.AuthToken)
	}
}

func TestAuthSettingsChange_SuccessUpdatesToken(t *testing.T) {
	db, r, cfg := setupAuthSettingsTest(t)

	resp := doPostJSON(t, r, "/api/settings/auth/change", map[string]any{
		"oldToken": "admin-auth-settings-token",
		"newToken": "rotated-admin-token",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	if cfg.AuthToken != "rotated-admin-token" {
		t.Fatalf("AuthToken = %q, want rotated-admin-token", cfg.AuthToken)
	}

	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = 'auth_token'"); err != nil {
		t.Fatalf("load auth_token setting: %v", err)
	}
	if stored != `"rotated-admin-token"` {
		t.Fatalf("stored auth_token = %q, want JSON-quoted rotated-admin-token", stored)
	}
}

func TestConstantTimeTokenEqual(t *testing.T) {
	if !constantTimeTokenEqual("same-token", "same-token") {
		t.Fatal("expected equal tokens to match")
	}
	if constantTimeTokenEqual("same-token", "diff-token") {
		t.Fatal("expected same-length mismatch to fail")
	}
	if constantTimeTokenEqual("short", "much-longer-token") {
		t.Fatal("expected length mismatch to fail")
	}
}
