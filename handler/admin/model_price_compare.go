package admin

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/routing"
)

// modelPriceCompare handles:
//
//	GET /api/models/price-compare?model=...&limit=...
//	GET /api/stats/model-prices?model=...&limit=...
//
// Aggregates effective model prices across enabled sites/accounts using
// billing_details rates, observed proxy_logs costs, configured unit_cost, and
// labeled platform fallbacks. Does not invent unlabeled prices.
func (h *statsHandler) modelPriceCompare(w http.ResponseWriter, r *http.Request) {
	modelQ := strings.TrimSpace(r.URL.Query().Get("model"))
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 200)
	days := clampInt(getQueryInt(r, "days", 30), 1, 365)
	topModels := clampInt(getQueryInt(r, "topModels", 12), 1, 50)

	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format(time.RFC3339)

	var models []string
	if modelQ != "" {
		models = []string{modelQ}
	} else {
		models = h.topModelsForPriceCompare(topModels, since)
	}

	candidates := make([]routing.PriceCompareRow, 0)
	for _, modelName := range models {
		inputs := h.loadPriceCompareInputs(modelName, since)
		for _, in := range inputs {
			row := routing.BuildPriceCompareRow(in, routing.DefaultPriceCompareSampleUsage)
			candidates = append(candidates, row)
		}
	}

	if modelQ != "" {
		q := strings.ToLower(modelQ)
		filtered := make([]routing.PriceCompareRow, 0, len(candidates))
		for _, row := range candidates {
			if strings.Contains(strings.ToLower(row.Model), q) {
				filtered = append(filtered, row)
			}
		}
		candidates = filtered
	}

	sorted := routing.AggregatePriceCompareRows(candidates)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	if sorted == nil {
		sorted = []routing.PriceCompareRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"model": modelQ,
		"days":  days,
		"limit": limit,
		"sampleUsage": map[string]any{
			"promptTokens":     routing.DefaultPriceCompareSampleUsage.PromptTokens,
			"completionTokens": routing.DefaultPriceCompareSampleUsage.CompletionTokens,
			"totalTokens":      routing.DefaultPriceCompareSampleUsage.TotalTokens,
		},
		"items": sorted,
		"meta": map[string]any{
			"count":            len(sorted),
			"modelsConsidered": len(models),
			"sources": []string{
				routing.PriceSourceBilling,
				routing.PriceSourceObserved,
				routing.PriceSourceConfigured,
				routing.PriceSourceFallback,
			},
			"notes": "Rates/costs from billing_details, observed proxy_logs, account unit_cost, or labeled fallback. missingPrice=true means no catalog/observed/configured signal.",
		},
	})
}

func (h *statsHandler) topModelsForPriceCompare(limit int, since string) []string {
	rows := queryRows(h.db, `
		SELECT COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '') AS model,
			COUNT(*) AS calls
		FROM proxy_logs pl
		WHERE pl.created_at >= ?
			AND pl.status = 'success'
			AND COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '') <> ''
		GROUP BY COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '')
		ORDER BY calls DESC
		LIMIT ?
	`, since, limit)

	out := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		m := strings.TrimSpace(coerceString(row["model"]))
		if m == "" {
			continue
		}
		key := strings.ToLower(m)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	if len(out) > 0 {
		return out
	}

	avail := queryRows(h.db, `
		SELECT model_name AS model, COUNT(*) AS cnt
		FROM model_availability
		GROUP BY model_name
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	for _, row := range avail {
		m := strings.TrimSpace(coerceString(row["model"]))
		if m == "" {
			continue
		}
		key := strings.ToLower(m)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	return out
}

type observedPriceSignal struct {
	AvgCost         float64
	Samples         int
	BillingDetails  string
	ResolvedModel   string
}

func (h *statsHandler) loadPriceCompareInputs(modelName string, since string) []routing.PriceCompareInput {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	like := "%" + strings.ToLower(modelName) + "%"

	// Observed success costs + latest-ish billing_details for matching models.
	obsRows := queryRows(h.db, `
		SELECT
			pl.account_id AS account_id,
			COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '') AS model_name,
			AVG(COALESCE(pl.estimated_cost, 0)) AS avg_cost,
			COUNT(*) AS samples,
			MAX(COALESCE(pl.billing_details, '')) AS billing_details
		FROM proxy_logs pl
		WHERE pl.created_at >= ?
			AND pl.status = 'success'
			AND COALESCE(pl.estimated_cost, 0) > 0
			AND LOWER(COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '')) LIKE ?
		GROUP BY pl.account_id, COALESCE(NULLIF(TRIM(pl.model_actual), ''), NULLIF(TRIM(pl.model_requested), ''), '')
	`, since, like)

	obsByAccount := map[int64]observedPriceSignal{}
	for _, row := range obsRows {
		accID := coerceInt64(row["accountId"])
		if accID <= 0 {
			continue
		}
		sig := observedPriceSignal{
			AvgCost:        coerceFloat(row["avgCost"]),
			Samples:        coerceInt(row["samples"]),
			BillingDetails: coerceString(row["billingDetails"]),
			ResolvedModel:  strings.TrimSpace(coerceString(row["modelName"])),
		}
		// Keep the highest-sample signal per account for this model filter.
		if prev, ok := obsByAccount[accID]; ok && prev.Samples >= sig.Samples {
			continue
		}
		obsByAccount[accID] = sig
	}

	// Accounts that advertise the model via availability.
	availRows := queryRows(h.db, `
		SELECT account_id, model_name
		FROM model_availability
		WHERE LOWER(model_name) LIKE ?
	`, like)
	availModelByAccount := map[int64]string{}
	for _, row := range availRows {
		accID := coerceInt64(row["accountId"])
		if accID <= 0 {
			continue
		}
		if _, ok := availModelByAccount[accID]; ok {
			continue
		}
		availModelByAccount[accID] = strings.TrimSpace(coerceString(row["modelName"]))
	}

	// Active accounts on non-disabled sites.
	accountRows := queryRows(h.db, `
		SELECT
			s.id AS site_id,
			s.name AS site_name,
			s.platform AS platform,
			a.id AS account_id,
			COALESCE(a.username, '') AS username,
			a.unit_cost AS unit_cost,
			COALESCE(a.status, '') AS account_status,
			COALESCE(s.status, '') AS site_status
		FROM accounts a
		INNER JOIN sites s ON s.id = a.site_id
		WHERE COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
		ORDER BY s.name ASC, a.id ASC
	`)

	out := make([]routing.PriceCompareInput, 0)
	for _, row := range accountRows {
		accID := coerceInt64(row["accountId"])
		if accID <= 0 {
			continue
		}
		obs, hasObs := obsByAccount[accID]
		availModel, hasAvail := availModelByAccount[accID]
		unitCost := coerceFloat(row["unitCost"])
		hasUnit := unitCost > 0 && !math.IsNaN(unitCost)

		// Include account if it has availability, observed traffic, or configured unit cost.
		if !hasObs && !hasAvail && !hasUnit {
			continue
		}

		resolvedModel := modelName
		if hasAvail && availModel != "" {
			resolvedModel = availModel
		} else if hasObs && obs.ResolvedModel != "" {
			resolvedModel = obs.ResolvedModel
		}

		in := routing.PriceCompareInput{
			SiteID:    coerceInt64(row["siteId"]),
			SiteName:  coerceString(row["siteName"]),
			Platform:  coerceString(row["platform"]),
			Model:     resolvedModel,
			AccountID: accID,
			Username:  coerceString(row["username"]),
		}
		if hasUnit {
			v := unitCost
			in.ConfiguredUnitCost = &v
		}
		if hasObs {
			in.ObservedSamples = obs.Samples
			if obs.AvgCost > 0 {
				v := obs.AvgCost
				in.ObservedAvgCost = &v
			}
			if strings.TrimSpace(obs.BillingDetails) != "" {
				applyBillingDetailsToPriceInput(&in, obs.BillingDetails)
			}
		}
		out = append(out, in)
	}

	// Empty-state: when no signals exist for this model, still return active
	// accounts as labeled fallback rows so operators see missing-price sites.
	if len(out) == 0 {
		for _, row := range accountRows {
			// Prefer active accounts for empty/missing UI.
			if coerceString(row["accountStatus"]) != "active" {
				continue
			}
			out = append(out, routing.PriceCompareInput{
				SiteID:    coerceInt64(row["siteId"]),
				SiteName:  coerceString(row["siteName"]),
				Platform:  coerceString(row["platform"]),
				Model:     modelName,
				AccountID: coerceInt64(row["accountId"]),
				Username:  coerceString(row["username"]),
			})
			if len(out) >= 50 {
				break
			}
		}
	}
	return out
}

func applyBillingDetailsToPriceInput(in *routing.PriceCompareInput, raw string) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return
	}
	breakdown := firstMap(parsed, "breakdown", "Breakdown")
	pricing := firstMap(parsed, "pricing", "Pricing")

	if breakdown != nil {
		if v, ok := floatFromAny(firstAny(breakdown, "inputPerMillion", "input_per_million", "InputPerMillion")); ok {
			in.InputPerMillion = &v
		}
		if v, ok := floatFromAny(firstAny(breakdown, "outputPerMillion", "output_per_million", "OutputPerMillion")); ok {
			in.OutputPerMillion = &v
		}
	}
	if pricing != nil {
		if v, ok := floatFromAny(firstAny(pricing, "modelRatio", "model_ratio", "ModelRatio")); ok {
			in.ModelRatio = &v
		}
		if v, ok := floatFromAny(firstAny(pricing, "completionRatio", "completion_ratio", "CompletionRatio")); ok {
			in.CompletionRatio = &v
		}
		if v, ok := floatFromAny(firstAny(pricing, "groupRatio", "group_ratio", "GroupRatio")); ok {
			in.GroupRatio = &v
		}
	}
}

func firstMap(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if mm, ok2 := v.(map[string]any); ok2 {
				return mm
			}
		}
	}
	return nil
}

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func floatFromAny(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
