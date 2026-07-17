package scheduler

import (
	"sync/atomic"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

func TestSiteAnnouncementSyncFuncInvoked(t *testing.T) {
	t.Cleanup(func() { SetDefaultSiteAnnouncementSyncFunc(nil) })

	var called atomic.Int64
	SetDefaultSiteAnnouncementSyncFunc(func(db *sqlx.DB) SiteAnnouncementSyncResult {
		called.Add(1)
		return SiteAnnouncementSyncResult{ScannedSites: 2, Inserted: 1}
	})

	s := NewSiteAnnouncementScheduler(&config.Config{})
	if s.syncFunc == nil {
		t.Fatal("expected default SyncFunc to be picked up")
	}
	// Direct locked path without DB/lease: call syncFunc shape
	result := s.syncFunc(nil)
	if result.ScannedSites != 2 || result.Inserted != 1 {
		t.Fatalf("result = %+v", result)
	}
	if called.Load() != 1 {
		t.Fatalf("called=%d", called.Load())
	}
}

func TestSiteAnnouncementResidualWhenNil(t *testing.T) {
	t.Cleanup(func() { SetDefaultSiteAnnouncementSyncFunc(nil) })
	SetDefaultSiteAnnouncementSyncFunc(nil)
	s := NewSiteAnnouncementScheduler(&config.Config{})
	s.SetSyncFunc(nil)
	if s.syncFunc != nil {
		t.Fatal("expected nil SyncFunc")
	}
}
