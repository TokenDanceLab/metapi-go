package store

import (
	"strings"
	"testing"
	"time"
)

// TestNowISO8601Format verifies the canonical ISO 8601 timestamp format
// that must be used for all datetime columns in both SQLite and PG.
// Per spec: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
func TestNowISO8601Format(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Must contain 'T' separator (ISO 8601).
	if !strings.Contains(now, "T") {
		t.Errorf("expected ISO 8601 format with 'T' separator, got %q", now)
	}

	// Must end with 'Z' (UTC indicator).
	if !strings.HasSuffix(now, "Z") {
		t.Errorf("expected UTC 'Z' suffix, got %q", now)
	}

	// Must match exact length: "2006-01-02T15:04:05.000Z" = 24 chars.
	if len(now) != 24 {
		t.Errorf("expected 24-char ISO 8601, got %d chars: %q", len(now), now)
	}

	// Must contain exactly 2 colons (time separator).
	colonCount := strings.Count(now, ":")
	if colonCount != 3 {
		// zulu format 2006-01-02T15:04:05.000Z has 3 colons (time + offset)
		// Actually: 15:04:05 has 2 colons, .000Z has none
		// Wait — 2006-01-02T15:04:05.000Z: "15:04:05" has 2 colons.
		// Let me re-count: "2006-01-02T15:04:05.000Z" — colons at positions
		// after "2006" (between 06-01), after "01" (between 01-02T15),
		// after "15" (between 15:04), after "04" (between 04:05).
		// Actually: "2006-01-02T15:04:05.000Z"
		// Colons: 2006[-]01, 01[-]02T15, T15[:]04, 04[:]05
		// Wait: "2006-01-02T15:04:05.000Z" the colons are at positions
		// 4, 7, and 13, 16. So 4 colons.
		// Hmm: the string "2006-01-02T15:04:05.000Z" has:
		// - after 2006: ":"
		// - after 01: ":"
		// - after T15: ":"
		// - after 04: ":"
		// Wait no. Let me count character by character:
		// 2 0 0 6 - 0 1 - 0 2 T 1 5 : 0 4 : 0 5 . 0 0 0 Z
		// Positions: 4 is "-", 7 is "-". Those are dashes not colons.
		// Colons are at position 13 (after 15) and 16 (after 04).
		// So only 2 colons.
	}
	// Allow 2 colons for the time part H:MM:SS = 2 colons.
	if colonCount != 2 {
		t.Errorf("expected 2 colons in time part, got %d: %q", colonCount, now)
	}
}

// TestISO8601FormatsAreConsistent verifies that multiple calls produce
// the same format (no format drift across calls).
func TestISO8601FormatsAreConsistent(t *testing.T) {
	const format = "2006-01-02T15:04:05.000Z"

	formats := make([]string, 5)
	for i := 0; i < 5; i++ {
		formats[i] = time.Now().UTC().Format(format)
		time.Sleep(1 * time.Millisecond) // ensure time changes
	}

	// Each should have the same structure.
	for i, f := range formats {
		if len(f) != 24 {
			t.Errorf("call %d: unexpected length %d: %q", i, len(f), f)
		}
		if !strings.HasSuffix(f, "Z") {
			t.Errorf("call %d: missing Z suffix: %q", i, f)
		}
		if !strings.Contains(f, "T") {
			t.Errorf("call %d: missing T separator: %q", i, f)
		}
	}
}

// TestDialectHelperBtype verifies boolean type mapping per dialect.
func TestDialectHelperBtype(t *testing.T) {
	tests := []struct {
		dialect  string
		expected string
	}{
		{DialectSQLite, "INTEGER"},
		{DialectPostgres, "BOOLEAN"},
		{"unknown", "INTEGER"}, // default to SQLite
	}

	for _, tt := range tests {
		got := btype(tt.dialect)
		if got != tt.expected {
			t.Errorf("btype(%q): expected %q, got %q", tt.dialect, tt.expected, got)
		}
	}
}

// TestDialectHelperRtype verifies float/real type mapping per dialect.
func TestDialectHelperRtype(t *testing.T) {
	tests := []struct {
		dialect  string
		expected string
	}{
		{DialectSQLite, "REAL"},
		{DialectPostgres, "DOUBLE PRECISION"},
		{"unknown", "REAL"}, // default to SQLite
	}

	for _, tt := range tests {
		got := rtype(tt.dialect)
		if got != tt.expected {
			t.Errorf("rtype(%q): expected %q, got %q", tt.dialect, tt.expected, got)
		}
	}

	// CRITICAL: PG must NEVER return naked "REAL" (float4 in PG = precision loss).
	if rtype(DialectPostgres) != "DOUBLE PRECISION" {
		t.Error("PG rtype must return DOUBLE PRECISION, not REAL")
	}
}

// TestDialectHelperSerialPK verifies serial PK mapping per dialect.
func TestDialectHelperSerialPK(t *testing.T) {
	tests := []struct {
		dialect  string
		expected string
	}{
		{DialectSQLite, "INTEGER PRIMARY KEY AUTOINCREMENT"},
		{DialectPostgres, "SERIAL PRIMARY KEY"},
		{"unknown", "INTEGER PRIMARY KEY AUTOINCREMENT"},
	}

	for _, tt := range tests {
		got := serialPK(tt.dialect)
		if got != tt.expected {
			t.Errorf("serialPK(%q): expected %q, got %q", tt.dialect, tt.expected, got)
		}
	}
}

// TestDialectHelperTextPK verifies text PK is same for both dialects.
func TestDialectHelperTextPK(t *testing.T) {
	for _, d := range []string{DialectSQLite, DialectPostgres, "unknown"} {
		got := textPK(d)
		if got != "TEXT PRIMARY KEY" {
			t.Errorf("textPK(%q): expected 'TEXT PRIMARY KEY', got %q", d, got)
		}
	}
}

// TestDialectIsPG verifies PG dialect detection.
func TestDialectIsPG(t *testing.T) {
	if !isPG(DialectPostgres) {
		t.Error("isPG(postgres) should be true")
	}
	if isPG(DialectSQLite) {
		t.Error("isPG(sqlite) should be false")
	}
	if isPG("mysql") {
		t.Error("isPG(mysql) should be false")
	}
	if isPG("") {
		t.Error("isPG('') should be false")
	}
}

// TestResolveSQLitePathEmpty returns default hub.db.
func TestResolveSQLitePathEmpty(t *testing.T) {
	path := ResolveSQLitePath("", "/data")
	if !strings.HasSuffix(path, "hub.db") {
		t.Errorf("empty DB_URL should resolve to hub.db, got %q", path)
	}
}

// TestResolveSQLitePathMemory preserves :memory:.
func TestResolveSQLitePathMemory(t *testing.T) {
	path := ResolveSQLitePath(":memory:", "/data")
	if path != ":memory:" {
		t.Errorf(":memory: should stay :memory:, got %q", path)
	}
}

// TestResolveSQLitePathFilePrefix strips file:// prefix.
func TestResolveSQLitePathFilePrefix(t *testing.T) {
	path := ResolveSQLitePath("file:///tmp/test.db", "/data")
	// file:// prefix should be stripped.
	if strings.HasPrefix(path, "file://") {
		t.Errorf("file:// prefix should be stripped, got %q", path)
	}
}

// TestResolveSQLitePathSQLitePrefix handles sqlite:// prefix.
func TestResolveSQLitePathSQLitePrefix(t *testing.T) {
	// We can't easily test absolute path resolution, but we can verify
	// the prefix is stripped.
	path := ResolveSQLitePath("sqlite://mydb.sqlite", "/data")
	if strings.HasPrefix(path, "sqlite://") {
		t.Errorf("sqlite:// prefix should be stripped, got %q", path)
	}
	if !strings.HasSuffix(path, "mydb.sqlite") {
		t.Errorf("expected path ending with mydb.sqlite, got %q", path)
	}
}

// TestDialectNameConstants verifies the dialect constants.
func TestDialectNameConstants(t *testing.T) {
	if DialectSQLite != "sqlite" {
		t.Errorf("DialectSQLite: expected 'sqlite', got %q", DialectSQLite)
	}
	if DialectPostgres != "postgres" {
		t.Errorf("DialectPostgres: expected 'postgres', got %q", DialectPostgres)
	}
}

// TestOpenInvalidDialect returns error for unsupported dialect.
func TestOpenInvalidDialect(t *testing.T) {
	_, err := Open("mysql", ":memory:", false)
	if err == nil {
		t.Error("expected error for unsupported 'mysql' dialect")
	}
	if !strings.Contains(err.Error(), "unsupported dialect") {
		t.Errorf("error should mention 'unsupported dialect': %v", err)
	}
}

func TestApplyPostgresSSLModeURL(t *testing.T) {
	got := applyPostgresSSLMode("postgres://user:pass@example.com:5432/metapi", "verify-full")
	if !strings.Contains(got, "sslmode=verify-full") {
		t.Fatalf("expected sslmode=verify-full in %q", got)
	}
}

func TestApplyPostgresSSLModeReplacesExistingURLParam(t *testing.T) {
	got := applyPostgresSSLMode("postgres://user:pass@example.com:5432/metapi?sslmode=disable&connect_timeout=5", "require")
	if strings.Contains(got, "sslmode=disable") {
		t.Fatalf("old sslmode was not replaced: %q", got)
	}
	if strings.Count(got, "sslmode=") != 1 {
		t.Fatalf("expected exactly one sslmode parameter, got %q", got)
	}
	if !strings.Contains(got, "sslmode=require") || !strings.Contains(got, "connect_timeout=5") {
		t.Fatalf("expected sslmode=require and preserved connect_timeout, got %q", got)
	}
}

func TestApplyPostgresSSLModeKeywordDSN(t *testing.T) {
	got := applyPostgresSSLMode("host=localhost dbname=metapi sslmode=disable connect_timeout=5", "verify-ca")
	if got != "host=localhost dbname=metapi sslmode=verify-ca connect_timeout=5" {
		t.Fatalf("unexpected keyword DSN: %q", got)
	}
}

func TestApplyPostgresSSLModeKeywordDSNAppend(t *testing.T) {
	got := applyPostgresSSLMode("host=localhost dbname=metapi", "require")
	if got != "host=localhost dbname=metapi sslmode=require" {
		t.Fatalf("unexpected keyword DSN append: %q", got)
	}
}

func TestOpenWithPostgresSSLModeRejectsInvalidMode(t *testing.T) {
	_, err := OpenWithPostgresSSLMode(DialectPostgres, "postgres://example.invalid/metapi", "invalid")
	if err == nil {
		t.Fatal("expected invalid sslmode error")
	}
	if !strings.Contains(err.Error(), "unsupported postgres sslmode") {
		t.Fatalf("unexpected error: %v", err)
	}
}
