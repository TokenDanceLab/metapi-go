package responses

import (
	"testing"
)

func TestShouldStripCompactResponsesStore(t *testing.T) {
	tests := []struct {
		name         string
		sitePlatform string
		want         bool
	}{
		{"codex lower", "codex", true},
		{"codex upper", "CODEX", true},
		{"codex mixed case", "Codex", true},
		{"codex with spaces", "  codex  ", true},
		{"sub2api lower", "sub2api", true},
		{"sub2api upper", "SUB2API", true},
		{"sub2api with spaces", "  sub2api  ", true},
		{"unknown platform", "openai", false},
		{"empty string", "", false},
		{"arbitrary platform", "azure", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldStripCompactResponsesStore(tt.sitePlatform)
			if got != tt.want {
				t.Errorf("ShouldStripCompactResponsesStore(%q) = %v, want %v", tt.sitePlatform, got, tt.want)
			}
		})
	}
}

func TestShouldForceResponsesUpstreamStream(t *testing.T) {
	tests := []struct {
		name             string
		sitePlatform     string
		isCompactRequest bool
		want             bool
	}{
		{"codex non-compact", "codex", false, true},
		{"codex compact", "codex", true, false},
		{"sub2api non-compact", "sub2api", false, true},
		{"sub2api compact", "sub2api", true, false},
		{"openai non-compact", "openai", false, false},
		{"openai compact", "openai", true, false},
		{"empty non-compact", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldForceResponsesUpstreamStream(tt.sitePlatform, tt.isCompactRequest)
			if got != tt.want {
				t.Errorf("ShouldForceResponsesUpstreamStream(%q, %v) = %v, want %v",
					tt.sitePlatform, tt.isCompactRequest, got, tt.want)
			}
		})
	}
}

func TestSanitizeCompactResponsesRequestBody(t *testing.T) {
	tests := []struct {
		name         string
		body         map[string]any
		sitePlatform string
		want         map[string]any
	}{
		{
			name:         "codex strips stream, stream_options, store, previous_response_id",
			body:         map[string]any{"model": "gpt-4", "stream": true, "stream_options": map[string]any{}, "store": true, "previous_response_id": "resp_1", "input": "hello"},
			sitePlatform: "codex",
			want:         map[string]any{"model": "gpt-4", "input": "hello"},
		},
		{
			name:         "sub2api strips stream, stream_options, store, previous_response_id",
			body:         map[string]any{"model": "gpt-4", "stream": true, "stream_options": map[string]any{}, "store": true, "previous_response_id": "resp_1"},
			sitePlatform: "sub2api",
			want:         map[string]any{"model": "gpt-4"},
		},
		{
			name:         "openai strips stream, stream_options, previous_response_id; keeps store",
			body:         map[string]any{"model": "gpt-4", "stream": true, "stream_options": map[string]any{}, "store": true, "previous_response_id": "resp_1"},
			sitePlatform: "openai",
			want:         map[string]any{"model": "gpt-4", "store": true},
		},
		{
			name:         "no stream fields present",
			body:         map[string]any{"model": "gpt-4", "input": "hello"},
			sitePlatform: "codex",
			want:         map[string]any{"model": "gpt-4", "input": "hello"},
		},
		{
			name:         "nil body yields empty map",
			body:         nil,
			sitePlatform: "openai",
			want:         map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeCompactResponsesRequestBody(tt.body, tt.sitePlatform)
			if len(got) != len(tt.want) {
				t.Errorf("SanitizeCompactResponsesRequestBody() len = %d, want %d; got=%v want=%v",
					len(got), len(tt.want), got, tt.want)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("SanitizeCompactResponsesRequestBody()[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestEnsureCompactResponsesJSONAcceptHeader(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		sitePlatform string
		wantAccept   string
		wantLen      int
		acceptKey    string
	}{
		{
			name:         "codex forces accept header",
			headers:      map[string]string{"Accept": "text/html", "X-Custom": "val"},
			sitePlatform: "codex",
			wantAccept:   "application/json",
			wantLen:      2,
		},
		{
			name:         "sub2api forces accept header",
			headers:      map[string]string{"accept": "text/plain"},
			sitePlatform: "sub2api",
			wantAccept:   "application/json",
			wantLen:      1,
		},
		{
			name:         "openai preserves headers unchanged (original case)",
			headers:      map[string]string{"Accept": "text/html", "X-Custom": "val"},
			sitePlatform: "openai",
			wantAccept:   "text/html",
			wantLen:      2,
			acceptKey:    "Accept",
		},
		{
			name:         "empty platform preserves headers",
			headers:      map[string]string{"Accept": "text/html"},
			sitePlatform: "",
			wantAccept:   "text/html",
			wantLen:      1,
			acceptKey:    "Accept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureCompactResponsesJSONAcceptHeader(tt.headers, tt.sitePlatform)
			if len(got) != tt.wantLen {
				t.Errorf("EnsureCompactResponsesJSONAcceptHeader() len = %d, want %d; got=%v",
					len(got), tt.wantLen, got)
				return
			}
			key := tt.acceptKey
			if key == "" {
				key = "accept"
			}
			if got[key] != tt.wantAccept {
				t.Errorf("EnsureCompactResponsesJSONAcceptHeader()[%q] = %q, want %q",
					key, got[key], tt.wantAccept)
			}
		})
	}
}

func TestShouldFallbackCompactResponsesToResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		rawErrText  string
		requestPath string
		want        bool
	}{
		{"404 with compact path", 404, "not found", "/v1/responses/compact", true},
		{"405 not allowed", 405, "method not allowed", "/v1/responses/compact", true},
		{"501 not implemented", 501, "not implemented", "/v1/responses/compact", true},
		{"200 no fallback", 200, "ok", "/v1/responses/compact", false},
		{"400 with unknown stream param and compact hint", 400, "unknown parameter: 'stream'", "/v1/responses/compact", true},
		{"400 with unknown stream param in error text", 400, "error: unknown parameter: 'stream' in /responses/compact", "/v1/chat/completions", true},
		{"400 invalid url with compact hint", 400, "invalid url for responses/compact", "/v1/chat/completions", true},
		{"400 not supported with compact hint", 400, "compact endpoint not supported", "/v1/chat/completions", true},
		{"400 unsupported with compact prefix", 400, "compact unsupported", "/v1/chat/completions", true},
		{"400 compact suffix", 400, "endpoint is compact", "/v1/chat/completions", false},
		{"400 compact surrounded by spaces", 400, "the  compact  endpoint", "/v1/chat/completions", false},
		{"400 no compact hint", 400, "bad request", "/v1/chat/completions", false},
		{"404 on non-compact path no fallback", 404, "not found", "/v1/chat/completions", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldFallbackCompactResponsesToResponses(tt.status, tt.rawErrText, tt.requestPath)
			if got != tt.want {
				t.Errorf("ShouldFallbackCompactResponsesToResponses(%d, %q, %q) = %v, want %v",
					tt.status, tt.rawErrText, tt.requestPath, got, tt.want)
			}
		})
	}
}

func TestInbound(t *testing.T) {
	tests := []struct {
		name    string
		body    any
		want    map[string]any
		wantErr bool
	}{
		{
			name: "valid map",
			body: map[string]any{"model": "gpt-4", "input": "hello"},
			want: map[string]any{"model": "gpt-4", "input": "hello"},
		},
		{
			name:    "string body returns empty map",
			body:    "not a map",
			want:    map[string]any{},
			wantErr: false,
		},
		{
			name:    "nil body returns empty map",
			body:    nil,
			want:    map[string]any{},
			wantErr: false,
		},
		{
			name:    "int body returns empty map",
			body:    42,
			want:    map[string]any{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Inbound(tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("Inbound() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("Inbound() len = %d, want %d; got=%v want=%v",
					len(got), len(tt.want), got, tt.want)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("Inbound()[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}
