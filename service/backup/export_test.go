package backup_test

import (
	"errors"
	"testing"

	backupsvc "github.com/tokendancelab/metapi-go/service/backup"
	"github.com/tokendancelab/metapi-go/store"
)

func setupBackupServiceTestDB(b testing.TB) *store.DB {
	b.Helper()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	b.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		b.Fatalf("migrate db: %v", err)
	}
	return db
}

func setExportLimitsForTest(t *testing.T, maxRows int, maxCellBytes int, maxPayloadBytes int64) {
	t.Helper()
	oldMaxRows := backupsvc.MaxExportRowsPerTable
	oldMaxCellBytes := backupsvc.MaxExportCellBytes
	oldMaxPayloadBytes := backupsvc.MaxExportPayloadBytes
	backupsvc.MaxExportRowsPerTable = maxRows
	backupsvc.MaxExportCellBytes = maxCellBytes
	backupsvc.MaxExportPayloadBytes = maxPayloadBytes
	t.Cleanup(func() {
		backupsvc.MaxExportRowsPerTable = oldMaxRows
		backupsvc.MaxExportCellBytes = oldMaxCellBytes
		backupsvc.MaxExportPayloadBytes = oldMaxPayloadBytes
	})
}

func TestBuildPayloadExportsSettingsRows(t *testing.T) {
	db := setupBackupServiceTestDB(t)
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	payload, err := backupsvc.BuildPayload(db.DB, "preferences")
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	if payload["type"] != "preferences" {
		t.Fatalf("type = %v, want preferences", payload["type"])
	}

	tables, ok := payload["tables"].(map[string]any)
	if !ok {
		t.Fatalf("tables = %#v, want object", payload["tables"])
	}
	settingsRows, ok := tables["settings"].([]map[string]any)
	if !ok || len(settingsRows) != 1 {
		t.Fatalf("settings rows = %#v, want one row", tables["settings"])
	}
	if settingsRows[0]["key"] != "theme" || settingsRows[0]["value"] != `"dark"` {
		t.Fatalf("settings row = %#v, want seeded row", settingsRows[0])
	}
}

func TestBuildPayloadRejectsOversizedProxyFileContent(t *testing.T) {
	setExportLimitsForTest(t, 50_000, 8, 64<<20)
	db := setupBackupServiceTestDB(t)
	_, err := db.Exec(`INSERT INTO proxy_files
		(public_id, owner_type, owner_id, filename, mime_type, byte_size, sha256, content_base64)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"file-1", "response", "resp-1", "payload.txt", "text/plain", 12, "sha", "123456789",
	)
	if err != nil {
		t.Fatalf("insert proxy file: %v", err)
	}

	_, err = backupsvc.BuildPayload(db.DB, "accounts")
	var limitErr backupsvc.ExportLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ExportLimitError", err)
	}
}

func TestBuildPayloadRejectsTooManyRows(t *testing.T) {
	setExportLimitsForTest(t, 1, 4<<20, 64<<20)
	db := setupBackupServiceTestDB(t)
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "language", `"zh"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	_, err := backupsvc.BuildPayload(db.DB, "preferences")
	var limitErr backupsvc.ExportLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ExportLimitError", err)
	}
}

func TestBuildPayloadRejectsEstimatedPayloadOverLimit(t *testing.T) {
	setExportLimitsForTest(t, 50_000, 4<<20, 32)
	db := setupBackupServiceTestDB(t)
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	_, err := backupsvc.BuildPayload(db.DB, "preferences")
	var limitErr backupsvc.ExportLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ExportLimitError", err)
	}
}

func TestTablesForExportTypeRejectsInvalidType(t *testing.T) {
	_, _, err := backupsvc.TablesForExportType("invalid")
	if !errors.Is(err, backupsvc.ErrInvalidExportType) {
		t.Fatalf("error = %v, want ErrInvalidExportType", err)
	}
}

func TestQueryTableAsJSONRejectsUnknownTable(t *testing.T) {
	db := setupBackupServiceTestDB(t)

	_, err := backupsvc.QueryTableAsJSON(db.DB, "settings; DROP TABLE settings")
	if err == nil {
		t.Fatal("expected unknown table error")
	}
}

func BenchmarkBuildPayloadPreferences(b *testing.B) {
	db := setupBackupServiceTestDB(b)
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		b.Fatalf("insert setting: %v", err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := backupsvc.BuildPayload(db.DB, "preferences"); err != nil {
			b.Fatalf("BuildPayload: %v", err)
		}
	}
}

func BenchmarkBuildPayloadAccountsWithProxyFile(b *testing.B) {
	db := setupBackupServiceTestDB(b)
	if _, err := db.Exec(`INSERT INTO proxy_files
		(public_id, owner_type, owner_id, filename, mime_type, byte_size, sha256, content_base64)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"file-1", "response", "resp-1", "payload.txt", "text/plain", 12, "sha", "cGF5bG9hZA==",
	); err != nil {
		b.Fatalf("insert proxy file: %v", err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := backupsvc.BuildPayload(db.DB, "accounts"); err != nil {
			b.Fatalf("BuildPayload: %v", err)
		}
	}
}

func BenchmarkBuildPayloadAllMinimal(b *testing.B) {
	db := setupBackupServiceTestDB(b)
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		b.Fatalf("insert setting: %v", err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := backupsvc.BuildPayload(db.DB, "all"); err != nil {
			b.Fatalf("BuildPayload: %v", err)
		}
	}
}
