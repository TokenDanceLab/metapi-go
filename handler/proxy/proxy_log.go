package proxyhandler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/store"
)

// defaultLogProxyWriter inserts a proxy.ProxyLogEntry into proxy_logs.
// Prefer injecting via UpstreamConfig.LogProxy so tests can capture writes.
func defaultLogProxyWriter(ctx context.Context, entry proxy.ProxyLogEntry) error {
	db := store.GetDB()
	if db == nil {
		return nil
	}
	return InsertProxyLog(ctx, db, entry)
}

// InsertProxyLog writes one proxy_logs row. Columns match store.ProxyLog / DDL.
// Fields present only on ProxyLogEntry (usage_source, upstream_path) are not
// persisted until the schema grows those columns; they remain on the entry for
// in-process consumers and tests.
func InsertProxyLog(ctx context.Context, db *store.DB, entry proxy.ProxyLogEntry) error {
	if db == nil {
		return nil
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)
	var billingDetails any
	if entry.BillingDetails != nil {
		switch v := entry.BillingDetails.(type) {
		case string:
			billingDetails = v
		default:
			if b, err := json.Marshal(v); err == nil {
				billingDetails = string(b)
			}
		}
	}

	query := `
		INSERT INTO proxy_logs (
			route_id, channel_id, account_id, downstream_api_key_id,
			model_requested, model_actual, status, http_status, is_stream,
			first_byte_latency_ms, latency_ms,
			prompt_tokens, completion_tokens, total_tokens,
			estimated_cost, billing_details,
			client_family, client_app_id, client_app_name, client_confidence,
			error_message, retry_count, request_id, created_at
		) VALUES (
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?
		)`

	args := []any{
		nullInt64(entry.RouteID),
		nullInt64(entry.ChannelID),
		nullInt64(entry.AccountID),
		nullInt64(entry.DownstreamAPIKeyID),
		nullString(strPtrOrEmpty(entry.ModelRequested)),
		nullString(entry.ModelActual),
		nullString(strPtrOrEmpty(entry.Status)),
		entry.HTTPStatus,
		nullBool(entry.IsStream),
		nullInt64(entry.FirstByteLatencyMs),
		entry.LatencyMs,
		nullInt64(entry.PromptTokens),
		nullInt64(entry.CompletionTokens),
		nullInt64(entry.TotalTokens),
		entry.EstimatedCost,
		billingDetails,
		nullString(strPtrOrEmpty(entry.ClientFamily)),
		nullString(strPtrOrEmpty(entry.ClientAppID)),
		nullString(strPtrOrEmpty(entry.ClientAppName)),
		nullString(strPtrOrEmpty(entry.ClientConfidence)),
		nullString(entry.ErrorMessage),
		entry.RetryCount,
		nullString(strPtrOrEmpty(entry.RequestID)),
		createdAt,
	}

	if ctx != nil {
		if db.Dialect == store.DialectPostgres {
			query = db.Rebind(query)
		}
		_, err := db.DB.ExecContext(ctx, query, args...)
		return err
	}
	_, err := db.Exec(query, args...)
	return err
}

func logProxy(ctx context.Context, cfg *UpstreamConfig, entry proxy.ProxyLogEntry) {
	if cfg == nil {
		return
	}
	// Prefer explicit entry.RequestID; otherwise inherit chi/MetAPI request id.
	if entry.RequestID == "" {
		entry.RequestID = proxy.RequestIDFromContext(ctx)
	}
	writer := cfg.LogProxy
	if writer == nil {
		writer = defaultLogProxyWriter
	}
	if err := writer(ctx, entry); err != nil {
		slog.Warn("LogProxy failed",
			"err", err,
			"status", entry.Status,
			"model", entry.ModelRequested,
			"request_id", entry.RequestID,
			"retry_count", entry.RetryCount,
		)
	}
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func strPtrOrEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func int64Ptr(v int64) *int64 { return &v }

func boolPtr(v bool) *bool { return &v }
