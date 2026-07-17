package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const defaultSiteAnnouncementIntervalMs = 15 * 60 * 1000 // 15 minutes

// SiteAnnouncementScheduler polls site announcements periodically.
type SiteAnnouncementScheduler struct {
	cfg      *config.Config
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
	inFlight bool
}

// NewSiteAnnouncementScheduler creates a new site announcement poller.
func NewSiteAnnouncementScheduler(cfg *config.Config) *SiteAnnouncementScheduler {
	return &SiteAnnouncementScheduler{cfg: cfg}
}

func (s *SiteAnnouncementScheduler) Name() string { return "site-announcement" }

func (s *SiteAnnouncementScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	interval := time.Duration(maxInt64(defaultSiteAnnouncementIntervalMs, 10_000)) * time.Millisecond
	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
	go s.runSync()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runSync()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("site-announcement scheduler started", "interval_ms", interval.Milliseconds())
	return nil
}

func (s *SiteAnnouncementScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	if s.ticker != nil {
		s.ticker.Stop()
	}
	if s.stopCh != nil {
		close(s.stopCh)
	}
	return nil
}

func (s *SiteAnnouncementScheduler) runSync() {
	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runSyncLocked(dbw)
	})
}

func (s *SiteAnnouncementScheduler) runSyncLocked(dbw *store.DB) {
	// Residual honesty (#246): this scheduler pass is scan-only.
	// What runs: enumerate active sites under the scheduler lease and log the
	// count. What does NOT run: platform adapter GetSiteAnnouncements, DB
	// insert/update into site_announcements, notifications, or events.
	// Admin path POST /api/site-announcements/sync already implements real
	// syncSiteAnnouncements; this ticker does not call it. "sync complete"
	// means "enumeration complete", not that announcements were fetched.
	slog.Info("site-announcement: residual scan (no platform sync)")

	rows, err := dbw.Query("SELECT id, platform, url FROM sites WHERE status = 'active'")
	if err != nil {
		slog.Error("site-announcement: failed to query sites", "error", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int64
		var platform, url string
		if err := rows.Scan(&id, &platform, &url); err != nil {
			continue
		}
		_ = id
		_ = platform
		_ = url
		// Residual (#246): site row is counted only.
		// TODO residual (not wired): call platform-specific announcement sync
		// (reuse admin syncSiteAnnouncements / adapter.GetSiteAnnouncements).
		// Do not invent inserted/updated success without real upstream calls.
		count++
	}

	slog.Info("site-announcement: residual scan complete (no announcements written)", "sites", count)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
