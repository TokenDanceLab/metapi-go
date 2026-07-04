package proxyhandler

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestHandleSearch_Success(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{"query":"latest AI news","max_results":5}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Fatal("expected non-nil response")
	}
	if m["model"] != defaultSearchModel {
		t.Errorf("model = %v, want %s", m["model"], defaultSearchModel)
	}
}


func TestHandleSearch_DefaultMaxResults(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{"query":"test"}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSearch_QueryRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSearch_QueryEmpty(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{"query":"   "}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for empty query, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSearch_StreamNotSupported(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{"query":"test","stream":true}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSearch_MaxResultsValidation(t *testing.T) {
	tests := []struct {
		name       string
		maxResults any
		wantStatus int
	}{
		{"valid int 1", 1, 200},
		{"valid int 20", 20, 200},
		{"valid float 10", 10.0, 200},
		{"too large int", 21, 400},
		{"too small int", 0, 400},
		{"negative float", -1.0, 400},
		{"string", "10", 400},
		{"bool", true, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"query":"test","max_results":%v}`, toJSON(tt.maxResults))
			req := makeProxyReq("POST", "/v1/search", body)
			rec := httptest.NewRecorder()
			HandleSearch(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("max_results=%v: expected %d, got %d: %s", tt.maxResults, tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleSearch_DefaultModel(t *testing.T) {
	req := makeProxyReq("POST", "/v1/search", `{"query":"test"}`)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	m := unmarshalResponse(t, rec)
	if m["model"] != defaultSearchModel {
		t.Errorf("default model = %v, want %s", m["model"], defaultSearchModel)
	}
}

func TestHandleSearch_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/search", nil)
	rec := httptest.NewRecorder()
	HandleSearch(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func toJSON(v any) string {
	switch val := v.(type) {
	case int:
		return itoa(int64(val))
	case float64:
		if float64(int64(val)) == val {
			return itoa(int64(val))
		}
		return fmt.Sprintf("%v", val)
	case string:
		return `"` + val + `"`
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
