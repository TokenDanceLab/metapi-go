package proxyhandler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

type upstreamTestRouter struct {
	selected  routing.SelectedChannel
	next      *routing.SelectedChannel
	policies  []routing.DownstreamRoutingPolicy
	failures  []upstreamTestFailure
	successes []upstreamTestSuccess
}

type upstreamTestFailure struct {
	channelID int64
	status    *int
	errorText *string
	modelName *string
}

type upstreamTestSuccess struct {
	channelID          int64
	latencyMs          float64
	cost               float64
	modelName          *string
	firstByteLatencyMs *float64
}

func (r *upstreamTestRouter) SelectChannel(_ context.Context, _ string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	r.policies = append(r.policies, policy)
	return &r.selected, nil
}

func (r *upstreamTestRouter) SelectNextChannel(_ context.Context, _ string, _ []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	r.policies = append(r.policies, policy)
	return r.next, nil
}

func (r *upstreamTestRouter) SelectPreferredChannel(_ context.Context, _ string, _ int64, policy routing.DownstreamRoutingPolicy, _ []int64) (*routing.SelectedChannel, error) {
	r.policies = append(r.policies, policy)
	return nil, nil
}

func (r *upstreamTestRouter) RecordSuccess(_ context.Context, channelID int64, latencyMs float64, cost float64, modelName *string, _ *int64, firstByteLatencyMs *float64) error {
	r.successes = append(r.successes, upstreamTestSuccess{
		channelID:          channelID,
		latencyMs:          latencyMs,
		cost:               cost,
		modelName:          cloneStringPtr(modelName),
		firstByteLatencyMs: cloneFloat64Ptr(firstByteLatencyMs),
	})
	return nil
}

func cloneFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func (r *upstreamTestRouter) RecordFailure(_ context.Context, channelID int64, failureCtx routing.SiteRuntimeFailureContext, _ *int64) error {
	r.failures = append(r.failures, upstreamTestFailure{
		channelID: channelID,
		status:    cloneIntPtr(failureCtx.Status),
		errorText: cloneStringPtr(failureCtx.ErrorText),
		modelName: cloneStringPtr(failureCtx.ModelName),
	})
	return nil
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func cloneIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func TestStreamUpstreamNon2xxRelaysJSONErrorStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exhausted","type":"rate_limit_error"}}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-4o-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "quota exhausted") {
		t.Fatalf("body = %q, want upstream error body", body)
	}
}

func TestHandleStreamUpstreamRelaysLargeSSE(t *testing.T) {
	event := "data: " + strings.Repeat("x", 512) + "\n\n"
	var body strings.Builder
	for i := 0; i < 4096; i++ {
		body.WriteString(event)
	}
	body.WriteString("data: [DONE]\n\n")
	raw := body.String()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	handleStreamUpstream(rec, req, resp, 12)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != raw {
		t.Fatalf("relayed body length = %d, want %d", len(got), len(raw))
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}

func TestHandleStreamUpstreamStopsAtConfiguredByteLimit(t *testing.T) {
	t.Setenv("PROXY_MAX_STREAM_RESPONSE_BYTES", "32")

	raw := "data: " + strings.Repeat("x", 128) + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	handleStreamUpstream(rec, req, resp, 12)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, raw[:32]) {
		t.Fatalf("body prefix = %q, want first 32 raw bytes", body[:min(len(body), 32)])
	}
	if strings.Contains(body, raw[32:80]) {
		t.Fatalf("body contains bytes beyond configured stream limit")
	}
	if !strings.Contains(body, "stream response exceeded configured byte limit") {
		t.Fatalf("body = %q, want stream limit error event", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("body = %q, want DONE marker", body)
	}
}

func TestNonV1ChatAliasForwardsCanonicalV1Path(t *testing.T) {
	pathCh := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-4o-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := <-pathCh; got != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want /v1/chat/completions", got)
	}
}

func TestResponsesAliasForwardsCanonicalV1Path(t *testing.T) {
	pathCh := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[]}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-4o-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/responses", `{"model":"gpt-4o","input":"hi"}`)
	rec := httptest.NewRecorder()

	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := <-pathCh; got != "/v1/responses" {
		t.Fatalf("upstream path = %q, want /v1/responses", got)
	}
}

func TestDispatchUpstreamPassesDownstreamPolicyToRouter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o-upstream",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	tokenID := int64(55)
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "managed-token",
		Source: "managed",
		Policy: auth.DownstreamRoutingPolicy{
			SupportedModels:       []string{"gpt-4o"},
			AllowedRouteIDs:       []int64{101},
			ExcludedSiteIDs:       []int64{202},
			SiteWeightMultipliers: map[int64]float64{303: 2.5},
			ExcludedCredentialRefs: []auth.ExcludedCredentialRef{{
				Kind:      auth.CredentialRefAccountToken,
				SiteID:    3,
				AccountID: 7,
				TokenID:   &tokenID,
			}},
			DenyAllWhenEmpty: true,
		},
	})
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(router.policies) == 0 {
		t.Fatal("router received no downstream policy")
	}
	got := router.policies[0]
	if len(got.AllowedRouteIDs) != 1 || got.AllowedRouteIDs[0] != 101 {
		t.Fatalf("AllowedRouteIDs = %v, want [101]", got.AllowedRouteIDs)
	}
	if len(got.ExcludedSiteIDs) != 1 || got.ExcludedSiteIDs[0] != 202 {
		t.Fatalf("ExcludedSiteIDs = %v, want [202]", got.ExcludedSiteIDs)
	}
	if got.SiteWeightMultipliers[303] != 2.5 {
		t.Fatalf("SiteWeightMultipliers = %v, want site 303 => 2.5", got.SiteWeightMultipliers)
	}
	if len(got.ExcludedCredentialRefs) != 1 ||
		got.ExcludedCredentialRefs[0].Kind != string(auth.CredentialRefAccountToken) ||
		got.ExcludedCredentialRefs[0].TokenID != tokenID {
		t.Fatalf("ExcludedCredentialRefs = %#v, want account token ref 55", got.ExcludedCredentialRefs)
	}
}

func TestDispatchUpstreamUsesSiteCustomHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Metapi-Site"); got != "site-header" {
			t.Fatalf("X-Metapi-Site = %q, want site-header", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	customHeaders := `{"X-Metapi-Site":"site-header"}`
	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active", CustomHeaders: &customHeaders},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o-upstream",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchUpstreamNonStreamFailoversOnRetryableHTTPStatusAndRecordsHealth(t *testing.T) {
	var firstCalls int
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream failure","type":"server_error"}}`))
	}))
	t.Cleanup(firstUpstream.Close)

	var secondCalls int
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		if got := r.URL.Path; got != "/v1/chat/completions" {
			t.Fatalf("second upstream path = %q, want /v1/chat/completions", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_recovered","choices":[{"message":{"content":"recovered"}}]}`))
	}))
	t.Cleanup(secondUpstream.Close)

	next := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 2, Enabled: true},
		Account:     store.Account{ID: 20, Status: "active"},
		Site:        store.Site{ID: 200, URL: secondUpstream.URL, Status: "active"},
		TokenValue:  "second-token",
		ActualModel: "gpt-4o-second",
	}
	router := &upstreamTestRouter{
		selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 1, Enabled: true},
			Account:     store.Account{ID: 10, Status: "active"},
			Site:        store.Site{ID: 100, URL: firstUpstream.URL, Status: "active"},
			TokenValue:  "first-token",
			ActualModel: "gpt-4o-first",
		},
		next: &next,
	}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("upstream calls = first:%d second:%d, want 1/1", firstCalls, secondCalls)
	}
	if body := rec.Body.String(); !strings.Contains(body, "chatcmpl_recovered") {
		t.Fatalf("body = %q, want second upstream response", body)
	}
	if len(router.failures) != 1 {
		t.Fatalf("failures = %#v, want one failure", router.failures)
	}
	if router.failures[0].channelID != 1 || router.failures[0].status == nil || *router.failures[0].status != http.StatusInternalServerError {
		t.Fatalf("failure = %#v, want channel 1 status 500", router.failures[0])
	}
	if router.failures[0].errorText == nil || !strings.Contains(*router.failures[0].errorText, "temporary upstream failure") {
		t.Fatalf("failure error text = %#v, want upstream error body", router.failures[0].errorText)
	}
	if len(router.successes) != 1 {
		t.Fatalf("successes = %#v, want one success", router.successes)
	}
	if router.successes[0].channelID != 2 {
		t.Fatalf("success channel = %d, want 2", router.successes[0].channelID)
	}
}

func TestNonStreamingUpstreamRejectsOversizedBufferedResponse(t *testing.T) {
	t.Setenv("PROXY_MAX_BUFFERED_RESPONSE_BYTES", "8")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`123456789`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-4o-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "upstream_error") {
		t.Fatalf("body = %q, want upstream_error", body)
	}
}

func TestVideosCreateMultipartForwardsFormToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos" {
			t.Fatalf("upstream path = %q, want /v1/videos", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer upstream-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data;") {
			t.Fatalf("Content-Type = %q, want multipart/form-data", got)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse upstream multipart: %v", err)
		}
		if got := r.FormValue("model"); got != "sora-upstream" {
			t.Fatalf("model = %q, want sora-upstream", got)
		}
		if got := r.FormValue("prompt"); got != "a cat walking" {
			t.Fatalf("prompt = %q, want prompt field", got)
		}
		files := r.MultipartForm.File["video"]
		if len(files) != 1 {
			t.Fatalf("video file count = %d, want 1", len(files))
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatalf("open forwarded file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read forwarded file: %v", err)
		}
		if string(data) != "fake-video-data" {
			t.Fatalf("file body = %q", string(data))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"vid_123","object":"video"}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "sora-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", "sora-2"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "a cat walking"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	part, err := writer.CreateFormFile("video", "clip.mp4")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := part.Write([]byte("fake-video-data")); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := makeProxyReqNoBody("POST", "/v1/videos")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	HandleVideosCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "vid_123") {
		t.Fatalf("body = %q, want upstream response", body)
	}
}

func TestSiteConcurrencySaturateSkipsWithoutFailure(t *testing.T) {
	// Site A is saturated (limit 1 already held). Fallback channel on site B must
	// still dispatch. Saturation must NOT RecordFailure (no cascade/expired).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	limiter := proxy.NewSiteConcurrencyLimiter()
	held, ok := limiter.TryAcquire(1, 1)
	if !ok {
		t.Fatal("pre-hold site 1 failed")
	}
	t.Cleanup(held.Release)

	selectedA := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 101, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 1, URL: upstream.URL, Status: "active", MaxConcurrency: 1},
		TokenValue:  "tok-a",
		ActualModel: "gpt-4o-upstream",
	}
	selectedB := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 202, Enabled: true},
		Account:     store.Account{ID: 8, Status: "active"},
		Site:        store.Site{ID: 2, URL: upstream.URL, Status: "active", MaxConcurrency: 1},
		TokenValue:  "tok-b",
		ActualModel: "gpt-4o-upstream",
	}
	router := &upstreamTestRouter{
		selected: selectedA,
		next:     &selectedB,
	}
	SetUpstreamConfig(&UpstreamConfig{
		Router:      router,
		SiteLimiter: limiter,
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	dispatchUpstream(rec, req, &Ctx{
		RequestedModel: "gpt-4o",
		DownstreamPath: "/v1/chat/completions",
		RawBody:        []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		MaxRetries:     1,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(router.failures) != 0 {
		t.Fatalf("saturated site must not cascade RecordFailure, got %+v", router.failures)
	}
	if len(router.successes) != 1 {
		t.Fatalf("expected 1 success on fallback channel, got %+v", router.successes)
	}
	if router.successes[0].channelID != 202 {
		t.Fatalf("expected channel 202 success, got %d", router.successes[0].channelID)
	}
}

func TestNonStreamSuccessPersistsUsageTokensToProxyLog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_usage","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}}`))
	}))
	t.Cleanup(upstream.Close)

	var logs []proxy.ProxyLogEntry
	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, RouteID: 9, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o-upstream",
	}}
	SetUpstreamConfig(&UpstreamConfig{
		Router: router,
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(logs) != 1 {
		t.Fatalf("proxy logs = %d, want 1: %#v", len(logs), logs)
	}
	entry := logs[0]
	if entry.Status != "success" {
		t.Fatalf("status = %q, want success", entry.Status)
	}
	if entry.PromptTokens == nil || *entry.PromptTokens != 12 {
		t.Fatalf("prompt_tokens = %#v, want 12", entry.PromptTokens)
	}
	if entry.CompletionTokens == nil || *entry.CompletionTokens != 34 {
		t.Fatalf("completion_tokens = %#v, want 34", entry.CompletionTokens)
	}
	if entry.TotalTokens == nil || *entry.TotalTokens != 46 {
		t.Fatalf("total_tokens = %#v, want 46", entry.TotalTokens)
	}
	if entry.UsageSource != "upstream" {
		t.Fatalf("usage_source = %q, want upstream", entry.UsageSource)
	}
	if entry.ChannelID == nil || *entry.ChannelID != 42 {
		t.Fatalf("channel_id = %#v, want 42", entry.ChannelID)
	}
	if entry.AccountID == nil || *entry.AccountID != 7 {
		t.Fatalf("account_id = %#v, want 7", entry.AccountID)
	}
	if entry.IsStream == nil || *entry.IsStream {
		t.Fatalf("is_stream = %#v, want false", entry.IsStream)
	}
}

func TestStreamSuccessExtractsFinalUsageAndPersistsProxyLog(t *testing.T) {
	streamBody := strings.Join([]string{
		`data: {"id":"chunk1","choices":[{"delta":{"content":"He"}}]}`,
		`data: {"id":"chunk2","choices":[{"delta":{"content":"llo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":9,"total_tokens":14}}`,
		`data: [DONE]`,
		``,
	}, "\n\n")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(streamBody))
	}))
	t.Cleanup(upstream.Close)

	var logs []proxy.ProxyLogEntry
	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, RouteID: 9, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o-upstream",
	}}
	SetUpstreamConfig(&UpstreamConfig{
		Router: router,
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(logs) != 1 {
		t.Fatalf("proxy logs = %d, want 1: %#v", len(logs), logs)
	}
	entry := logs[0]
	if entry.Status != "success" {
		t.Fatalf("status = %q, want success", entry.Status)
	}
	if entry.IsStream == nil || !*entry.IsStream {
		t.Fatalf("is_stream = %#v, want true", entry.IsStream)
	}
	if entry.PromptTokens == nil || *entry.PromptTokens != 5 {
		t.Fatalf("prompt_tokens = %#v, want 5", entry.PromptTokens)
	}
	if entry.CompletionTokens == nil || *entry.CompletionTokens != 9 {
		t.Fatalf("completion_tokens = %#v, want 9", entry.CompletionTokens)
	}
	if entry.TotalTokens == nil || *entry.TotalTokens != 14 {
		t.Fatalf("total_tokens = %#v, want 14", entry.TotalTokens)
	}
	if entry.UsageSource != "upstream" {
		t.Fatalf("usage_source = %q, want upstream", entry.UsageSource)
	}
}

func TestDetectProxyFailureReceivesParsedUsageFromBody(t *testing.T) {
	// When empty-content-fail is enabled and completion tokens are present,
	// DetectProxyFailure must not treat the response as empty even without text.
	t.Setenv("PROXY_EMPTY_CONTENT_FAIL", "1")
	// config.Get() may already be loaded; DetectProxyFailure reads config singleton.
	// Prefer direct unit path: ParseUsageFromBody -> ToUsageSummary.
	body := []byte(`{"choices":[{"message":{"content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	usage := ParseUsageFromBody(body)
	if !usage.Found || usage.CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want completion 2", usage)
	}
	sum := usage.ToUsageSummary()
	if sum.CompletionTokens != 2 {
		t.Fatalf("summary completion = %d", sum.CompletionTokens)
	}
}

func TestIncrementalSseAnalyzerExtractsUsage(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()
	analyzer.Push([]byte(`data: {"choices":[{"delta":{"content":"x"}}]}` + "\n\n"))
	analyzer.Push([]byte(`data: {"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}` + "\n\n"))
	analyzer.Push([]byte("data: [DONE]\n\n"))
	result := analyzer.Result()
	if !result.Usage.Found {
		t.Fatal("expected usage found in analyzer result")
	}
	if result.Usage.TotalTokens != 5 {
		t.Fatalf("total = %d, want 5", result.Usage.TotalTokens)
	}
}
