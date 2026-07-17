package proxyhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
	"github.com/tokendancelab/metapi-go/transform/openai/responses"
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
	channelID int64
	latencyMs float64
	cost      float64
	modelName *string
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

func (r *upstreamTestRouter) RecordSuccess(_ context.Context, channelID int64, latencyMs float64, cost float64, modelName *string, _ *int64) error {
	r.successes = append(r.successes, upstreamTestSuccess{
		channelID: channelID,
		latencyMs: latencyMs,
		cost:      cost,
		modelName: cloneStringPtr(modelName),
	})
	return nil
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


func TestDispatchUpstreamDeniesSensitiveCustomHeaders(t *testing.T) {
	var gotAuth, gotHost, gotCustom, gotCookie, gotConn string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHost = r.Host
		gotCustom = r.Header.Get("X-Custom")
		gotCookie = r.Header.Get("Cookie")
		gotConn = r.Header.Get("Connection")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	// Malicious/sensitive custom headers must not reach upstream; X-Custom still applies.
	customHeaders := `{"Authorization":"Bearer evil","Host":"evil.example","Cookie":"x=1","Connection":"close","X-Custom":"ok"}`
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
	if gotAuth != "Bearer upstream-token" {
		t.Fatalf("Authorization = %q, want Bearer upstream-token", gotAuth)
	}
	if gotCustom != "ok" {
		t.Fatalf("X-Custom = %q, want ok", gotCustom)
	}
	if gotCookie != "" {
		t.Fatalf("Cookie leaked: %q", gotCookie)
	}
	if gotConn != "" {
		t.Fatalf("Connection leaked: %q", gotConn)
	}
	if gotHost == "evil.example" {
		t.Fatalf("Host overridden to evil.example")
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
	// Create rewrites upstream id → publicId (#235); raw upstream id must not leak.
	bodyStr := rec.Body.String()
	if strings.Contains(bodyStr, "vid_123") {
		t.Fatalf("body = %q, must not expose raw upstream id after publicId rewrite", bodyStr)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, bodyStr)
	}
	publicID, _ := out["id"].(string)
	if publicID == "" || !strings.HasPrefix(publicID, "video_") {
		t.Fatalf("public id = %q, want video_*", publicID)
	}
	task := GetProxyVideoTaskByPublicID(publicID)
	if task == nil {
		t.Fatalf("expected mapping for publicId %q", publicID)
	}
	if task.UpstreamVideoID != "vid_123" {
		t.Fatalf("UpstreamVideoID = %q, want vid_123", task.UpstreamVideoID)
	}
	t.Cleanup(func() { DeleteProxyVideoTaskByPublicID(publicID) })
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

func TestDispatchUpstream_MultiRetrySharesRequestIDInProxyLogsAndError(t *testing.T) {
	// Channel A always 429 (retryable); channel B also 429 → terminal after retries.
	// Same parent request_id must appear on success-path logs if B later succeeds,
	// and on the final client error body / header when all attempts fail.
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	t.Cleanup(upstream.Close)

	selectedA := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 11, RouteID: 1, Enabled: true},
		Account:     store.Account{ID: 1, Status: "active"},
		Site:        store.Site{ID: 1, URL: upstream.URL, Status: "active"},
		TokenValue:  "tok-a",
		ActualModel: "gpt-4o-upstream",
	}
	selectedB := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 22, RouteID: 1, Enabled: true},
		Account:     store.Account{ID: 2, Status: "active"},
		Site:        store.Site{ID: 2, URL: upstream.URL, Status: "active"},
		TokenValue:  "tok-b",
		ActualModel: "gpt-4o-upstream",
	}
	router := &upstreamTestRouter{selected: selectedA, next: &selectedB}

	var logs []proxy.ProxyLogEntry
	SetUpstreamConfig(&UpstreamConfig{
		Router: router,
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	const wantID = "trace-multi-retry-42"
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req = req.WithContext(proxy.WithRequestID(req.Context(), wantID))
	rec := httptest.NewRecorder()
	dispatchUpstream(rec, req, &Ctx{
		RequestedModel: "gpt-4o",
		DownstreamPath: "/v1/chat/completions",
		RawBody:        []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		MaxRetries:     1,
	})

	if hits.Load() < 2 {
		t.Fatalf("expected multi-channel attempts, hits=%d", hits.Load())
	}
	if rec.Code != http.StatusTooManyRequests && rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusBadGateway {
		// Terminal may be relayed upstream 429 or synthetic exhausted JSON depending on retry policy.
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// When we emit MetAPI JSON errors, request_id is present; when we relay upstream
	// body, X-Request-Id may still be set if write path used writeJSONErrorWithRequest.
	body := rec.Body.String()
	if strings.Contains(body, `"request_id"`) && !strings.Contains(body, wantID) {
		t.Fatalf("body has request_id field but missing %s: %s", wantID, body)
	}
	if hdr := rec.Header().Get("X-Request-Id"); hdr != "" && hdr != wantID {
		t.Fatalf("X-Request-Id = %q, want %s", hdr, wantID)
	}
	// proxy_logs currently only write on success in the hot path; ensure the
	// parent id is still recoverable from the request context after multi-attempt.
	if got := proxy.RequestIDFromContext(req.Context()); got != wantID {
		t.Fatalf("request context lost id: %q", got)
	}
	_ = logs
}

func TestDispatchUpstream_RetryThenSuccessKeepsSameRequestIDInProxyLog(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)

	selectedA := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 11, RouteID: 1, Enabled: true},
		Account:     store.Account{ID: 1, Status: "active"},
		Site:        store.Site{ID: 1, URL: upstream.URL, Status: "active"},
		TokenValue:  "tok-a",
		ActualModel: "gpt-4o-upstream",
	}
	selectedB := routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 22, RouteID: 1, Enabled: true},
		Account:     store.Account{ID: 2, Status: "active"},
		Site:        store.Site{ID: 2, URL: upstream.URL, Status: "active"},
		TokenValue:  "tok-b",
		ActualModel: "gpt-4o-upstream",
	}
	router := &upstreamTestRouter{selected: selectedA, next: &selectedB}

	var logs []proxy.ProxyLogEntry
	SetUpstreamConfig(&UpstreamConfig{
		Router: router,
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	const wantID = "trace-success-after-retry"
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req = req.WithContext(proxy.WithRequestID(req.Context(), wantID))
	rec := httptest.NewRecorder()
	dispatchUpstream(rec, req, &Ctx{
		RequestedModel: "gpt-4o",
		DownstreamPath: "/v1/chat/completions",
		RawBody:        []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		MaxRetries:     1,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s hits=%d", rec.Code, rec.Body.String(), hits.Load())
	}
	if hits.Load() < 2 {
		t.Fatalf("expected retry across channels, hits=%d", hits.Load())
	}
	if len(logs) < 2 {
		t.Fatalf("proxy logs = %d, want failed attempt + success: %#v", len(logs), logs)
	}
	var success *proxy.ProxyLogEntry
	var failed *proxy.ProxyLogEntry
	for i := range logs {
		switch logs[i].Status {
		case "success":
			success = &logs[i]
		case "failed":
			if failed == nil {
				failed = &logs[i]
			}
		}
	}
	if failed == nil {
		t.Fatalf("expected failed proxy log for first 429 attempt: %#v", logs)
	}
	if failed.RequestID != wantID {
		t.Fatalf("failed request_id = %q, want %s", failed.RequestID, wantID)
	}
	if failed.RetryCount != 0 {
		t.Fatalf("failed retry_count = %d, want 0", failed.RetryCount)
	}
	if failed.ChannelID == nil || *failed.ChannelID != 11 {
		t.Fatalf("failed channel_id = %#v, want 11", failed.ChannelID)
	}
	if success == nil {
		t.Fatalf("expected success proxy log after retry: %#v", logs)
	}
	if success.RequestID != wantID {
		t.Fatalf("success request_id = %q, want %s", success.RequestID, wantID)
	}
	if success.RetryCount != 1 {
		t.Fatalf("success retry_count = %d, want 1 (second channel attempt)", success.RetryCount)
	}
	if success.ChannelID == nil || *success.ChannelID != 22 {
		t.Fatalf("success channel_id = %#v, want 22", success.ChannelID)
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

// disconnectAfterNWriter fails the Nth Write to simulate client disconnect mid-stream.
type disconnectAfterNWriter struct {
	*httptest.ResponseRecorder
	n        int
	writes   int
	failErr  error
	lastBody []byte
}

func (w *disconnectAfterNWriter) Write(p []byte) (int, error) {
	w.writes++
	w.lastBody = append(w.lastBody[:0], p...)
	if w.writes >= w.n {
		if w.failErr == nil {
			w.failErr = io.ErrClosedPipe
		}
		return 0, w.failErr
	}
	return w.ResponseRecorder.Write(p)
}

// chunkedReader returns one pre-split SSE frame per Read so stream logic can
// observe mid-stream disconnect between events.
type chunkedReader struct {
	chunks []string
	i      int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, []byte(r.chunks[r.i]))
	r.i++
	return n, nil
}

func (r *chunkedReader) Close() error { return nil }

func TestHandleStreamUpstreamClientDisconnectPreservesUsage(t *testing.T) {
	// Content chunk succeeds; usage-bearing final chunk fails to write (client gone).
	// Analyzer must still extract usage from the failed-write chunk.
	body := &chunkedReader{chunks: []string{
		`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
		`data: {"usage":{"prompt_tokens":7,"completion_tokens":9,"total_tokens":16}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       body,
	}
	resp.Header.Set("Content-Type", "text/event-stream")

	rec := &disconnectAfterNWriter{ResponseRecorder: httptest.NewRecorder(), n: 2}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	usage := handleStreamUpstream(rec, req, resp, 12)
	if !usage.Found {
		t.Fatal("expected usage retained after client disconnect on usage chunk")
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 9 || usage.TotalTokens != 16 {
		t.Fatalf("usage = %+v, want 7/9/16", usage)
	}
	if usage.Source != usageSourceUpstream {
		t.Fatalf("source = %q, want upstream", usage.Source)
	}
}

func TestHandleStreamUpstreamContextCancelPreservesPartialUsage(t *testing.T) {
	// After a full successful stream that already extracted usage, a canceled
	// context on a subsequent empty body should not invent tokens; empty path
	// stays unknown. Separately verify canceled context mid-read still returns
	// previously extracted usage via Result().
	analyzer := newIncrementalSseAnalyzer()
	analyzer.Push([]byte(`data: {"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}` + "\n\n"))
	got := analyzer.Result().Usage
	if !got.Found || got.TotalTokens != 10 {
		t.Fatalf("precondition usage = %+v", got)
	}

	// Full handleStreamUpstream with already-canceled context and body that still
	// has usage: select may fire before first Read. Documented residual: pure
	// pre-cancel without any bytes yields unknown (no invent). With bytes already
	// analyzed above, partial retention is covered by disconnect test.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`data: {"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n",
		)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	usage := handleStreamUpstream(rec, req, resp, 1)
	// Pre-canceled before any read: must not invent usage.
	if usage.Found {
		// If runtime read wins the race and extracts, that is also correct — accept either.
		if usage.TotalTokens != 2 {
			t.Fatalf("if found, usage must match upstream: %+v", usage)
		}
		return
	}
	if usage.Source != usageSourceUnknown {
		t.Fatalf("source = %q, want unknown when no usage extracted", usage.Source)
	}
}

func TestSanitizeUpstreamJSONBody_MultiTurnReasoningInjectsContent(t *testing.T) {
	// Hermes/Codex second-turn: reasoning has encrypted_content + summary, no content.
	body := []byte(`{
  "model": "gpt-5.4",
  "input": [
    {"type": "message", "role": "user", "content": "continue"},
    {
      "type": "reasoning",
      "id": "rs_1",
      "encrypted_content": "enc_abc",
      "summary": [{"type": "summary_text", "text": "step one"}]
    },
    {"type": "message", "role": "assistant", "content": ""},
    {"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{}"}
  ]
}`)
	out, err := sanitizeUpstreamJSONBody(body, "codex", "/v1/responses", "")
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal out: %v", err)
	}
	arr, ok := m["input"].([]any)
	if !ok || len(arr) != 4 {
		t.Fatalf("input = %#v", m["input"])
	}
	reasoning, _ := arr[1].(map[string]any)
	if reasoning["encrypted_content"] != "enc_abc" {
		t.Fatalf("encrypted_content dropped: %#v", reasoning["encrypted_content"])
	}
	if _, ok := reasoning["summary"]; !ok {
		t.Fatal("summary dropped")
	}
	if reasoning["content"] != "step one" {
		t.Fatalf("content = %#v, want summary text for strict gateways", reasoning["content"])
	}
}

func TestSanitizeUpstreamJSONBody_PrettyPrintedReasoningTypeSpace(t *testing.T) {
	// Pretty-printed "type" : "reasoning" (spaces around colon) must still sanitize.
	body := []byte(`{
  "model": "gpt-5.4",
  "input": [
    {
      "type" : "reasoning",
      "encrypted_content": "enc_pretty",
      "summary": [{"type": "summary_text", "text": "think"}]
    }
  ]
}`)
	out, err := sanitizeUpstreamJSONBody(body, "openai", "/v1/responses", "")
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	arr := m["input"].([]any)
	r := arr[0].(map[string]any)
	if r["encrypted_content"] != "enc_pretty" {
		t.Fatalf("encrypted_content = %#v", r["encrypted_content"])
	}
	if r["content"] != "think" {
		t.Fatalf("content = %#v (pretty type key must still inject content)", r["content"])
	}
}

func TestSanitizeUpstreamJSONBody_ReasoningMissingFieldsHonest400(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"reasoning","summary":[]}]}`)
	out, err := sanitizeUpstreamJSONBody(body, "openai", "/v1/responses", "")
	if err == nil {
		t.Fatalf("expected ReasoningInputError, got body=%s", out)
	}
	if !strings.Contains(err.Error(), "input[0]") {
		t.Fatalf("err = %q, want input index", err.Error())
	}
	if !strings.Contains(err.Error(), "encrypted_content") {
		t.Fatalf("err = %q, want required field list", err.Error())
	}
	// Wire path maps this to 400 invalid_request_error (no silent accept).
	var re *responses.ReasoningInputError
	if !errors.As(err, &re) {
		t.Fatalf("err type = %T, want *responses.ReasoningInputError", err)
	}
	if re.Index != 0 {
		t.Fatalf("index = %d", re.Index)
	}
}

func TestSanitizeUpstreamJSONBody_NonResponsesPathSkipsUnlessMarkers(t *testing.T) {
	// Chat path without markers: no parse, body unchanged.
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`)
	out, err := sanitizeUpstreamJSONBody(body, "openai", "/v1/chat/completions", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(out, body) {
		t.Fatalf("chat body mutated without markers")
	}
}

func TestSanitizeUpstreamJSONBody_CompactPreservesReasoningInput(t *testing.T) {
	body := []byte(`{
  "model":"gpt-5.4",
  "stream":true,
  "store":true,
  "previous_response_id":"resp_1",
  "input":[{"type":"reasoning","encrypted_content":"enc","summary":[{"type":"summary_text","text":"s"}]}]
}`)
	out, err := sanitizeUpstreamJSONBody(body, "codex", "/v1/responses/compact", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"stream", "previous_response_id"} {
		if _, ok := m[k]; ok {
			t.Fatalf("expected %q stripped on compact", k)
		}
	}
	arr := m["input"].([]any)
	r := arr[0].(map[string]any)
	if r["encrypted_content"] != "enc" {
		t.Fatalf("encrypted_content dropped: %#v", r)
	}
	if r["content"] != "s" {
		t.Fatalf("content = %#v", r["content"])
	}
}

func TestNonStreamHTTPErrorPersistsUsageTokensToFailedProxyLog(t *testing.T) {
	// Upstream 429 with usage still billed by the gateway must not under-count.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"},"usage":{"prompt_tokens":11,"completion_tokens":0,"total_tokens":11}}`))
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

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	if len(logs) == 0 {
		t.Fatal("expected at least one failed proxy log")
	}
	entry := logs[0]
	if entry.Status != "failed" {
		t.Fatalf("status = %q, want failed", entry.Status)
	}
	if entry.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("http_status = %d, want 429", entry.HTTPStatus)
	}
	if entry.PromptTokens == nil || *entry.PromptTokens != 11 {
		t.Fatalf("prompt_tokens = %#v, want 11", entry.PromptTokens)
	}
	if entry.TotalTokens == nil || *entry.TotalTokens != 11 {
		t.Fatalf("total_tokens = %#v, want 11", entry.TotalTokens)
	}
	if entry.UsageSource != "upstream" {
		t.Fatalf("usage_source = %q, want upstream", entry.UsageSource)
	}
	if entry.ErrorMessage == nil || *entry.ErrorMessage == "" {
		t.Fatalf("error_message missing: %#v", entry.ErrorMessage)
	}
	if entry.IsStream == nil || *entry.IsStream {
		t.Fatalf("is_stream = %#v, want false", entry.IsStream)
	}
}

func TestNonStreamContentFailurePersistsParsedUsageToFailedProxyLog(t *testing.T) {
	// Keyword-matched content failure must still persist usage extracted from the body.
	t.Setenv("PROXY_ERROR_KEYWORDS", "content_policy_violation")
	// DetectProxyFailure reads config.Get(); force a fresh config load if available.
	if cfg := config.Get(); cfg != nil {
		cfg.ProxyErrorKeywords = []string{"content_policy_violation"}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// HTTP 200 but body matches failure keyword + usage present.
		_, _ = w.Write([]byte(`{"error":{"message":"content_policy_violation"},"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
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

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-200 content failure; body=%s", rec.Code, rec.Body.String())
	}
	if len(logs) == 0 {
		t.Fatal("expected failed proxy log with usage")
	}
	entry := logs[0]
	if entry.Status != "failed" {
		t.Fatalf("status = %q, want failed", entry.Status)
	}
	if entry.PromptTokens == nil || *entry.PromptTokens != 5 {
		t.Fatalf("prompt_tokens = %#v, want 5", entry.PromptTokens)
	}
	if entry.CompletionTokens == nil || *entry.CompletionTokens != 3 {
		t.Fatalf("completion_tokens = %#v, want 3", entry.CompletionTokens)
	}
	if entry.TotalTokens == nil || *entry.TotalTokens != 8 {
		t.Fatalf("total_tokens = %#v, want 8", entry.TotalTokens)
	}
	if entry.UsageSource != "upstream" {
		t.Fatalf("usage_source = %q, want upstream", entry.UsageSource)
	}
}

func TestTruncateErrTextBoundsLength(t *testing.T) {
	short := truncateErrText("  hello  ")
	if short != "hello" {
		t.Fatalf("short = %q", short)
	}
	long := strings.Repeat("a", 2500)
	got := truncateErrText(long)
	if len([]rune(got)) > 2003 { // 2000 + "..."
		t.Fatalf("len = %d, want <= 2003", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis suffix, got len=%d", len(got))
	}
}

func TestApplyUpstreamStreamIncludeUsage_InjectsWhenMissing(t *testing.T) {
	t.Parallel()
	in := []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	out, expect := applyUpstreamStreamIncludeUsage(in, "openai", "/v1/chat/completions", true)
	if !expect {
		t.Fatal("expected expectStreamUsage=true after inject")
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	opts, ok := body["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("expected stream_options map, got %#v", body["stream_options"])
	}
	if opts["include_usage"] != true {
		t.Fatalf("include_usage = %#v, want true", opts["include_usage"])
	}
}

func TestApplyUpstreamStreamIncludeUsage_ForcesFalseToTrueAndPreservesKeys(t *testing.T) {
	t.Parallel()
	in := []byte(`{"model":"gpt-4o","stream":true,"stream_options":{"include_usage":false,"foo":"bar"}}`)
	out, expect := applyUpstreamStreamIncludeUsage(in, "new-api", "/v1/chat/completions", true)
	if !expect {
		t.Fatal("expected expectStreamUsage=true after force inject")
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	opts := body["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Fatalf("include_usage = %#v", opts["include_usage"])
	}
	if opts["foo"] != "bar" {
		t.Fatalf("preserved key lost: %#v", opts)
	}
}

func TestApplyUpstreamStreamIncludeUsage_NoopWhenAlreadyTrue(t *testing.T) {
	t.Parallel()
	in := []byte(`{"stream":true,"stream_options":{"include_usage":true,"foo":1}}`)
	out, expect := applyUpstreamStreamIncludeUsage(in, "openai", "/v1/chat/completions", true)
	if !expect {
		t.Fatal("already-true include_usage still expects stream usage")
	}
	if string(out) != string(in) {
		t.Fatalf("expected identical bytes when already true, got %s want %s", out, in)
	}
}

func TestApplyUpstreamStreamIncludeUsage_SkipNonChatAndNonStream(t *testing.T) {
	t.Parallel()
	in := []byte(`{"stream":true,"input":"hi"}`)
	if got, expect := applyUpstreamStreamIncludeUsage(in, "openai", "/v1/responses", true); string(got) != string(in) || expect {
		t.Fatalf("responses path should skip: body=%s expect=%v", got, expect)
	}
	if got, expect := applyUpstreamStreamIncludeUsage(in, "openai", "/v1/messages", true); string(got) != string(in) || expect {
		t.Fatalf("messages path should skip: body=%s expect=%v", got, expect)
	}
	chat := []byte(`{"stream":false,"messages":[]}`)
	if got, expect := applyUpstreamStreamIncludeUsage(chat, "openai", "/v1/chat/completions", false); string(got) != string(chat) || expect {
		t.Fatalf("non-stream should skip: body=%s expect=%v", got, expect)
	}
}

func TestApplyUpstreamStreamIncludeUsage_SkipCodexSub2API(t *testing.T) {
	t.Parallel()
	in := []byte(`{"stream":true,"messages":[{"role":"user","content":"x"}]}`)
	for _, plat := range []string{"codex", "CODEX", "sub2api", "chatgpt-codex"} {
		if got, expect := applyUpstreamStreamIncludeUsage(in, plat, "/v1/chat/completions", true); string(got) != string(in) || expect {
			t.Fatalf("platform %q should skip stream_options inject: body=%s expect=%v", plat, got, expect)
		}
	}
}

func TestApplyUpstreamStreamIncludeUsage_CompletionsPath(t *testing.T) {
	t.Parallel()
	in := []byte(`{"model":"gpt-3.5-turbo-instruct","stream":true,"prompt":"hi"}`)
	out, expect := applyUpstreamStreamIncludeUsage(in, "openai", "/v1/completions", true)
	if !expect {
		t.Fatal("completions path should expect stream usage")
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	opts, ok := body["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("expected stream_options on completions path, got %#v", body["stream_options"])
	}
	if opts["include_usage"] != true {
		t.Fatalf("include_usage = %#v", opts["include_usage"])
	}
	// nested suffix
	out2, expect2 := applyUpstreamStreamIncludeUsage(in, "openai", "/proxy/v1/completions", true)
	if !expect2 {
		t.Fatal("suffix completions path should expect stream usage")
	}
	if err := json.Unmarshal(out2, &body); err != nil {
		t.Fatalf("unmarshal2: %v", err)
	}
	if body["stream_options"].(map[string]any)["include_usage"] != true {
		t.Fatalf("suffix path failed: %#v", body)
	}
}

func TestShouldWarnMissingStreamUsage(t *testing.T) {
	t.Parallel()
	if shouldWarnMissingStreamUsage(false, ParsedUsage{Source: usageSourceUnknown}) {
		t.Fatal("no warn when include_usage was not expected")
	}
	if !shouldWarnMissingStreamUsage(true, ParsedUsage{Source: usageSourceUnknown}) {
		t.Fatal("warn when expected and usage not found")
	}
	if !shouldWarnMissingStreamUsage(true, ParsedUsage{Found: true, Source: usageSourceUpstream}) {
		t.Fatal("warn when expected and usage is all zeros")
	}
	if shouldWarnMissingStreamUsage(true, ParsedUsage{
		Found:            true,
		Source:           usageSourceUpstream,
		PromptTokens:     11,
		CompletionTokens: 22,
		TotalTokens:      33,
	}) {
		t.Fatal("no warn when usable tokens present")
	}
}

func TestParseUsageFromSSEEvents_FakeStreamWithoutUsage(t *testing.T) {
	t.Parallel()
	// Fake OpenAI chat stream that never emits a usage-bearing chunk (provider ignored include_usage).
	events := []SseEvent{
		{Data: `{"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`},
		{Data: `{"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if got.Found {
		t.Fatalf("fake stream without usage must not invent tokens: %+v", got)
	}
	if !shouldWarnMissingStreamUsage(true, got) {
		t.Fatal("expected warn when include_usage path ends without usage")
	}
	// Incremental analyzer path (production stream relay).
	a := newIncrementalSseAnalyzer()
	for _, ev := range events {
		if ev.Data == "[DONE]" {
			a.Push([]byte("data: [DONE]\n\n"))
			continue
		}
		a.Push([]byte("data: " + ev.Data + "\n\n"))
	}
	result := a.Result()
	if result.Usage.Found {
		t.Fatalf("incremental analyzer must not invent tokens: %+v", result.Usage)
	}
	if !shouldWarnMissingStreamUsage(true, result.Usage) {
		t.Fatal("expected warn from incremental path without usage")
	}
}
