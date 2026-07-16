package proxyhandler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/tokendancelab/metapi-go/routing"
)

// Cost attribution response headers (operator-facing, LiteLLM-inspired).
// Set only when headers have not been written yet (cheap / best-effort).
const (
	headerMetapiCost              = "X-Metapi-Cost"
	headerMetapiCostSource        = "X-Metapi-Cost-Source"
	headerMetapiPromptTokens      = "X-Metapi-Prompt-Tokens"
	headerMetapiCompletionTokens  = "X-Metapi-Completion-Tokens"
	headerMetapiTotalTokens       = "X-Metapi-Total-Tokens"
	headerMetapiCacheReadTokens   = "X-Metapi-Cache-Read-Tokens"
	headerMetapiCacheCreateTokens = "X-Metapi-Cache-Creation-Tokens"
	headerMetapiReasoningTokens   = "X-Metapi-Reasoning-Tokens"
)

// buildRequestCostAttribution estimates cost + billing_details for a proxy log.
// Pricing catalog wiring is optional: when no PricingModel is available the
// platform fallback divisor is used and ratios are zeroed (not 1.0). Claude
// cache_ratio policy is applied only when a PricingModel is supplied.
func buildRequestCostAttribution(
	modelName string,
	platform string,
	usage ParsedUsage,
	model *routing.PricingModel,
	groupRatio map[string]float64,
) routing.RequestCostAttribution {
	attr := routing.EstimateRequestCost(modelName, platform, usage.ToUsageForCost(), model, groupRatio)
	attr.ReasoningTokens = usage.ReasoningTokens
	if attr.BillingDetails != nil && usage.ReasoningTokens > 0 {
		attr.BillingDetails.Usage.ReasoningTokens = usage.ReasoningTokens
	}
	return attr
}

// applyCostAttributionHeaders sets optional operator cost/token headers when the
// response writer has not yet written a status line. Safe no-op after WriteHeader.
func applyCostAttributionHeaders(w http.ResponseWriter, usage ParsedUsage, attr routing.RequestCostAttribution) {
	if w == nil {
		return
	}
	// httptest.ResponseRecorder and real servers: check if headers were written.
	if rw, ok := w.(interface{ Written() bool }); ok && rw.Written() {
		return
	}
	// Standard library: after WriteHeader, Header map is still mutable on most
	// ResponseWriters but clients may already have received headers. Prefer the
	// Written() check above; fall through for plain ResponseWriter.
	h := w.Header()
	if attr.Source != "" {
		h.Set(headerMetapiCost, formatCostHeader(attr.EstimatedCost))
		h.Set(headerMetapiCostSource, attr.Source)
	}
	if usage.Found || usage.PromptTokens > 0 || usage.CompletionTokens > 0 || usage.TotalTokens > 0 {
		h.Set(headerMetapiPromptTokens, strconv.FormatInt(usage.PromptTokens, 10))
		h.Set(headerMetapiCompletionTokens, strconv.FormatInt(usage.CompletionTokens, 10))
		h.Set(headerMetapiTotalTokens, strconv.FormatInt(usage.TotalTokens, 10))
	}
	if usage.CacheReadTokens > 0 {
		h.Set(headerMetapiCacheReadTokens, strconv.FormatInt(usage.CacheReadTokens, 10))
	}
	if usage.CacheCreationTokens > 0 {
		h.Set(headerMetapiCacheCreateTokens, strconv.FormatInt(usage.CacheCreationTokens, 10))
	}
	if usage.ReasoningTokens > 0 {
		h.Set(headerMetapiReasoningTokens, strconv.FormatInt(usage.ReasoningTokens, 10))
	}
}

func formatCostHeader(cost float64) string {
	// Fixed precision without scientific notation; trim trailing zeros lightly.
	s := strconv.FormatFloat(cost, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// formatCostDebug is used only in tests / slog if needed.
func formatCostDebug(attr routing.RequestCostAttribution) string {
	return fmt.Sprintf("cost=%s source=%s", formatCostHeader(attr.EstimatedCost), attr.Source)
}
