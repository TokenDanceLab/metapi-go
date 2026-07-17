package scheduler

import (
	"sync/atomic"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/store"
)

func resetDefaultSiteAnnouncementSyncFunc(t *testing.T) {
	t.Helper()
	previous := getDefaultSiteAnnouncementSyncFunc()
	t.Cleanup(func() {
		SetDefaultSiteAnnouncementSyncFunc(previous)
	})
	SetDefaultSiteAnnouncementSyncFunc(nil)
}

func TestSiteAnnouncementScheduler_SyncFuncInvoked(t *testing.T) {
	resetDefaultSiteAnnouncementSyncFunc(t)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	var calls atomic.Int64
	s := NewSiteAnnouncementScheduler(testConfig())
	s.SetSyncFunc(func(sqlDB *sqlx.DB) SiteAnnouncementSyncResult {
		if sqlDB == nil {
			t.Error("sync func received nil db")
		}
		calls.Add(1)
		// Honest empty-sites style result — no invented inserts.
		return SiteAnnouncementSyncResult{
			ScannedSites: 0,
			Inserted:     0,
			Updated:      0,
		}
	})

	s.runSyncLocked(db)

	if got := calls.Load(); got != 1 {
		t.Fatalf("SyncFunc calls = %d, want 1", got)
	}
}

func TestSiteAnnouncementScheduler_NilSyncFuncResidualScan(t *testing.T) {
	resetDefaultSiteAnnouncementSyncFunc(t)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Empty sites path: residual scan must complete without SyncFunc.
	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	s := NewSiteAnnouncementScheduler(testConfig())
	if s.syncFunc != nil {
		t.Fatal("expected nil SyncFunc for residual path")
	}

	// Must not panic; residual scan enumerates zero active sites.
	s.runSyncLocked(db)
}

func TestSiteAnnouncementScheduler_DefaultSyncFuncPickedUp(t *testing.T) {
	resetDefaultSiteAnnouncementSyncFunc(t)

	var calls atomic.Int64
	SetDefaultSiteAnnouncementSyncFunc(func(db *sqlx.DB) SiteAnnouncementSyncResult {
		calls.Add(1)
		return SiteAnnouncementSyncResult{ScannedSites: 2}
	})

	s := NewSiteAnnouncementScheduler(testConfig())
	if s.syncFunc == nil {
		t.Fatal("NewSiteAnnouncementScheduler did not pick up default SyncFunc")
	}

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	s.runSyncLocked(db)
	if got := calls.Load(); got != 1 {
		t.Fatalf("default SyncFunc calls = %d, want 1", got)
	}
}

func TestSiteAnnouncementScheduler_EmptySitesWithSyncFunc(t *testing.T) {
	resetDefaultSiteAnnouncementSyncFunc(t)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	s := NewSiteAnnouncementScheduler(testConfig())
	s.SetSyncFunc(func(sqlDB *sqlx.DB) SiteAnnouncementSyncResult {
		// Honest empty-sites result: no invented inserts/updates.
		return SiteAnnouncementSyncResult{
			ScannedSites: 0,
			Inserted:     0,
			Updated:      0,
			Failed:       0,
		}
	})
	s.runSyncLocked(db)
}
