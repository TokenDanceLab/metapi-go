package proxyhandler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tokendancelab/metapi-go/auth"
)

// TestResponsesWebsocket_PrewarmTurn verifies C1 upgrade + generate=false prewarm
// frames without inventing completions for a real upstream turn.
func TestResponsesWebsocket_PrewarmTurn(t *testing.T) {
	// Enable proxy stub so any accidental bridge call would still return 200,
	// but prewarm must short-circuit before bridgeHTTP.
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "1")

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			HandleResponsesGet426(w, r)
			return
		}
		http.Error(w, "unexpected method", 405)
	})

	// Wrap with synthetic ProxyAuth for upgrade path.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pac := &auth.ProxyAuthContext{
			Token:   "test-global-token",
			Source:  "global",
			KeyName: "global",
		}
		r = r.WithContext(auth.WithProxyAuth(r.Context(), pac))
		mux.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/responses"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer test-global-token"},
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	create := map[string]any{
		"type":     "response.create",
		"model":    "gpt-4o",
		"generate": false,
		"input":    []any{},
	}
	raw, _ := json.Marshal(create)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write create: %v", err)
	}

	// Expect response.created then response.completed.
	types := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("json: %v body=%s", err, string(data))
		}
		typ, _ := frame["type"].(string)
		types = append(types, typ)
	}
	if types[0] != "response.created" || types[1] != "response.completed" {
		t.Fatalf("frame types = %v, want created+completed", types)
	}
}

// TestResponsesWebsocket_DialWithoutAuthRejected ensures the HTTP upgrade path
// refuses unauthenticated clients before a socket is established.
func TestResponsesWebsocket_DialWithoutAuthRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", HandleResponsesGet426)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/responses"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial failure without auth")
	}
	if resp == nil {
		// Some failures may not surface HTTP response; accept err-only.
		return
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		// Accept may surface other non-101 statuses depending on stack.
		if resp.StatusCode == http.StatusSwitchingProtocols {
			t.Fatalf("must not switch protocols without auth, status=%d", resp.StatusCode)
		}
	}
}
