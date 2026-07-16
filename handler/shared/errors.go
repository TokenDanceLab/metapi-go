// Package shared provides common HTTP handler utilities used across
// admin and proxy handler packages.
package shared

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIError represents a structured JSON error response.
// Message is safe for public consumption; Internal is logged but never sent.
// JSON field names are camelCase-compatible public keys used by the admin UI:
//   - error  (string message)
//   - detail (optional classifier / subtype)
type APIError struct {
	Code     int    `json:"-"`
	Message  string `json:"error"`
	Detail   string `json:"detail,omitempty"`
	Internal error  `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Internal != nil {
		return e.Message + ": " + e.Internal.Error()
	}
	return e.Message
}

// WriteError writes a structured JSON error response to the client.
// It sets Content-Type: application/json and the given HTTP status code.
// Callers must use a non-2xx status for failures — never HTTP 200 with an error body.
func WriteError(w http.ResponseWriter, code int, message string) {
	WriteAPIError(w, &APIError{Code: code, Message: message})
}

// WriteErrorDetail writes a structured JSON error response with
// additional detail field (e.g., "invalid_request" error type).
func WriteErrorDetail(w http.ResponseWriter, code int, message, detail string) {
	WriteAPIError(w, &APIError{Code: code, Message: message, Detail: detail})
}

// WriteAPIError writes the public fields of APIError with the given status.
// Status defaults to Code when Code > 0; otherwise 500.
func WriteAPIError(w http.ResponseWriter, err *APIError) {
	if err == nil {
		err = &APIError{Code: http.StatusInternalServerError, Message: "internal error"}
	}
	code := err.Code
	if code < 400 {
		// Guard against accidental silent success statuses for error bodies.
		if code == 0 {
			code = http.StatusInternalServerError
		} else {
			code = http.StatusBadRequest
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if encErr := json.NewEncoder(w).Encode(APIError{
		Message: err.Message,
		Detail:  err.Detail,
	}); encErr != nil {
		slog.Warn("shared: failed to write error response", "error", encErr)
	}
}
