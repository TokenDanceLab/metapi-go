package scheduler

import (
	"context"
	"sync"
	"log/slog"
	"time"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// ProbeRecorder records proactive probe outcomes into routing health (#114).
type ProbeRecorder interface {
	RecordProbeSuccess(ctx context.Context, channelID int64, latencyMs float64, modelName *string, actualAccountID *int64) error
	RecordFailure(ctx context.Context, channelID int64, failureCtx routing.SiteRuntimeFailureContext, actualAccountID *int64) error
}

// SetProbeRecorder wires a TokenRouter (or test double) into the model probe scheduler.
func (s *ModelProbeScheduler) SetProbeRecorder(r ProbeRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probeRecorder = r
}

// ProbeChannelOutcome is a single proactive probe result.
type ProbeChannelOutcome struct {
	ChannelID int64
	AccountID int64
	SiteID    int64
	ModelName string
	OK        bool
	LatencyMs float64
	ErrorText string
}

// ApplyProbeOutcome feeds a probe result into routing health and model_availability.
func ApplyProbeOutcome(ctx context.Context, dbw *store.DB, recorder ProbeRecorder, outcome ProbeChannelOutcome) {
	model := outcome.ModelName
	if model == "" {
		model = "probe"
	}
	modelPtr := &model

	if recorder != nil && outcome.ChannelID > 0 {
		if outcome.OK {
			_ = recorder.RecordProbeSuccess(ctx, outcome.ChannelID, outcome.LatencyMs, modelPtr, nil)
		} else {
			errText := outcome.ErrorText
			if errText == "" {
				errText = "proactive model probe failed"
			}
			status := 503
			_ = recorder.RecordFailure(ctx, outcome.ChannelID, routing.SiteRuntimeFailureContext{
				Status:    &status,
				ErrorText: &errText,
				ModelName: modelPtr,
			}, nil)
		}
	} else if outcome.SiteID > 0 && outcome.OK {
		routing.RecordSiteRuntimeSuccess(outcome.SiteID, outcome.LatencyMs, modelPtr, outcome.LatencyMs)
	} else if outcome.SiteID > 0 && !outcome.OK {
		slog.Debug("model-probe: site-scoped probe failure",
			"site_id", outcome.SiteID,
			"model", model,
			"error", outcome.ErrorText,
		)
	}

	if dbw == nil || outcome.AccountID <= 0 || outcome.ModelName == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	available := outcome.OK
	var existingID int64
	err := dbw.Get(&existingID, dbw.Rebind(`SELECT id FROM model_availability WHERE account_id = ? AND model_name = ?`), outcome.AccountID, outcome.ModelName)
	if err == nil && existingID > 0 {
		_, _ = dbw.Exec(dbw.Rebind(`
			UPDATE model_availability
			SET available = ?, latency_ms = ?, checked_at = ?
			WHERE id = ?
		`), available, int64(outcome.LatencyMs), now, existingID)
		return
	}
	_, _ = dbw.Exec(dbw.Rebind(`
		INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`), outcome.AccountID, outcome.ModelName, available, false, int64(outcome.LatencyMs), now)
}

// collectProbeOutcomes builds synthetic proactive probe outcomes from current DB
// state (enabled channels + model_availability). This unblocks health scoring
// without requiring a live upstream HTTP probe implementation.
func (s *ModelProbeScheduler) collectProbeOutcomes(dbw *store.DB, accountIDs []int64) []ProbeChannelOutcome {
	if dbw == nil || len(accountIDs) == 0 {
		return nil
	}
	out := make([]ProbeChannelOutcome, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		var siteID int64
		if err := dbw.Get(&siteID, dbw.Rebind(`SELECT site_id FROM accounts WHERE id = ?`), accountID); err != nil {
			continue
		}
		// Prefer an enabled channel for this account.
		var channelID int64
		_ = dbw.Get(&channelID, dbw.Rebind(`
			SELECT id FROM route_channels
			WHERE account_id = ? AND COALESCE(enabled, 1) = 1
			ORDER BY id ASC
			LIMIT 1
		`), accountID)

		type modelRow struct {
			ModelName string  `db:"model_name"`
			Available bool    `db:"available"`
			LatencyMs *int64  `db:"latency_ms"`
		}
		var models []modelRow
		_ = dbw.Select(&models, dbw.Rebind(`
			SELECT model_name, available, latency_ms
			FROM model_availability
			WHERE account_id = ?
			ORDER BY checked_at DESC
			LIMIT 3
		`), accountID)
		if len(models) == 0 {
			// No availability rows yet: soft-success with default model label so
			// operators enabling the probe still get runtime health updates.
			out = append(out, ProbeChannelOutcome{
				ChannelID: channelID,
				AccountID: accountID,
				SiteID:    siteID,
				ModelName: "probe-default",
				OK:        true,
				LatencyMs: 500,
			})
			continue
		}
		for _, m := range models {
			lat := 800.0
			if m.LatencyMs != nil && *m.LatencyMs > 0 {
				lat = float64(*m.LatencyMs)
			}
			out = append(out, ProbeChannelOutcome{
				ChannelID: channelID,
				AccountID: accountID,
				SiteID:    siteID,
				ModelName: m.ModelName,
				OK:        m.Available,
				LatencyMs: lat,
				ErrorText: map[bool]string{true: "", false: "model marked unavailable"}[m.Available],
			})
		}
	}
	return out
}


var (
	globalProbeMu       sync.RWMutex
	globalProbeRecorder ProbeRecorder
)

// SetGlobalModelProbeRecorder stores a process-wide probe recorder.
func SetGlobalModelProbeRecorder(r ProbeRecorder) {
	globalProbeMu.Lock()
	defer globalProbeMu.Unlock()
	globalProbeRecorder = r
}

func globalModelProbeRecorder() ProbeRecorder {
	globalProbeMu.RLock()
	defer globalProbeMu.RUnlock()
	return globalProbeRecorder
}
