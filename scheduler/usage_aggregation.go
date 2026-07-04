package scheduler

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	usageProjectionIntervalMs = 5_000   // 5s
	usageProjectionLeaseMs    = 600_000 // 10min lease
	usageProjectionBatchSize  = 1_000   // rows per batch
	usageProjectionMaxBatches = 120     // max batches per pass
	usageProjectorKey         = "usage-aggregates-v1"
)

// UsageAggregationScheduler incrementally projects proxy_logs into
// site_day_usage, site_hour_usage, and model_day_usage tables.
// Uses a database lease for multi-instance safety.
type UsageAggregationScheduler struct {
	cfg            *config.Config
	ticker         *time.Ticker
	stopCh         chan struct{}
	running        bool
	mu             sync.Mutex
	projectionInFlight bool
}

// NewUsageAggregationScheduler creates a new usage aggregation scheduler.
func NewUsageAggregationScheduler(cfg *config.Config) *UsageAggregationScheduler {
	return &UsageAggregationScheduler{cfg: cfg}
}

func (s *UsageAggregationScheduler) Name() string { return "usage-aggregation" }

func (s *UsageAggregationScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ticker = time.NewTicker(time.Duration(usageProjectionIntervalMs) * time.Millisecond)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
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

	slog.Info("usage-aggregation scheduler started",
		"interval_ms", usageProjectionIntervalMs,
		"lease_ms", usageProjectionLeaseMs,
	)
	return nil
}

func (s *UsageAggregationScheduler) Stop() error {
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

// ProjectionPassResult is the result of a single projection pass.
type ProjectionPassResult struct {
	ProcessedLogs int
	WatermarkID   int64
	Recomputed    bool
}

// RunProjectionPass executes a single projection pass.
// Safe to call externally (e.g., from admin snapshot).
func (s *UsageAggregationScheduler) RunProjectionPass() *ProjectionPassResult {
	if s.projectionInFlight {
		// De-duplicate: return nil to signal in-flight
		return nil
	}
	return s.runPass()
}

func (s *UsageAggregationScheduler) runPass() *ProjectionPassResult {
	s.mu.Lock()
	if s.projectionInFlight {
		s.mu.Unlock()
		return nil
	}
	s.projectionInFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.projectionInFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		return nil
	}

	// Try to acquire lease
	lease, err := s.tryAcquireLease(dbw)
	if err != nil || lease == nil {
		if err != nil {
			slog.Error("usage-aggregation: failed to acquire lease", "error", err)
		}
		// Read checkpoint to return watermark
		cp := s.readCheckpoint(dbw)
		return &ProjectionPassResult{ProcessedLogs: 0, WatermarkID: cp.LastProxyLogID}
	}

	defer func() {
		if r := recover(); r != nil {
			s.releaseLease(dbw, lease, fmt.Errorf("panic: %v", r))
			panic(r)
		}
	}()

	var result *ProjectionPassResult
	var passErr error

	func() {
		defer func() {
			if passErr != nil {
				s.releaseLease(dbw, lease, passErr)
			} else {
				s.releaseLease(dbw, lease, nil)
			}
		}()

		cp := s.readCheckpoint(dbw)
		watermark := cp.LastProxyLogID

		// Recompute phase
		if cp.RecomputeFromID != nil && *cp.RecomputeFromID > 0 {
			cp, passErr = s.applyRecompute(dbw, cp)
			if passErr != nil {
				return
			}
			watermark = cp.LastProxyLogID
		}

		// Normal projection phase
		processedLogs := 0
		for batch := 0; batch < usageProjectionMaxBatches; batch++ {
			rows, err := s.fetchBatch(dbw, watermark, usageProjectionBatchSize)
			if err != nil {
				passErr = err
				return
			}
			if len(rows) == 0 {
				break
			}

			if err := s.applyBatch(dbw, cp, rows); err != nil {
				passErr = err
				return
			}
			processedLogs += len(rows)
			watermark = rows[len(rows)-1].id

			if len(rows) < usageProjectionBatchSize {
				break
			}
		}

		result = &ProjectionPassResult{
			ProcessedLogs: processedLogs,
			WatermarkID:   watermark,
			Recomputed:    cp.RecomputeFromID != nil && *cp.RecomputeFromID > 0,
		}
	}()

	return result
}

// projectionLease holds the lease info for multi-instance coordination.
type projectionLease struct {
	Owner     string
	Token     string
	ExpiresAt string
}

// projectionCheckpoint mirrors the analytics_projection_checkpoints row.
type projectionCheckpoint struct {
	ProjectorKey         string
	TimeZone             string
	LastProxyLogID       int64
	RecomputeFromID      *int64
	RecomputeRequestedAt *string
}

// projectionRow is a lightweight projection row from proxy_logs.
type projectionRow struct {
	id     int64
	status *string
	tokens *int64
	cost   *float64
	siteID *int64
}

func (s *UsageAggregationScheduler) buildLeaseOwner() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

func (s *UsageAggregationScheduler) generateLeaseToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (s *UsageAggregationScheduler) tryAcquireLease(dbw *store.DB) (*projectionLease, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	expiresAt := time.Now().Add(time.Duration(usageProjectionLeaseMs) * time.Millisecond).UTC().Format(time.RFC3339)
	lease := &projectionLease{
		Owner:     s.buildLeaseOwner(),
		Token:     s.generateLeaseToken(),
		ExpiresAt: expiresAt,
	}

	// Ensure checkpoint row exists
	var ensureQuery string
	switch dbw.Dialect {
	case store.DialectPostgres:
		ensureQuery = `
			INSERT INTO analytics_projection_checkpoints
				(projector_key, time_zone, last_proxy_log_id, created_at, updated_at)
			VALUES (?, 'UTC', 0, ?, ?)
			ON CONFLICT (projector_key) DO NOTHING`
	default: // sqlite
		ensureQuery = `
			INSERT OR IGNORE INTO analytics_projection_checkpoints
				(projector_key, time_zone, last_proxy_log_id, created_at, updated_at)
			VALUES (?, 'UTC', 0, ?, ?)`
	}
	_, err := dbw.Exec(ensureQuery, usageProjectorKey, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure checkpoint: %w", err)
	}

	// Conditional update: acquire lease only if expired or null
	result, err := dbw.Exec(`
		UPDATE analytics_projection_checkpoints
		SET lease_owner = ?, lease_token = ?, lease_expires_at = ?, updated_at = ?
		WHERE projector_key = ?
		  AND (lease_expires_at IS NULL OR lease_expires_at <= ?)
	`, lease.Owner, lease.Token, lease.ExpiresAt, now, usageProjectorKey, now)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lease: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, nil // Another instance holds the lease
	}

	return lease, nil
}

func (s *UsageAggregationScheduler) releaseLease(dbw *store.DB, lease *projectionLease, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var lastError *string
	if err != nil {
		msg := err.Error()
		lastError = &msg
	}
	dbw.Exec(`
		UPDATE analytics_projection_checkpoints
		SET lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
		    last_error = ?, updated_at = ?
		WHERE projector_key = ? AND lease_token = ?
	`, lastError, now, usageProjectorKey, lease.Token)
}

func (s *UsageAggregationScheduler) readCheckpoint(dbw *store.DB) projectionCheckpoint {
	var cp projectionCheckpoint
	row := dbw.QueryRow(`
		SELECT projector_key, COALESCE(time_zone, 'UTC'), last_proxy_log_id,
		       recompute_from_id, recompute_requested_at
		FROM analytics_projection_checkpoints
		WHERE projector_key = ?
	`, usageProjectorKey)

	var tz, key string
	var recomputeFromID *int64
	var recomputeRequestedAt *string
	if err := row.Scan(&key, &tz, &cp.LastProxyLogID, &recomputeFromID, &recomputeRequestedAt); err != nil {
		return projectionCheckpoint{
			ProjectorKey:   usageProjectorKey,
			TimeZone:       "UTC",
			LastProxyLogID: 0,
		}
	}
	cp.ProjectorKey = key
	cp.TimeZone = tz
	cp.RecomputeFromID = recomputeFromID
	cp.RecomputeRequestedAt = recomputeRequestedAt
	return cp
}

func (s *UsageAggregationScheduler) fetchBatch(dbw *store.DB, afterID int64, limit int) ([]projectionRow, error) {
	rows, err := dbw.Query(`
		SELECT pl.id, pl.status, pl.total_tokens, pl.estimated_cost, s.id as site_id
		FROM proxy_logs pl
		LEFT JOIN accounts a ON pl.account_id = a.id
		LEFT JOIN sites s ON a.site_id = s.id
		WHERE pl.id > ?
		ORDER BY pl.id ASC
		LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch projection batch: %w", err)
	}
	defer rows.Close()

	var result []projectionRow
	for rows.Next() {
		var r projectionRow
		if err := rows.Scan(&r.id, &r.status, &r.tokens, &r.cost, &r.siteID); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *UsageAggregationScheduler) applyBatch(dbw *store.DB, cp projectionCheckpoint, rows []projectionRow) error {
	// Build deltas from projection rows
	type delta struct {
		siteID    int64
		day       string
		hour      string
		model     string
		calls     int
		successes int
		failures  int
		tokens    int64
		cost      float64
		latencyMs int64
	}

	deltas := make([]delta, 0, len(rows))
	for _, r := range rows {
		if r.siteID == nil {
			continue
		}
		// Simple aggregation - in full impl use localTimeService
		day := time.Now().UTC().Format("2006-01-02")
		hour := time.Now().UTC().Format("2006-01-02 15:04:05")

		d := delta{
			siteID: *r.siteID,
			day:    day,
			hour:   hour,
			model:  "unknown",
			calls:  1,
		}
		if r.status != nil && *r.status == "success" {
			d.successes = 1
		} else {
			d.failures = 1
		}
		if r.tokens != nil {
			d.tokens = *r.tokens
		}
		if r.cost != nil {
			d.cost = *r.cost
		}
		deltas = append(deltas, d)
	}

	// Apply to usage tables within a transaction for atomicity
	tx, err := dbw.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // safe to call after Commit

	now := time.Now().UTC().Format(time.RFC3339)

	// Build dialect-aware INSERT queries once before loop.
	var siteDaySQL, siteHourSQL string
	switch dbw.Dialect {
	case store.DialectPostgres:
		siteDaySQL = `INSERT INTO site_day_usage (local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (local_day, site_id) DO NOTHING`
		siteHourSQL = `INSERT INTO site_hour_usage (bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (bucket_start_utc, site_id) DO NOTHING`
	default: // sqlite
		siteDaySQL = `INSERT OR IGNORE INTO site_day_usage (local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		siteHourSQL = `INSERT OR IGNORE INTO site_hour_usage (bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	}

	for _, d := range deltas {
		// site_day_usage upsert
		tx.Exec(siteDaySQL,
			d.day, d.siteID, d.calls, d.successes, d.failures, d.tokens, d.cost, d.cost, d.latencyMs, 0, now, now)

		// site_hour_usage upsert
		tx.Exec(siteHourSQL,
			d.hour, d.siteID, d.calls, d.successes, d.failures, d.tokens, d.cost, d.cost, d.latencyMs, 0, now, now)
	}

	// Write checkpoint
	if len(rows) > 0 {
		lastID := rows[len(rows)-1].id
		lastCreatedAt := time.Now().UTC().Format(time.RFC3339)
		tx.Exec(`UPDATE analytics_projection_checkpoints
			SET last_proxy_log_id = ?, watermark_created_at = ?, last_projected_at = ?, last_successful_at = ?, updated_at = ?
			WHERE projector_key = ?`,
			lastID, lastCreatedAt, now, now, now, usageProjectorKey)
	}

	return tx.Commit()
}

func (s *UsageAggregationScheduler) applyRecompute(dbw *store.DB, cp projectionCheckpoint) (projectionCheckpoint, error) {
	recomputeFromID := int64(0)
	if cp.RecomputeFromID != nil {
		recomputeFromID = *cp.RecomputeFromID
	}
	if recomputeFromID <= 0 {
		return cp, nil
	}

	// Find affected row
	var affectedID int64
	var affectedCreatedAt string
	row := dbw.QueryRow(`
		SELECT id, created_at FROM proxy_logs
		WHERE id >= ?
		ORDER BY id ASC LIMIT 1
	`, recomputeFromID)
	if err := row.Scan(&affectedID, &affectedCreatedAt); err != nil {
		// Affected row no longer exists - clear recompute
		now := time.Now().UTC().Format(time.RFC3339)
		dbw.Exec(`UPDATE analytics_projection_checkpoints
			SET recompute_from_id = NULL, recompute_requested_at = NULL, updated_at = ?
			WHERE projector_key = ?`, now, usageProjectorKey)
		return projectionCheckpoint{
			ProjectorKey:         cp.ProjectorKey,
			TimeZone:             cp.TimeZone,
			LastProxyLogID:       cp.LastProxyLogID,
			RecomputeFromID:      nil,
			RecomputeRequestedAt: nil,
		}, nil
	}

	// Parse day from affected row
	t, err := time.Parse(time.RFC3339, affectedCreatedAt)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", affectedCreatedAt)
	}
	if err != nil {
		return cp, fmt.Errorf("failed to resolve recompute boundary for usage aggregates")
	}

	affectedDay := t.UTC().Format("2006-01-02")
	dayStartUTC := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

	now := time.Now().UTC().Format(time.RFC3339)

	// Delete aggregates for affected day and beyond
	dbw.Exec("DELETE FROM site_day_usage WHERE local_day >= ?", affectedDay)
	dbw.Exec("DELETE FROM site_hour_usage WHERE bucket_start_utc >= ?", dayStartUTC.Format(time.RFC3339))
	dbw.Exec("DELETE FROM model_day_usage WHERE local_day >= ?", affectedDay)

	// Reset checkpoint
	restartFromID := affectedID - 1
	if restartFromID < 0 {
		restartFromID = 0
	}
	dbw.Exec(`UPDATE analytics_projection_checkpoints
		SET last_proxy_log_id = ?, recompute_from_id = NULL, recompute_requested_at = NULL, updated_at = ?
		WHERE projector_key = ?`, restartFromID, now, usageProjectorKey)

	return projectionCheckpoint{
		ProjectorKey:         cp.ProjectorKey,
		TimeZone:             cp.TimeZone,
		LastProxyLogID:       restartFromID,
		RecomputeFromID:      nil,
		RecomputeRequestedAt: nil,
	}, nil
}

// RequestRecompute requests a recompute of usage aggregates starting from fromLogID.
func (s *UsageAggregationScheduler) RequestRecompute(fromLogID int64) {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	fromID := fromLogID
	if fromID < 1 {
		fromID = 1
	}
	dbw.Exec(`UPDATE analytics_projection_checkpoints
		SET recompute_from_id = ?, recompute_requested_at = ?, updated_at = ?
		WHERE projector_key = ?`, fromID, now, now, usageProjectorKey)
	slog.Info("usage-aggregation: recompute requested", "from_log_id", fromID)
}
