package proxy

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestRequestIDFromContext_MetAPIKey(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-metapi-1")
	if got := RequestIDFromContext(ctx); got != "req-metapi-1" {
		t.Fatalf("RequestIDFromContext = %q, want req-metapi-1", got)
	}
}

func TestRequestIDFromContext_ChiFallback(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-chi-1")
	if got := RequestIDFromContext(ctx); got != "req-chi-1" {
		t.Fatalf("RequestIDFromContext chi fallback = %q, want req-chi-1", got)
	}
}

func TestEnsureRequestID_PreservesExistingAcrossAttempts(t *testing.T) {
	base := WithRequestID(context.Background(), "parent-trace")
	// Simulate channel attempt #0 and #1: same parent id, no overwrite.
	ctx0, id0 := EnsureRequestID(base, "should-not-win")
	ctx1, id1 := EnsureRequestID(ctx0, "also-ignored")
	if id0 != "parent-trace" || id1 != "parent-trace" {
		t.Fatalf("ids = %q / %q, want parent-trace for both attempts", id0, id1)
	}
	if RequestIDFromContext(ctx1) != "parent-trace" {
		t.Fatalf("ctx lost request id: %q", RequestIDFromContext(ctx1))
	}
}

func TestEnsureRequestID_UsesFallbackWhenMissing(t *testing.T) {
	ctx, id := EnsureRequestID(context.Background(), "fallback-1")
	if id != "fallback-1" {
		t.Fatalf("id = %q, want fallback-1", id)
	}
	if RequestIDFromContext(ctx) != "fallback-1" {
		t.Fatalf("ctx id = %q", RequestIDFromContext(ctx))
	}
}
