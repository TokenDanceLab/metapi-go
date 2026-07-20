package proxyhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/tokendancelab/metapi-go/config"
)

// Codex upstream WebSocket runtime (parity Wave C3).
//
// Process-local session store + previous_response_id memory (single-instance honesty).
// Dial failures with zero events are intended to fall back to the HTTP SSE bridge.
// Forbidden: invent terminal frames on empty failure.

const (
	codexWSDefaultBeta  = "responses_websockets=2026-02-06"
	codexWSDialTimeout  = 20 * time.Second
	codexWSReadTimeout  = 10 * time.Minute
	codexWSMaxMsgBytes  = 8 << 20
	codexSessionTTLMin  = 5 * time.Minute
	codexSessionMaxKeys = 10_000
)

// CodexWebsocketRuntimeError is returned when an upstream Codex wss turn fails.
// When Events is empty and Status is set, the caller should prefer HTTP bridge fallback.
type CodexWebsocketRuntimeError struct {
	Message string
	Status  int
	Events  []map[string]any
	Payload any
}

func (e *CodexWebsocketRuntimeError) Error() string {
	if e == nil {
		return "codex websocket runtime error"
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return "codex websocket runtime error"
}

// CodexWebsocketRuntimeResult is a successful terminal turn.
type CodexWebsocketRuntimeResult struct {
	Events        []map[string]any
	ReusedSession bool
}

// CodexWebsocketSendInput is one upstream wss turn.
type CodexWebsocketSendInput struct {
	SessionID  string
	RequestURL string
	Headers    map[string]string
	Body       map[string]any
}

// ---- response id store (process-local) ----

type sessionResponseEntry struct {
	responseID  string
	updatedAtMs int64
}

var (
	codexSessionResponseMu  sync.Mutex
	codexSessionResponseIDs = map[string]sessionResponseEntry{}
)

func codexSessionTTL() time.Duration {
	ttlMs := config.DefaultProxyStickySessionTtlMs
	if cfg := safeConfigGet(); cfg != nil && cfg.ProxyStickySessionTtlMs > 0 {
		ttlMs = cfg.ProxyStickySessionTtlMs
	}
	d := time.Duration(ttlMs) * time.Millisecond
	if d < codexSessionTTLMin {
		return codexSessionTTLMin
	}
	return d
}

func safeConfigGet() *config.Config {
	defer func() { _ = recover() }()
	return config.Get()
}

func normalizeCodexSessionKey(sessionID string) string {
	return strings.TrimSpace(sessionID)
}

// BuildCodexSessionResponseStoreKey scopes a WS client session to site/account/channel.
// Empty sessionID → empty key.
func BuildCodexSessionResponseStoreKey(sessionID string, siteID, accountID, channelID int64) string {
	sid := normalizeCodexSessionKey(sessionID)
	if sid == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	if siteID > 0 {
		parts = append(parts, fmt.Sprintf("site:%d", siteID))
	}
	if accountID > 0 {
		parts = append(parts, fmt.Sprintf("account:%d", accountID))
	}
	if channelID > 0 {
		parts = append(parts, fmt.Sprintf("channel:%d", channelID))
	}
	parts = append(parts, "session:"+sid)
	return strings.Join(parts, "|")
}

func getCodexSessionResponseID(sessionID string) string {
	key := normalizeCodexSessionKey(sessionID)
	if key == "" {
		return ""
	}
	now := time.Now().UnixMilli()
	ttl := codexSessionTTL().Milliseconds()
	codexSessionResponseMu.Lock()
	defer codexSessionResponseMu.Unlock()
	sweepExpiredSessionResponseIDsLocked(now, ttl)
	entry, ok := codexSessionResponseIDs[key]
	if !ok {
		return ""
	}
	if entry.updatedAtMs+ttl <= now {
		delete(codexSessionResponseIDs, key)
		return ""
	}
	entry.updatedAtMs = now
	codexSessionResponseIDs[key] = entry
	return entry.responseID
}

func setCodexSessionResponseID(sessionID, responseID string) {
	key := normalizeCodexSessionKey(sessionID)
	rid := strings.TrimSpace(responseID)
	if key == "" || rid == "" {
		return
	}
	now := time.Now().UnixMilli()
	ttl := codexSessionTTL().Milliseconds()
	codexSessionResponseMu.Lock()
	defer codexSessionResponseMu.Unlock()
	sweepExpiredSessionResponseIDsLocked(now, ttl)
	if len(codexSessionResponseIDs) >= codexSessionMaxKeys {
		// Drop oldest entry (linear scan; bounded by max keys).
		var oldestKey string
		var oldestMs int64
		for k, e := range codexSessionResponseIDs {
			if oldestKey == "" || e.updatedAtMs < oldestMs {
				oldestKey = k
				oldestMs = e.updatedAtMs
			}
		}
		if oldestKey != "" {
			delete(codexSessionResponseIDs, oldestKey)
		}
	}
	codexSessionResponseIDs[key] = sessionResponseEntry{responseID: rid, updatedAtMs: now}
}

func clearCodexSessionResponseID(sessionID string) {
	key := normalizeCodexSessionKey(sessionID)
	if key == "" {
		return
	}
	codexSessionResponseMu.Lock()
	delete(codexSessionResponseIDs, key)
	codexSessionResponseMu.Unlock()
}

func sweepExpiredSessionResponseIDsLocked(nowMs, ttlMs int64) {
	for k, e := range codexSessionResponseIDs {
		if e.updatedAtMs+ttlMs <= nowMs {
			delete(codexSessionResponseIDs, k)
		}
	}
}

// ResetCodexSessionResponseStoreForTest clears process-local state (tests only).
func ResetCodexSessionResponseStoreForTest() {
	codexSessionResponseMu.Lock()
	codexSessionResponseIDs = map[string]sessionResponseEntry{}
	codexSessionResponseMu.Unlock()
}

// ---- socket session store ----

type codexWSSocketSession struct {
	sessionID string
	conn      *websocket.Conn
	socketURL string
	mu        sync.Mutex // serialise turns on one session
}

var (
	codexWSSocketMu       sync.Mutex
	codexWSSocketSessions = map[string]*codexWSSocketSession{}
)

func getOrCreateCodexWSSocketSession(sessionID string) *codexWSSocketSession {
	key := normalizeCodexSessionKey(sessionID)
	codexWSSocketMu.Lock()
	defer codexWSSocketMu.Unlock()
	if s, ok := codexWSSocketSessions[key]; ok {
		return s
	}
	s := &codexWSSocketSession{sessionID: key}
	codexWSSocketSessions[key] = s
	return s
}

func takeCodexWSSocketSession(sessionID string) *codexWSSocketSession {
	key := normalizeCodexSessionKey(sessionID)
	codexWSSocketMu.Lock()
	defer codexWSSocketMu.Unlock()
	s := codexWSSocketSessions[key]
	delete(codexWSSocketSessions, key)
	return s
}

// ---- URL / headers / body helpers ----

// ToCodexWebsocketURL converts an https/http responses URL to wss/ws.
func ToCodexWebsocketURL(requestURL string) string {
	u := strings.TrimSpace(requestURL)
	if u == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(strings.ToLower(u), "https:"):
		return "wss:" + u[6:]
	case strings.HasPrefix(strings.ToLower(u), "http:"):
		return "ws:" + u[5:]
	default:
		return u
	}
}

// BuildCodexWebsocketHandshakeHeaders ensures OpenAI-Beta includes responses_websockets.
func BuildCodexWebsocketHandshakeHeaders(headers map[string]string) map[string]string {
	next := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		next[k] = v
	}
	beta := codexWSDefaultBeta
	if cfg := safeConfigGet(); cfg != nil && strings.TrimSpace(cfg.CodexResponsesWebsocketBeta) != "" {
		beta = strings.TrimSpace(cfg.CodexResponsesWebsocketBeta)
	}
	existing := ""
	for k, v := range next {
		if strings.EqualFold(strings.TrimSpace(k), "openai-beta") {
			existing = strings.TrimSpace(v)
			break
		}
	}
	if existing == "" {
		next["OpenAI-Beta"] = beta
		return next
	}
	if !strings.Contains(existing, "responses_websockets=") {
		// Replace the openAI-beta key with merged value (preserve original casing if present).
		for k := range next {
			if strings.EqualFold(strings.TrimSpace(k), "openai-beta") {
				next[k] = existing + "," + beta
				return next
			}
		}
		next["OpenAI-Beta"] = existing + "," + beta
	}
	return next
}

// BuildCodexWebsocketRequestBody wraps a responses body as type=response.create.
func BuildCodexWebsocketRequestBody(body map[string]any) map[string]any {
	next := make(map[string]any, len(body)+1)
	for k, v := range body {
		next[k] = v
	}
	next["type"] = "response.create"
	return next
}

// ---- continuation helpers ----

func hasResponsesToolOutput(input any) bool {
	arr, ok := input.([]any)
	if !ok {
		return false
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(asStringAny(m["type"])))
		if t != "function_call_output" && t != "custom_tool_call_output" {
			continue
		}
		id := strings.TrimSpace(asStringAny(m["call_id"]))
		if id == "" {
			id = strings.TrimSpace(asStringAny(m["id"]))
		}
		if id != "" {
			return true
		}
	}
	return false
}

func shouldInferResponsesPreviousResponseID(body map[string]any, candidate string) bool {
	if body == nil {
		return false
	}
	if strings.TrimSpace(asStringAny(body["previous_response_id"])) != "" {
		return false
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	return hasResponsesToolOutput(body["input"])
}

func withResponsesPreviousResponseID(body map[string]any, previousResponseID string) map[string]any {
	next := make(map[string]any, len(body)+1)
	for k, v := range body {
		next[k] = v
	}
	next["previous_response_id"] = strings.TrimSpace(previousResponseID)
	return next
}

func stripResponsesPreviousResponseID(body map[string]any) (map[string]any, bool) {
	if body == nil {
		return body, false
	}
	if _, ok := body["previous_response_id"]; !ok {
		return body, false
	}
	next := make(map[string]any, len(body))
	for k, v := range body {
		if k == "previous_response_id" {
			continue
		}
		next[k] = v
	}
	return next, true
}

func isResponsesPreviousResponseNotFoundError(rawErrText string, payload any) bool {
	fragments := collectResponsesErrorFragments(payload)
	if s := strings.TrimSpace(rawErrText); s != "" {
		fragments = append(fragments, s)
	}
	combined := strings.ToLower(strings.Join(fragments, " "))
	if combined == "" {
		return false
	}
	// TS parity: previous_response_id not found / unknown response id style errors.
	return (strings.Contains(combined, "previous_response_id") || strings.Contains(combined, "previous response")) &&
		(strings.Contains(combined, "not found") || strings.Contains(combined, "unknown") || strings.Contains(combined, "invalid"))
}

func collectResponsesErrorFragments(value any) []string {
	if value == nil {
		return nil
	}
	if s, ok := value.(string); ok {
		if t := strings.TrimSpace(s); t != "" {
			return []string{t}
		}
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	for _, key := range []string{"type", "code", "message", "reason"} {
		if t := strings.TrimSpace(asStringAny(m[key])); t != "" {
			out = append(out, t)
		}
	}
	if e, ok := m["error"]; ok {
		out = append(out, collectResponsesErrorFragments(e)...)
	}
	if r, ok := m["response"]; ok {
		out = append(out, collectResponsesErrorFragments(r)...)
	}
	if d, ok := m["incomplete_details"]; ok {
		out = append(out, collectResponsesErrorFragments(d)...)
	}
	return out
}

func asStringAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func isTerminalResponsesEvent(payload map[string]any) bool {
	t := strings.TrimSpace(asStringAny(payload["type"]))
	return t == "response.completed" || t == "response.failed" || t == "response.incomplete" || t == "error"
}

func isRuntimeErrorEvent(payload map[string]any) bool {
	return strings.TrimSpace(asStringAny(payload["type"])) == "error"
}

func extractResponsesTerminalResponseID(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	// Prefer nested response.id on terminal events.
	if resp, ok := m["response"].(map[string]any); ok {
		if id := strings.TrimSpace(asStringAny(resp["id"])); id != "" {
			return id
		}
	}
	if id := strings.TrimSpace(asStringAny(m["id"])); id != "" && strings.HasPrefix(id, "resp_") {
		return id
	}
	return ""
}

func extractTerminalErrorMessage(payload map[string]any) string {
	t := strings.TrimSpace(asStringAny(payload["type"]))
	if t == "error" {
		if e, ok := payload["error"].(map[string]any); ok {
			if msg := strings.TrimSpace(asStringAny(e["message"])); msg != "" {
				return msg
			}
		}
		return "upstream websocket error"
	}
	if t == "response.failed" || t == "response.incomplete" {
		if resp, ok := payload["response"].(map[string]any); ok {
			if e, ok := resp["error"].(map[string]any); ok {
				if msg := strings.TrimSpace(asStringAny(e["message"])); msg != "" {
					return msg
				}
			}
			if d, ok := resp["incomplete_details"].(map[string]any); ok {
				if reason := strings.TrimSpace(asStringAny(d["reason"])); reason != "" {
					return reason
				}
			}
		}
		return "upstream " + t
	}
	if t == "" {
		return "upstream websocket error"
	}
	return "upstream " + t
}

func extractFailureTerminalStatus(payload map[string]any) int {
	candidates := []any{payload["status"], payload["statusCode"], payload["code"]}
	if e, ok := payload["error"].(map[string]any); ok {
		candidates = append(candidates, e["status"], e["statusCode"], e["code"])
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if e, ok := resp["error"].(map[string]any); ok {
			candidates = append(candidates, e["status"], e["statusCode"], e["code"])
		}
	}
	for _, c := range candidates {
		switch n := c.(type) {
		case float64:
			if n >= 400 && n < 600 {
				return int(n)
			}
		case int:
			if n >= 400 && n < 600 {
				return n
			}
		case json.Number:
			if v, err := n.Int64(); err == nil && v >= 400 && v < 600 {
				return int(v)
			}
		}
	}
	return 502
}

// ---- capability probe ----

// SelectedChannelSupportsCodexWebsocketTransport reports whether an upstream
// Codex wss dial should be attempted (TS selectedChannelSupportsCodexWebsocketTransport).
// Platform must be codex; global flag CodexUpstreamWebsocketEnabled must be on;
// optional account extraConfig websockets false disables.
func SelectedChannelSupportsCodexWebsocketTransport(platform string, accountExtraConfig *string, requestModel string) bool {
	_ = requestModel // model match is applied by channel selection; probe stays platform-level.
	if strings.ToLower(strings.TrimSpace(platform)) != "codex" {
		return false
	}
	cfg := safeConfigGet()
	if cfg == nil || !cfg.CodexUpstreamWebsocketEnabled {
		return false
	}
	if accountExtraConfig == nil || strings.TrimSpace(*accountExtraConfig) == "" {
		return true
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(*accountExtraConfig), &extra); err != nil {
		return true
	}
	// Look for websockets flags in extraConfig / attributes / metadata / oauth.providerData.
	candidates := []any{
		extra["websockets"],
	}
	if attrs, ok := extra["attributes"].(map[string]any); ok {
		candidates = append(candidates, attrs["websockets"])
	}
	if meta, ok := extra["metadata"].(map[string]any); ok {
		candidates = append(candidates, meta["websockets"])
	}
	if oauth, ok := extra["oauth"].(map[string]any); ok {
		if pd, ok := oauth["providerData"].(map[string]any); ok {
			candidates = append(candidates, pd["websockets"])
			if attrs, ok := pd["attributes"].(map[string]any); ok {
				candidates = append(candidates, attrs["websockets"])
			}
			if meta, ok := pd["metadata"].(map[string]any); ok {
				candidates = append(candidates, meta["websockets"])
			}
		}
	}
	for _, c := range candidates {
		if b, ok := toBoolLike(c); ok {
			return b
		}
	}
	return true
}

func toBoolLike(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		switch s {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case float64:
		return t != 0, true
	case int:
		return t != 0, true
	}
	return false, false
}

// ---- runtime send ----

func buildContinuationAwareRuntimeBody(sessionID string, body map[string]any) map[string]any {
	remembered := getCodexSessionResponseID(sessionID)
	if !shouldInferResponsesPreviousResponseID(body, remembered) {
		return body
	}
	return withResponsesPreviousResponseID(body, remembered)
}

// SendCodexWebsocketRequest dials (or reuses) an upstream Codex wss session and
// waits for a terminal event. previous_response_id not-found recovers once by strip.
func SendCodexWebsocketRequest(ctx context.Context, input CodexWebsocketSendInput) (*CodexWebsocketRuntimeResult, error) {
	sessionID := normalizeCodexSessionKey(input.SessionID)
	if sessionID == "" {
		return nil, &CodexWebsocketRuntimeError{Message: "missing websocket session id", Status: 400}
	}
	session := getOrCreateCodexWSSocketSession(sessionID)
	session.mu.Lock()
	defer session.mu.Unlock()

	currentBody := buildContinuationAwareRuntimeBody(sessionID, input.Body)
	previousRecoveryTried := false

	for {
		result, err := sendCodexWSSessionAttempt(ctx, session, CodexWebsocketSendInput{
			SessionID:  sessionID,
			RequestURL: input.RequestURL,
			Headers:    input.Headers,
			Body:       currentBody,
		})
		if err == nil {
			return result, nil
		}
		runtimeErr, ok := err.(*CodexWebsocketRuntimeError)
		if !ok {
			return nil, err
		}
		if previousRecoveryTried {
			return nil, runtimeErr
		}
		payload := runtimeErr.Payload
		if payload == nil && len(runtimeErr.Events) > 0 {
			payload = runtimeErr.Events[len(runtimeErr.Events)-1]
		}
		if !isResponsesPreviousResponseNotFoundError(runtimeErr.Message, payload) {
			return nil, runtimeErr
		}
		stripped, removed := stripResponsesPreviousResponseID(currentBody)
		if !removed {
			return nil, runtimeErr
		}
		previousRecoveryTried = true
		clearCodexSessionResponseID(sessionID)
		currentBody = stripped
	}
}

func sendCodexWSSessionAttempt(ctx context.Context, session *codexWSSocketSession, input CodexWebsocketSendInput) (*CodexWebsocketRuntimeResult, error) {
	wsURL := ToCodexWebsocketURL(input.RequestURL)
	if wsURL == "" {
		return nil, &CodexWebsocketRuntimeError{Message: "missing upstream websocket url", Status: 502}
	}

	reused := false
	conn := session.conn
	if conn != nil && session.socketURL == wsURL {
		reused = true
	} else {
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "replace session")
			session.conn = nil
			session.socketURL = ""
		}
		headers := BuildCodexWebsocketHandshakeHeaders(input.Headers)
		httpHeader := http.Header{}
		for k, v := range headers {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				continue
			}
			httpHeader.Set(k, v)
		}
		dialCtx, cancel := context.WithTimeout(ctx, codexWSDialTimeout)
		next, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
			HTTPHeader: httpHeader,
		})
		cancel()
		if err != nil {
			return nil, &CodexWebsocketRuntimeError{
				Message: fmt.Sprintf("upstream websocket upgrade failed: %v", err),
				Status:  502,
			}
		}
		next.SetReadLimit(codexWSMaxMsgBytes)
		session.conn = next
		session.socketURL = wsURL
		conn = next
		reused = false
	}

	body := BuildCodexWebsocketRequestBody(input.Body)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &CodexWebsocketRuntimeError{Message: "failed to encode upstream websocket body", Status: 400}
	}

	writeCtx, writeCancel := context.WithTimeout(ctx, 30*time.Second)
	err = conn.Write(writeCtx, websocket.MessageText, payload)
	writeCancel()
	if err != nil {
		clearSessionSocket(session, conn)
		_ = conn.Close(websocket.StatusGoingAway, "write failed")
		return nil, &CodexWebsocketRuntimeError{
			Message: fmt.Sprintf("failed to send upstream websocket request: %v", err),
			Status:  502,
		}
	}

	events := make([]map[string]any, 0, 16)
	for {
		readCtx, readCancel := context.WithTimeout(ctx, codexWSReadTimeout)
		_, data, readErr := conn.Read(readCtx)
		readCancel()
		if readErr != nil {
			clearSessionSocket(session, conn)
			return nil, &CodexWebsocketRuntimeError{
				Message: "stream closed before response.completed",
				Status:  408,
				Events:  events,
			}
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			// Ignore malformed frames and wait for terminal.
			continue
		}
		events = append(events, parsed)
		if !isTerminalResponsesEvent(parsed) {
			continue
		}
		if isRuntimeErrorEvent(parsed) || isResponsesPreviousResponseNotFoundError(extractTerminalErrorMessage(parsed), parsed) {
			clearSessionSocket(session, conn)
			_ = conn.Close(websocket.StatusGoingAway, "upstream error")
			return nil, &CodexWebsocketRuntimeError{
				Message: extractTerminalErrorMessage(parsed),
				Status:  extractFailureTerminalStatus(parsed),
				Events:  events,
				Payload: parsed,
			}
		}
		if rid := extractResponsesTerminalResponseID(parsed); rid != "" {
			setCodexSessionResponseID(session.sessionID, rid)
		}
		return &CodexWebsocketRuntimeResult{Events: events, ReusedSession: reused}, nil
	}
}

func clearSessionSocket(session *codexWSSocketSession, conn *websocket.Conn) {
	if session == nil {
		return
	}
	if session.conn == conn {
		session.conn = nil
		session.socketURL = ""
	}
}

// CloseCodexWebsocketSession closes the upstream socket for a session key.
// Intentionally preserves remembered previous_response_id (TS parity).
func CloseCodexWebsocketSession(sessionID string) {
	s := takeCodexWSSocketSession(sessionID)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close(websocket.StatusNormalClosure, "session close")
		s.conn = nil
		s.socketURL = ""
	}
}

// LogCodexWebsocketDialFallback records honest dial→HTTP fallback.
func LogCodexWebsocketDialFallback(sessionID string, err error) {
	slog.Info("codex upstream wss dial failed; falling back to HTTP bridge",
		"session", normalizeCodexSessionKey(sessionID),
		"err", err,
	)
}
