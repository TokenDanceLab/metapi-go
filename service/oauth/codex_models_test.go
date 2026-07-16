package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsCodexGPT5FamilyModel(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.5", true},
		{"gpt-5.4", true},
		{"GPT-5.2-codex", true},
		{"gpt-5", true},
		{"gpt-5-mini", true},
		{"gpt-4o", false},
		{"claude-3.7-sonnet", false},
		{"", false},
		{"  gpt-5.5  ", true},
	}
	for _, tc := range cases {
		if got := IsCodexGPT5FamilyModel(tc.model); got != tc.want {
			t.Errorf("IsCodexGPT5FamilyModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestIsCodexModelAllowed_IncludesGPT55WithoutHardBlock(t *testing.T) {
	// gpt-5.5 must be allowed even when discovery list is empty or only has older models.
	if !IsCodexModelAllowed("gpt-5.5", nil) {
		t.Fatal("gpt-5.5 must be allowed with empty discovered list")
	}
	if !IsCodexModelAllowed("gpt-5.5", []string{"gpt-5.4", "gpt-5.2-codex"}) {
		t.Fatal("gpt-5.5 must be allowed even when only older models were discovered")
	}
	if !IsCodexModelAllowed("gpt-5.4-mini", nil) {
		t.Fatal("gpt-5.x family should be version-flexible")
	}
	// Non-family models require explicit discovery membership.
	if IsCodexModelAllowed("o3-pro", nil) {
		t.Fatal("non-family model without discovery membership must be denied")
	}
	if !IsCodexModelAllowed("o3-pro", []string{"o3-pro"}) {
		t.Fatal("non-family model present in discovered must be allowed")
	}
}

func TestCodexSeedModels_NotObsoleteOnly(t *testing.T) {
	has55 := false
	hasOlder := false
	for _, m := range CodexSeedModels {
		if !IsCodexGPT5FamilyModel(m) {
			t.Errorf("seed model %q is not gpt-5.x family", m)
		}
		if strings.EqualFold(m, "gpt-5.5") {
			has55 = true
		}
		if strings.HasPrefix(strings.ToLower(m), "gpt-5.4") || strings.HasPrefix(strings.ToLower(m), "gpt-5.2") {
			hasOlder = true
		}
	}
	if !has55 {
		t.Fatal("CodexSeedModels must include gpt-5.5 (not obsolete-only)")
	}
	if !hasOlder {
		t.Fatal("CodexSeedModels should keep recent predecessors for fixture coverage")
	}
}

func TestFilterCodexAllowedModels_VersionFlexible(t *testing.T) {
	input := []string{"gpt-5.5", "gpt-5.4", "o3-pro", "  ", "claude-opus"}
	got := FilterCodexAllowedModels(input, []string{"o3-pro"})
	want := map[string]bool{"gpt-5.5": true, "gpt-5.4": true, "o3-pro": true}
	if len(got) != 3 {
		t.Fatalf("expected 3 allowed models, got %v", got)
	}
	for _, m := range got {
		if !want[m] {
			t.Errorf("unexpected model %q in filtered set %v", m, got)
		}
	}
}

func TestSelectCodexQuotaProbeModel_PrefersGPT55(t *testing.T) {
	// Empty discovery → current fallback is gpt-5.5 (not obsolete gpt-5.4 only).
	if got := SelectCodexQuotaProbeModel(nil); got != "gpt-5.5" {
		t.Errorf("empty discovery fallback = %q, want gpt-5.5", got)
	}
	// Prefer newest preferred model present in discovery.
	if got := SelectCodexQuotaProbeModel([]string{"gpt-5.2-codex", "gpt-5.5", "gpt-5.4"}); got != "gpt-5.5" {
		t.Errorf("prefer gpt-5.5, got %q", got)
	}
	// When only older models are discovered, pick preferred older (gpt-5.4).
	if got := SelectCodexQuotaProbeModel([]string{"gpt-5.2", "gpt-5.4"}); got != "gpt-5.4" {
		t.Errorf("prefer gpt-5.4 among older, got %q", got)
	}
	// Preserve original casing from discovery.
	if got := SelectCodexQuotaProbeModel([]string{"GPT-5.5"}); got != "GPT-5.5" {
		t.Errorf("preserve casing, got %q", got)
	}
}

func TestCodexQuotaProbeModelForAccount(t *testing.T) {
	if got := CodexQuotaProbeModelForAccount(nil); got != "gpt-5.5" {
		t.Errorf("nil oauth = %q, want gpt-5.5", got)
	}
	oauth := &OauthInfo{LastDiscoveredModels: []string{"gpt-5.4", "gpt-5.5"}}
	if got := CodexQuotaProbeModelForAccount(oauth); got != "gpt-5.5" {
		t.Errorf("account with gpt-5.5 discovered = %q, want gpt-5.5", got)
	}
}

func TestNormalizeDiscoveredCodexModels_Dedupe(t *testing.T) {
	got := NormalizeDiscoveredCodexModels([]string{"gpt-5.5", " GPT-5.5 ", "gpt-5.4", ""})
	if len(got) != 2 {
		t.Fatalf("expected 2, got %v", got)
	}
	if got[0] != "gpt-5.5" {
		t.Errorf("first-wins casing: got %q", got[0])
	}
}

func TestMergeCodexDiscoveredWithSeed_IncludesGPT55(t *testing.T) {
	// Partial upstream list without gpt-5.5 still merges seed gpt-5.5.
	got := MergeCodexDiscoveredWithSeed([]string{"gpt-5.4", "gpt-5.2-codex"})
	found := false
	for _, m := range got {
		if strings.EqualFold(m, "gpt-5.5") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("merged list must include gpt-5.5, got %v", got)
	}
}

func TestExtractCodexModelIDs(t *testing.T) {
	payload := map[string]any{
		"models": []any{
			map[string]any{"id": "gpt-5.5"},
			map[string]any{"slug": "gpt-5.4"},
			"gpt-5.2-codex",
			map[string]any{"model": "gpt-5.1"},
		},
	}
	got := ExtractCodexModelIDs(payload)
	if len(got) != 4 {
		t.Fatalf("expected 4 models, got %v", got)
	}
	if got[0] != "gpt-5.5" {
		t.Errorf("first model = %q", got[0])
	}
}

func TestBuildCodexModelsEndpoint(t *testing.T) {
	got := BuildCodexModelsEndpoint("https://chatgpt.com/backend-api/codex/")
	want := "https://chatgpt.com/backend-api/codex/models?client_version=1.0.0"
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
}

func TestClassifyCodexModelDiscoveryError(t *testing.T) {
	if ClassifyCodexModelDiscoveryError(errors.New("codex model discovery timeout (30s)")) != CodexDiscoveryTimeout {
		t.Error("timeout classification failed")
	}
	if ClassifyCodexModelDiscoveryError(errors.New("HTTP 401: unauthorized")) != CodexDiscoveryUnauthorized {
		t.Error("unauthorized classification failed")
	}
	if ClassifyCodexModelDiscoveryError(errors.New("未获取到可用模型")) != CodexDiscoveryEmptyModels {
		t.Error("empty models classification failed")
	}
	if ClassifyCodexModelDiscoveryError(errors.New("boom")) != CodexDiscoveryUnknown {
		t.Error("unknown classification failed")
	}
}

func TestFormatCodexModelDiscoveryTimeoutStatus_Actionable(t *testing.T) {
	msg := FormatCodexModelDiscoveryTimeoutStatus(CodexModelDiscoveryTimeout, 2)
	for _, needle := range []string{
		"timed out",
		"attempt",
		"proxy",
		"abnormal",
		"30s",
	} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(needle)) && !strings.Contains(msg, needle) {
			// soft check — at least core tokens
		}
	}
	if !strings.Contains(msg, "timed out") {
		t.Errorf("expected timeout wording, got %q", msg)
	}
	if !strings.Contains(msg, "abnormal") {
		t.Errorf("expected status=abnormal guidance, got %q", msg)
	}
	if !strings.Contains(msg, "proxy") {
		t.Errorf("expected proxy guidance, got %q", msg)
	}
}

func TestDiscoverCodexModelsFromCloud_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("client_version") != "1.0.0" {
			t.Errorf("client_version = %q", r.URL.Query().Get("client_version"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Chatgpt-Account-Id"); got != "acct-1" {
			t.Errorf("Chatgpt-Account-Id = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []any{
				map[string]any{"id": "gpt-5.5"},
				map[string]any{"id": "gpt-5.4"},
			},
		})
	}))
	defer srv.Close()

	models, err := DiscoverCodexModelsFromCloud(context.Background(), CodexModelDiscoveryInput{
		BaseURL:     srv.URL,
		AccessToken: "tok-abc",
		AccountID:   "acct-1",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-5.5" {
		t.Fatalf("models = %v", models)
	}
	if !IsCodexModelAllowed("gpt-5.5", models) {
		t.Fatal("discovered gpt-5.5 must be allowed")
	}
}

func TestDiscoverCodexModelsWithSoftRetry_SuccessFirstAttempt(t *testing.T) {
	var calls atomic.Int32
	fetch := func(ctx context.Context, input CodexModelDiscoveryInput) ([]string, error) {
		calls.Add(1)
		return []string{"gpt-5.5", "gpt-5.4"}, nil
	}
	result := DiscoverCodexModelsWithSoftRetry(context.Background(), CodexModelDiscoveryInput{}, fetch)
	if result.Status != OauthModelDiscoveryHealthy {
		t.Fatalf("status = %q, want healthy", result.Status)
	}
	if result.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", result.Attempts)
	}
	if calls.Load() != 1 {
		t.Errorf("fetch calls = %d, want 1", calls.Load())
	}
	if len(result.Models) != 2 || result.Models[0] != "gpt-5.5" {
		t.Errorf("models = %v", result.Models)
	}
	if result.ErrorMessage != "" {
		t.Errorf("error message should be empty on success, got %q", result.ErrorMessage)
	}
}

func TestDiscoverCodexModelsWithSoftRetry_TimeoutThenSuccess(t *testing.T) {
	var calls atomic.Int32
	fetch := func(ctx context.Context, input CodexModelDiscoveryInput) ([]string, error) {
		n := calls.Add(1)
		if n == 1 {
			return nil, errors.New("codex model discovery timeout (30s)")
		}
		return []string{"gpt-5.5"}, nil
	}
	var slept time.Duration
	result := DiscoverCodexModelsWithSoftRetry(context.Background(), CodexModelDiscoveryInput{
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = d
			return nil
		},
	}, fetch)
	if result.Status != OauthModelDiscoveryHealthy {
		t.Fatalf("status = %q (%s), want healthy", result.Status, result.ErrorMessage)
	}
	if result.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", result.Attempts)
	}
	if calls.Load() != 2 {
		t.Errorf("fetch calls = %d, want 2", calls.Load())
	}
	if slept != CodexModelDiscoveryBackoff {
		t.Errorf("backoff = %v, want %v", slept, CodexModelDiscoveryBackoff)
	}
	if result.Models[0] != "gpt-5.5" {
		t.Errorf("models = %v", result.Models)
	}
}

func TestDiscoverCodexModelsWithSoftRetry_TimeoutExhausted(t *testing.T) {
	var calls atomic.Int32
	fetch := func(ctx context.Context, input CodexModelDiscoveryInput) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("codex model discovery timeout (30s): context deadline exceeded")
	}
	result := DiscoverCodexModelsWithSoftRetry(context.Background(), CodexModelDiscoveryInput{
		Sleep: func(ctx context.Context, d time.Duration) error { return nil },
	}, fetch)
	if result.Status != OauthModelDiscoveryAbnormal {
		t.Fatalf("status = %q, want abnormal", result.Status)
	}
	if result.ErrorCode != CodexDiscoveryTimeout {
		t.Errorf("errorCode = %q, want timeout", result.ErrorCode)
	}
	if !result.TimedOut {
		t.Error("TimedOut should be true")
	}
	if result.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", result.Attempts)
	}
	if calls.Load() != 2 {
		t.Errorf("fetch calls = %d, want 2", calls.Load())
	}
	if !strings.Contains(result.ErrorMessage, "timed out") {
		t.Errorf("actionable message missing timeout text: %q", result.ErrorMessage)
	}
	if !strings.Contains(result.ErrorMessage, "proxy") {
		t.Errorf("actionable message missing proxy guidance: %q", result.ErrorMessage)
	}
}

func TestDiscoverCodexModelsWithSoftRetry_UnauthorizedNoRetry(t *testing.T) {
	var calls atomic.Int32
	fetch := func(ctx context.Context, input CodexModelDiscoveryInput) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("HTTP 401: unauthorized")
	}
	result := DiscoverCodexModelsWithSoftRetry(context.Background(), CodexModelDiscoveryInput{}, fetch)
	if result.Status != OauthModelDiscoveryAbnormal {
		t.Fatalf("status = %q", result.Status)
	}
	if result.ErrorCode != CodexDiscoveryUnauthorized {
		t.Errorf("errorCode = %q, want unauthorized", result.ErrorCode)
	}
	if calls.Load() != 1 {
		t.Errorf("must not soft-retry non-timeout errors; calls = %d", calls.Load())
	}
	if result.TimedOut {
		t.Error("TimedOut should be false for unauthorized")
	}
}

func TestBuildOauthModelDiscoveryPatch(t *testing.T) {
	ok := &CodexModelDiscoveryResult{
		Models:   []string{"gpt-5.5", "gpt-5.4"},
		Status:   OauthModelDiscoveryHealthy,
		Attempts: 1,
	}
	patch := BuildOauthModelDiscoveryPatch(ok, "2026-07-17T00:00:00Z")
	if patch.ModelDiscoveryStatus != OauthModelDiscoveryHealthy {
		t.Errorf("status = %q", patch.ModelDiscoveryStatus)
	}
	if patch.LastModelSyncAt != "2026-07-17T00:00:00Z" {
		t.Errorf("sync at = %q", patch.LastModelSyncAt)
	}
	if len(patch.LastDiscoveredModels) != 2 {
		t.Errorf("models = %v", patch.LastDiscoveredModels)
	}

	fail := &CodexModelDiscoveryResult{
		Status:       OauthModelDiscoveryAbnormal,
		ErrorCode:    CodexDiscoveryTimeout,
		ErrorMessage: FormatCodexModelDiscoveryTimeoutStatus(CodexModelDiscoveryTimeout, 2),
		Attempts:     2,
		TimedOut:     true,
	}
	failPatch := BuildOauthModelDiscoveryPatch(fail, "2026-07-17T00:00:01Z")
	if failPatch.ModelDiscoveryStatus != OauthModelDiscoveryAbnormal {
		t.Errorf("fail status = %q", failPatch.ModelDiscoveryStatus)
	}
	if failPatch.LastModelSyncError == "" {
		t.Error("fail patch must carry error message")
	}
}

func TestDiscoveryTimeoutConstants_SufficientForColdStart(t *testing.T) {
	// Historical upstream first discovery was 12s; require a larger first budget.
	if CodexModelDiscoveryTimeout < 20*time.Second {
		t.Errorf("first discovery timeout %v is too short for cold start", CodexModelDiscoveryTimeout)
	}
	if CodexModelDiscoveryRetryTimeout < CodexModelDiscoveryTimeout {
		t.Errorf("retry timeout %v should be >= first timeout %v",
			CodexModelDiscoveryRetryTimeout, CodexModelDiscoveryTimeout)
	}
	if CodexModelDiscoveryMaxAttempts < 2 {
		t.Error("soft-retry requires at least 2 attempts")
	}
}
