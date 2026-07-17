package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

// RegisterTestRoutes registers all /api/test routes.
// Sync proxy/chat probes alias the forced-channel harness (#119 / #185).
// Stream and async job surfaces return honest 501 residuals (no fake success).
func RegisterTestRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &testHandler{
		channel: &channelTestHandler{db: db, cfg: cfg},
	}

	// Proxy test endpoints
	r.Post("/api/test/proxy", handler.proxyTest)
	r.Post("/api/test/proxy/stream", handler.proxyTestStream)
	r.Post("/api/test/proxy/jobs", handler.proxyTestJob)
	r.Get("/api/test/proxy/jobs/{jobId}", handler.proxyTestJobStatus)
	r.Delete("/api/test/proxy/jobs/{jobId}", handler.proxyTestJobCancel)

	// Chat test endpoints
	r.Post("/api/test/chat", handler.chatTest)
	r.Post("/api/test/chat/stream", handler.chatTestStream)
	r.Post("/api/test/chat/jobs", handler.chatTestJob)
	r.Get("/api/test/chat/jobs/{jobId}", handler.chatTestJobStatus)
	r.Delete("/api/test/chat/jobs/{jobId}", handler.chatTestJobCancel)
}

type testHandler struct {
	channel *channelTestHandler
}

// flexibleTestBody accepts both harness-shaped fields and the richer frontend
// proxy/chat envelopes (forcedChannelId, jsonBody, messages).
type flexibleTestBody struct {
	ChannelID       *int64          `json:"channelId"`
	ForcedChannelID *int64          `json:"forcedChannelId"`
	SiteID          *int64          `json:"siteId"`
	Model           string          `json:"model"`
	Prompt          string          `json:"prompt"`
	Mode            string          `json:"mode"`
	TimeoutMs       *int64          `json:"timeoutMs"`
	Path            string          `json:"path"`
	JSONBody        json.RawMessage `json:"jsonBody"`
	Messages        []testMessage   `json:"messages"`
}

type testMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// POST /api/test/proxy
// Accepts channelId/siteId/forcedChannelId + model and delegates to the
// forced-channel harness. Full path/multipart matrix testing remains residual.
func (h *testHandler) proxyTest(w http.ResponseWriter, r *http.Request) {
	h.handleSyncProbe(w, r, "proxy")
}

// POST /api/test/proxy/stream
func (h *testHandler) proxyTestStream(w http.ResponseWriter, r *http.Request) {
	writeNotImplementedResidual(w,
		"Proxy stream test is not implemented in Go",
		"SSE /api/test/proxy/stream matrix; use non-stream POST /api/test/proxy with channelId/siteId or POST /api/admin/test-channel",
	)
}

// POST /api/test/proxy/jobs
func (h *testHandler) proxyTestJob(w http.ResponseWriter, r *http.Request) {
	writeNotImplementedResidual(w,
		"Proxy test job queue is not implemented in Go",
		"async /api/test/proxy/jobs; use sync POST /api/test/proxy or POST /api/admin/test-channel",
	)
}

// GET /api/test/proxy/jobs/:jobId
func (h *testHandler) proxyTestJobStatus(w http.ResponseWriter, r *http.Request) {
	writeJobNotFound(w)
}

// DELETE /api/test/proxy/jobs/:jobId
func (h *testHandler) proxyTestJobCancel(w http.ResponseWriter, r *http.Request) {
	writeJobNotFound(w)
}

// POST /api/test/chat
// Alias of the forced-channel harness when channelId/siteId/forcedChannelId is set.
func (h *testHandler) chatTest(w http.ResponseWriter, r *http.Request) {
	h.handleSyncProbe(w, r, "chat")
}

// POST /api/test/chat/stream
func (h *testHandler) chatTestStream(w http.ResponseWriter, r *http.Request) {
	writeNotImplementedResidual(w,
		"Chat stream test is not implemented in Go",
		"SSE /api/test/chat/stream; use non-stream POST /api/test/chat with channelId/siteId or POST /api/admin/test-channel",
	)
}

// POST /api/test/chat/jobs
func (h *testHandler) chatTestJob(w http.ResponseWriter, r *http.Request) {
	writeNotImplementedResidual(w,
		"Chat test job queue is not implemented in Go",
		"async /api/test/chat/jobs; use sync POST /api/test/chat or POST /api/admin/test-channel",
	)
}

// GET /api/test/chat/jobs/:jobId
func (h *testHandler) chatTestJobStatus(w http.ResponseWriter, r *http.Request) {
	writeJobNotFound(w)
}

// DELETE /api/test/chat/jobs/:jobId
func (h *testHandler) chatTestJobCancel(w http.ResponseWriter, r *http.Request) {
	writeJobNotFound(w)
}

func (h *testHandler) handleSyncProbe(w http.ResponseWriter, r *http.Request, surface string) {
	var body flexibleTestBody
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req, ok := mapFlexibleToChannelTest(body)
	if !ok {
		writeNotImplementedResidual(w,
			strings.ToUpper(surface[:1])+surface[1:]+" test requires channelId, siteId, or forcedChannelId for the forced-channel harness",
			"full /api/test/"+surface+" path/multipart/routing matrix without a forced channel; provide channelId/siteId/forcedChannelId or use POST /api/admin/test-channel",
		)
		return
	}

	h.channel.runChannelTest(w, r, req)
}

func mapFlexibleToChannelTest(body flexibleTestBody) (channelTestRequest, bool) {
	channelID := firstPositiveID(body.ChannelID, body.ForcedChannelID)
	siteID := body.SiteID
	if (channelID == nil || *channelID <= 0) && (siteID == nil || *siteID <= 0) {
		return channelTestRequest{}, false
	}

	model := strings.TrimSpace(body.Model)
	if model == "" && len(body.JSONBody) > 0 {
		var nested struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body.JSONBody, &nested); err == nil {
			model = strings.TrimSpace(nested.Model)
		}
	}

	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		prompt = lastUserPrompt(body.Messages)
	}
	if prompt == "" && len(body.JSONBody) > 0 {
		var nested struct {
			Messages []testMessage `json:"messages"`
			Prompt   string        `json:"prompt"`
			Input    string        `json:"input"`
		}
		if err := json.Unmarshal(body.JSONBody, &nested); err == nil {
			if p := strings.TrimSpace(nested.Prompt); p != "" {
				prompt = p
			} else if p := strings.TrimSpace(nested.Input); p != "" {
				prompt = p
			} else {
				prompt = lastUserPrompt(nested.Messages)
			}
		}
	}

	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	if mode == "" {
		path := strings.ToLower(strings.TrimSpace(body.Path))
		if strings.Contains(path, "/models") && !strings.Contains(path, "chat") {
			mode = channelTestModeModels
		} else {
			mode = channelTestModeChat
		}
	}

	return channelTestRequest{
		ChannelID: channelID,
		SiteID:    siteID,
		Model:     model,
		Prompt:    prompt,
		Mode:      mode,
		TimeoutMs: body.TimeoutMs,
	}, true
}

func firstPositiveID(ids ...*int64) *int64 {
	for _, id := range ids {
		if id != nil && *id > 0 {
			return id
		}
	}
	return nil
}

func lastUserPrompt(messages []testMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role := strings.ToLower(strings.TrimSpace(messages[i].Role))
		if role != "" && role != "user" {
			continue
		}
		if p := contentToPrompt(messages[i].Content); p != "" {
			return p
		}
	}
	return ""
}

func contentToPrompt(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	// OpenAI-style multipart content: [{"type":"text","text":"..."}]
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if t, _ := part["type"].(string); t != "" && t != "text" {
				continue
			}
			if text, ok := part["text"].(string); ok {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(strings.TrimSpace(text))
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func writeNotImplementedResidual(w http.ResponseWriter, message, residual string) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"success":  false,
		"message":  message,
		"residual": residual,
	})
}

func writeJobNotFound(w http.ResponseWriter) {
	// Honest empty job surface: no in-process job registry for /api/test/* yet.
	writeJSON(w, http.StatusNotFound, map[string]any{
		"success": false,
		"error": map[string]any{
			"message": "job not found",
			"type":    "not_found",
		},
	})
}
