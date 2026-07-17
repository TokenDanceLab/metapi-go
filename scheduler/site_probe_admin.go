package scheduler

import (
	"fmt"

	"github.com/tokendancelab/metapi-go/store"
)

// ProbeSiteResult is one model outcome for admin probe-now (#154).
type ProbeSiteResult struct {
	ChannelID int64   `json:"channelId"`
	AccountID int64   `json:"accountId"`
	Model     string  `json:"model"`
	Status    string  `json:"status"`
	LatencyMs float64 `json:"latencyMs"`
	Error     string  `json:"error,omitempty"`
}

// ProbeSite runs probes for active channels on a site (bounded to 32).
func (s *ModelProbeScheduler) ProbeSite(siteID int64) (results []ProbeSiteResult, available, unavailable int) {
	if s == nil || siteID <= 0 {
		return nil, 0, 0
	}
	dbw := store.GetDB()
	if dbw == nil {
		return nil, 0, 0
	}
	sql := "SELECT rc.id, rc.account_id, a.site_id, COALESCE(rc.source_model, '') AS source_model " +
		"FROM route_channels rc " +
		"INNER JOIN accounts a ON rc.account_id = a.id " +
		"INNER JOIN sites st ON a.site_id = st.id " +
		"WHERE rc.enabled = TRUE AND a.status = 'active' AND st.status = 'active' " +
		"AND a.site_id = ? AND COALESCE(rc.source_model, '') <> '' " +
		"ORDER BY rc.id ASC LIMIT 32"
	targets, err := queryProbeTargets(dbw, sql, siteID)
	if err != nil {
		return []ProbeSiteResult{{Status: "error", Error: fmt.Sprintf("%v", err)}}, 0, 0
	}
	timeoutMs := 15000
	if s.cfg != nil && s.cfg.ModelAvailabilityProbeTimeoutMs >= 3000 {
		timeoutMs = s.cfg.ModelAvailabilityProbeTimeoutMs
	}
	for _, target := range targets {
		outcome := s.probeOne(target, timeoutMs)
		res := ProbeSiteResult{
			ChannelID: target.ChannelID,
			AccountID: target.AccountID,
			Model:     target.ModelName,
			Status:    outcome,
		}
		results = append(results, res)
		if outcome == "success" {
			available++
		} else if outcome == "failure" {
			unavailable++
		}
	}
	if results == nil {
		results = []ProbeSiteResult{}
	}
	return results, available, unavailable
}
