package scheduler

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	cfg                *config.Config
	ticker             *time.Ticker
	stopCh             chan struct{}
	running            bool
	mu                 sync.Mutex
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
//
// OrphanLogs counts proxy_logs without a site join (missing account/site).
// They still advance the watermark so projection does not stall, but they
// do not contribute to site/model aggregates (P0-555 residual honesty).
type ProjectionPassResult struct {
	ProcessedLogs int
	OrphanLogs    int
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
			slog.Error("usage aggregation pass panicked", "panic", r)
			s.releaseLease(dbw, lease, fmt.Errorf("panic: %v", r))
			// Do NOT re-panic — this runs inside a goroutine launched by runPass().
			// Re-panicking would crash the entire server.
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
		orphanLogs := 0
		for batch := 0; batch < usageProjectionMaxBatches; batch++ {
			rows, err := s.fetchBatch(dbw, watermark, usageProjectionBatchSize)
			if err != nil {
				passErr = err
				return
			}
			if len(rows) == 0 {
				break
			}

			batchOrphans, err := s.applyBatch(dbw, cp, rows)
			if err != nil {
				passErr = err
				return
			}
			processedLogs += len(rows)
			orphanLogs += batchOrphans
			watermark = rows[len(rows)-1].id

			if len(rows) < usageProjectionBatchSize {
				break
			}
		}

		if orphanLogs > 0 {
			slog.Info("usage-aggregation: orphan proxy_logs skipped site buckets",
				"orphan_logs", orphanLogs,
				"processed_logs", processedLogs,
				"watermark_id", watermark,
			)
		}

		result = &ProjectionPassResult{
			ProcessedLogs: processedLogs,
			OrphanLogs:    orphanLogs,
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
	id               int64
	status           *string
	promptTokens     *int64
	completionTokens *int64
	tokens           *int64
	cost             *float64
	siteID           *int64
	platform         *string
	model            *string
	latencyMs        *int64
	createdAt        *string
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
		SELECT pl.id, pl.status, pl.prompt_tokens, pl.completion_tokens, pl.total_tokens, pl.estimated_cost,
		       s.id as site_id, s.platform, pl.model_actual, pl.latency_ms, pl.created_at
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
		if err := rows.Scan(
			&r.id, &r.status, &r.promptTokens, &r.completionTokens, &r.tokens, &r.cost,
			&r.siteID, &r.platform, &r.model, &r.latencyMs, &r.createdAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan projection row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate projection rows: %w", err)
	}
	return result, nil
}

func (s *UsageAggregationScheduler) applyBatch(dbw *store.DB, cp projectionCheckpoint, rows []projectionRow) (orphanCount int, err error) {
	// Aggregate deltas by key so one batch never multiplies the same bucket
	// with repeated single-row upserts (correctness + fewer writes).
	type dayKey struct {
		day    string
		siteID int64
	}
	type hourKey struct {
		hour   string
		siteID int64
	}
	type modelKey struct {
		day    string
		siteID int64
		model  string
	}
	type bucketDelta struct {
		calls         int
		successes     int
		failures      int
		tokens        int64
		summarySpend  float64
		siteSpend     float64
		modelSpend    float64
		latencyMs     int64
		latencyCount  int
	}

	dayDeltas := make(map[dayKey]*bucketDelta)
	hourDeltas := make(map[hourKey]*bucketDelta)
	modelDeltas := make(map[modelKey]*bucketDelta)

	for _, r := range rows {
		if r.siteID == nil {
			// Orphan logs without a site join still advance the watermark below;
			// they cannot contribute to site/model aggregates.
			orphanCount++
			continue
		}
		logTime := projectionTimestamp(r.createdAt)
		day := logTime.Format("2006-01-02")
		hour := logTime.Truncate(time.Hour).Format(time.RFC3339)
		model := "unknown"
		if r.model != nil && strings.TrimSpace(*r.model) != "" {
			model = strings.TrimSpace(*r.model)
		}
		platform := ""
		if r.platform != nil {
			platform = *r.platform
		}

		tokens := effectiveTokenCount(r.tokens, r.promptTokens, r.completionTokens)
		summarySpend := resolveSummarySpend(r.cost, tokens, platform)
		siteSpend := resolveSiteSpend(r.cost, tokens, platform)
		modelSpend := resolveModelSpend(r.cost, tokens)

		successes := 0
		failures := 1
		if r.status != nil && *r.status == "success" {
			successes = 1
			failures = 0
		}
		latencyMs := int64(0)
		latencyCount := 0
		if r.latencyMs != nil && *r.latencyMs > 0 {
			latencyMs = *r.latencyMs
			latencyCount = 1
		}

		dk := dayKey{day: day, siteID: *r.siteID}
		dd := dayDeltas[dk]
		if dd == nil {
			dd = &bucketDelta{}
			dayDeltas[dk] = dd
		}
		dd.calls++
		dd.successes += successes
		dd.failures += failures
		dd.tokens += tokens
		dd.summarySpend += summarySpend
		dd.siteSpend += siteSpend
		dd.latencyMs += latencyMs
		dd.latencyCount += latencyCount

		hk := hourKey{hour: hour, siteID: *r.siteID}
		hd := hourDeltas[hk]
		if hd == nil {
			hd = &bucketDelta{}
			hourDeltas[hk] = hd
		}
		hd.calls++
		hd.successes += successes
		hd.failures += failures
		hd.tokens += tokens
		hd.summarySpend += summarySpend
		hd.siteSpend += siteSpend
		hd.latencyMs += latencyMs
		hd.latencyCount += latencyCount

		mk := modelKey{day: day, siteID: *r.siteID, model: model}
		md := modelDeltas[mk]
		if md == nil {
			md = &bucketDelta{}
			modelDeltas[mk] = md
		}
		md.calls++
		md.successes += successes
		md.failures += failures
		md.tokens += tokens
		md.modelSpend += modelSpend
		md.latencyMs += latencyMs
		md.latencyCount += latencyCount
	}

	// Apply to usage tables within a transaction for atomicity
	tx, err := dbw.Beginx()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // safe to call after Commit

	now := time.Now().UTC().Format(time.RFC3339)

	// Build dialect-aware INSERT queries once before loop.
	siteDaySQL := `INSERT INTO site_day_usage (local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (local_day, site_id) DO UPDATE SET
			total_calls = site_day_usage.total_calls + excluded.total_calls,
			success_calls = site_day_usage.success_calls + excluded.success_calls,
			failed_calls = site_day_usage.failed_calls + excluded.failed_calls,
			total_tokens = site_day_usage.total_tokens + excluded.total_tokens,
			total_summary_spend = site_day_usage.total_summary_spend + excluded.total_summary_spend,
			total_site_spend = site_day_usage.total_site_spend + excluded.total_site_spend,
			total_latency_ms = site_day_usage.total_latency_ms + excluded.total_latency_ms,
			latency_count = site_day_usage.latency_count + excluded.latency_count,
			updated_at = excluded.updated_at`
	siteHourSQL := `INSERT INTO site_hour_usage (bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (bucket_start_utc, site_id) DO UPDATE SET
			total_calls = site_hour_usage.total_calls + excluded.total_calls,
			success_calls = site_hour_usage.success_calls + excluded.success_calls,
			failed_calls = site_hour_usage.failed_calls + excluded.failed_calls,
			total_tokens = site_hour_usage.total_tokens + excluded.total_tokens,
			total_summary_spend = site_hour_usage.total_summary_spend + excluded.total_summary_spend,
			total_site_spend = site_hour_usage.total_site_spend + excluded.total_site_spend,
			total_latency_ms = site_hour_usage.total_latency_ms + excluded.total_latency_ms,
			latency_count = site_hour_usage.latency_count + excluded.latency_count,
			updated_at = excluded.updated_at`
	modelDaySQL := `INSERT INTO model_day_usage (local_day, site_id, model, total_calls, success_calls, failed_calls, total_tokens, total_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (local_day, site_id, model) DO UPDATE SET
			total_calls = model_day_usage.total_calls + excluded.total_calls,
			success_calls = model_day_usage.success_calls + excluded.success_calls,
			failed_calls = model_day_usage.failed_calls + excluded.failed_calls,
			total_tokens = model_day_usage.total_tokens + excluded.total_tokens,
			total_spend = model_day_usage.total_spend + excluded.total_spend,
			total_latency_ms = model_day_usage.total_latency_ms + excluded.total_latency_ms,
			latency_count = model_day_usage.latency_count + excluded.latency_count,
			updated_at = excluded.updated_at`

	for k, d := range dayDeltas {
		if _, err := tx.Exec(tx.Rebind(siteDaySQL),
			k.day, k.siteID, d.calls, d.successes, d.failures, d.tokens, d.summarySpend, d.siteSpend, d.latencyMs, d.latencyCount, now, now); err != nil {
			return 0, fmt.Errorf("failed to upsert site_day_usage: %w", err)
		}
	}
	for k, d := range hourDeltas {
		if _, err := tx.Exec(tx.Rebind(siteHourSQL),
			k.hour, k.siteID, d.calls, d.successes, d.failures, d.tokens, d.summarySpend, d.siteSpend, d.latencyMs, d.latencyCount, now, now); err != nil {
			return 0, fmt.Errorf("failed to upsert site_hour_usage: %w", err)
		}
	}
	for k, d := range modelDeltas {
		if _, err := tx.Exec(tx.Rebind(modelDaySQL),
			k.day, k.siteID, k.model, d.calls, d.successes, d.failures, d.tokens, d.modelSpend, d.latencyMs, d.latencyCount, now, now); err != nil {
			return 0, fmt.Errorf("failed to upsert model_day_usage: %w", err)
		}
	}

	// Write checkpoint for every fetched row (including orphans) so the
	// watermark never re-projects the same proxy_log id range.
	if len(rows) > 0 {
		lastID := rows[len(rows)-1].id
		lastCreatedAt := time.Now().UTC().Format(time.RFC3339)
		if rows[len(rows)-1].createdAt != nil && strings.TrimSpace(*rows[len(rows)-1].createdAt) != "" {
			lastCreatedAt = strings.TrimSpace(*rows[len(rows)-1].createdAt)
		}
		if _, err := tx.Exec(tx.Rebind(`UPDATE analytics_projection_checkpoints
			SET last_proxy_log_id = ?, watermark_created_at = ?, last_projected_at = ?, last_successful_at = ?, updated_at = ?
			WHERE projector_key = ?`),
			lastID, lastCreatedAt, now, now, now, usageProjectorKey); err != nil {
			return 0, fmt.Errorf("failed to update projection checkpoint: %w", err)
		}
	}

	return orphanCount, tx.Commit()
}

// effectiveTokenCount prefers total_tokens when present and positive; otherwise
// falls back to prompt+completion so partial upstream payloads still count.
func effectiveTokenCount(total, prompt, completion *int64) int64 {
	var totalVal, promptVal, completionVal int64
	if total != nil && *total > 0 {
		totalVal = *total
	}
	if prompt != nil && *prompt > 0 {
		promptVal = *prompt
	}
	if completion != nil && *completion > 0 {
		completionVal = *completion
	}
	if totalVal > 0 {
		return totalVal
	}
	sum := promptVal + completionVal
	if sum > 0 {
		return sum
	}
	return 0
}

func fallbackTokenCost(tokens int64, platform string) float64 {
	if tokens <= 0 {
		return 0
	}
	// Veloera-style platforms bill roughly tokens/1e6; other NewAPI-like
	// platforms use the historical tokens/5e5 fallback from the TS service.
	if strings.EqualFold(strings.TrimSpace(platform), "veloera") {
		return float64(tokens) / 1_000_000
	}
	return float64(tokens) / 500_000
}

func resolveSummarySpend(explicit *float64, tokens int64, platform string) float64 {
	if explicit != nil && *explicit > 0 {
		return *explicit
	}
	return fallbackTokenCost(tokens, platform)
}

func resolveSiteSpend(explicit *float64, tokens int64, platform string) float64 {
	if explicit != nil && *explicit > 0 {
		return *explicit
	}
	return fallbackTokenCost(tokens, platform)
}

func resolveModelSpend(explicit *float64, tokens int64) float64 {
	if explicit != nil && *explicit > 0 {
		return *explicit
	}
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) / 500_000
}

func projectionTimestamp(raw *string) time.Time {
	if raw != nil {
		value := strings.TrimSpace(*raw)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Now().UTC()
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
// When a recompute is already pending, the earlier (smaller) log id is kept so the
// rewind window is never silently reduced.
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
	cp := s.readCheckpoint(dbw)
	if cp.RecomputeFromID != nil && *cp.RecomputeFromID > 0 && *cp.RecomputeFromID < fromID {
		fromID = *cp.RecomputeFromID
	}
	dbw.Exec(`UPDATE analytics_projection_checkpoints
		SET recompute_from_id = ?, recompute_requested_at = ?, updated_at = ?
		WHERE projector_key = ?`, fromID, now, now, usageProjectorKey)
	slog.Info("usage-aggregation: recompute requested", "from_log_id", fromID)
}
