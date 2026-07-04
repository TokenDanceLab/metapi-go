package store

import (
	"strings"
	"testing"
)

// setupSettingsDB opens a SQLite :memory: DB, migrates, and returns a SettingsStore.
func setupSettingsDB(t *testing.T) *SettingsStore {
	t.Helper()
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return NewSettingsStore(db)
}

// TestSettingStoreSetAndGet tests basic Set -> Get roundtrip.
func TestSettingStoreSetAndGet(t *testing.T) {
	s := setupSettingsDB(t)

	if err := s.Set("mykey", "myvalue"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := s.Get("mykey")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "myvalue" {
		t.Errorf("expected 'myvalue', got %q", val)
	}
}

// TestSettingStoreGetNonExistent returns empty string for missing key.
func TestSettingStoreGetNonExistent(t *testing.T) {
	s := setupSettingsDB(t)

	val, err := s.Get("does.not.exist")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}
}

// TestSettingStoreOverwrite verifies that Set overwrites an existing key.
func TestSettingStoreOverwrite(t *testing.T) {
	s := setupSettingsDB(t)

	if err := s.Set("overwrite.key", "v1"); err != nil {
		t.Fatalf("Set v1 failed: %v", err)
	}
	if err := s.Set("overwrite.key", "v2"); err != nil {
		t.Fatalf("Set v2 failed: %v", err)
	}

	val, err := s.Get("overwrite.key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "v2" {
		t.Errorf("expected 'v2', got %q", val)
	}
}

// TestSettingStoreGetAll verifies GetAll returns all keys.
func TestSettingStoreGetAll(t *testing.T) {
	s := setupSettingsDB(t)

	testData := map[string]string{
		"k1": "v1",
		"k2": "v2",
		"k3": "v3",
	}
	for k, v := range testData {
		if err := s.Set(k, v); err != nil {
			t.Fatalf("Set %q failed: %v", k, err)
		}
	}

	all, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	if len(all) != len(testData) {
		t.Errorf("expected %d keys, got %d", len(testData), len(all))
	}

	for k, expected := range testData {
		got, ok := all[k]
		if !ok {
			t.Errorf("GetAll missing key %q", k)
			continue
		}
		if got != expected {
			t.Errorf("GetAll[%q]: expected %q, got %q", k, expected, got)
		}
	}
}

// TestSettingStoreGetAllEmpty verifies GetAll on empty table returns empty map.
func TestSettingStoreGetAllEmpty(t *testing.T) {
	s := setupSettingsDB(t)

	all, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty map, got %d entries", len(all))
	}
}

// TestSettingStoreJSONValue tests storing and retrieving JSON values.
func TestSettingStoreJSONValue(t *testing.T) {
	s := setupSettingsDB(t)

	jsonVal := `{"enabled":true,"limit":42,"name":"test"}`
	if err := s.Set("config.json", jsonVal); err != nil {
		t.Fatalf("Set JSON failed: %v", err)
	}

	got, err := s.Get("config.json")
	if err != nil {
		t.Fatalf("Get JSON failed: %v", err)
	}
	if got != jsonVal {
		t.Errorf("expected %q, got %q", jsonVal, got)
	}
}

// TestSettingStoreNullValue tests that NULL values are handled correctly.
func TestSettingStoreNullValue(t *testing.T) {
	s := setupSettingsDB(t)

	// Insert a NULL value directly (bypassing Set).
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, NULL)`, "null.key")
	if err != nil {
		t.Fatalf("INSERT NULL failed: %v", err)
	}

	val, err := s.Get("null.key")
	if err != nil {
		t.Fatalf("Get NULL key failed: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for NULL value, got %q", val)
	}
}

// TestSettingStoreLargeValue tests storing a large value.
func TestSettingStoreLargeValue(t *testing.T) {
	s := setupSettingsDB(t)

	// 10KB value
	largeValue := strings.Repeat("a", 10240)
	if err := s.Set("large.key", largeValue); err != nil {
		t.Fatalf("Set large value failed: %v", err)
	}

	got, err := s.Get("large.key")
	if err != nil {
		t.Fatalf("Get large value failed: %v", err)
	}
	if len(got) != 10240 {
		t.Errorf("expected 10240 bytes, got %d", len(got))
	}
}
