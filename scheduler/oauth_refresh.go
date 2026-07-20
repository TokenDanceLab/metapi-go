package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/oauth"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	oauthRefreshSchedulerIntervalMs    = 60_000 // 60s, matches TS original
	oauthRefreshSchedulerMinIntervalMs = 60_000

	// Default lead time when provider not listed (5 min).
	defaultOauthRefreshLeadMs = 5 * 60 * 1000
)

// leadMsByProvider mirrors OAUTH_REFRESH_LEAD_BY_PROVIDER in the TS original.
// Tokens are refreshed when tokenExpiresAt - now <= lead time.
var leadMsByProvider = map[string]int64{
	"codex":        5 * 24 * 60 * 60 * 1000, // 5 days
	"claude":       4 * 60 * 60 * 1000,      // 4 hours
	"gemini-cli":   5 * 60 * 1000,           // 5 minutes
	"antigravity":  5 * 60 * 1000,           // 5 minutes
}

// OAuthRefreshScheduler periodically scans OAuth accounts and refreshes
// tokens nearing expiry. Mirrors the TS oauthRefreshScheduler.ts.
type OAuthRefreshScheduler struct {
	cfg          *config.Config
	ticker       *time.Ticker
	stopCh       chan struct{}
	running      bool
	mu           sync.Mutex
	passInFlight bool
}

// NewOAuthRefreshScheduler creates a new OAuth token refresh scheduler.
func NewOAuthRefreshScheduler(cfg *config.Config) *OAuthRefreshScheduler {
	return &OAuthRefreshScheduler{cfg: cfg}
}

func (s *OAuthRefreshScheduler) Name() string { return "oauth-refresh" }

func (s *OAuthRefreshScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intervalMs := maxInt(oauthRefreshSchedulerIntervalMs, oauthRefreshSchedulerMinIntervalMs)
	interval := time.Duration(intervalMs) * time.Millisecond

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run.
	go s.runPass()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runPass()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("oauth-refresh scheduler started", "interval_ms", intervalMs)
	return nil
}

func (s *OAuthRefreshScheduler) Stop() error {
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

// OAuthRefreshResult is the outcome of a single refresh pass.
type OAuthRefreshResult struct {
	Scanned             int
	Refreshed           int
	Failed              int
	Skipped             int
	RefreshedAccountIDs []int64
	FailedAccountIDs    []int64
}

func getOauthRefreshLeadMs(provider string) int64 {
	if v, ok := leadMsByProvider[provider]; ok {
		return v
	}
	return defaultOauthRefreshLeadMs
}

func (s *OAuthRefreshScheduler) runPass() {
	s.mu.Lock()
	if s.passInFlight {
		s.mu.Unlock()
		return
	}
	s.passInFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.passInFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		slog.Warn("oauth-refresh: database not initialized, skipping pass")
		return
	}

	nowMs := time.Now().UnixMilli()

	var rows []struct {
		Account store.Account `db:"a"`
		Site    store.Site    `db:"s"`
	}

	err := dbw.Select(&rows,
		`SELECT a.*, s.* FROM accounts a
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE a.oauth_provider IS NOT NULL`)
	if err != nil {
		slog.Warn("oauth-refresh: query failed", "error", err)
		return
	}

	result := &OAuthRefreshResult{
		Scanned: len(rows),
	}

	for _, row := range rows {
		// Skip if account or site is not active.
		if row.Account.Status != "active" || row.Site.Status != "active" {
			result.Skipped++
			continue
		}

		oauthInfo := oauth.GetOauthInfoFromAccount(&row.Account)
		if oauthInfo == nil || oauthInfo.RefreshToken == "" {
			result.Skipped++
			continue
		}

		if oauthInfo.TokenExpiresAt <= 0 {
			result.Skipped++
			continue
		}

		leadMs := getOauthRefreshLeadMs(oauthInfo.Provider)
		if oauthInfo.TokenExpiresAt-nowMs > leadMs {
			result.Skipped++
			continue
		}

		// Token is within lead window — refresh.
		_, err := oauth.RefreshAccessTokenSingleflight(row.Account.ID)
		if err != nil {
			result.Failed++
			result.FailedAccountIDs = append(result.FailedAccountIDs, row.Account.ID)
			slog.Warn("oauth-refresh: refresh failed",
				"account_id", row.Account.ID,
				"provider", oauthInfo.Provider,
				"error", err)
		} else {
			result.Refreshed++
			result.RefreshedAccountIDs = append(result.RefreshedAccountIDs, row.Account.ID)
		}
	}

	if result.Refreshed > 0 || result.Failed > 0 {
		slog.Info("oauth-refresh pass complete",
			"scanned", result.Scanned,
			"refreshed", result.Refreshed,
			"failed", result.Failed,
			"skipped", result.Skipped)
	}
}
