// Package shared provides common HTTP handler utilities used across
// admin and proxy handler packages.
package shared

import (
	"encoding/json"
	"net/http"
)

// APIError represents a structured JSON error response.
// Message is safe for public consumption; Internal is logged but never sent.
type APIError struct {
	Code     int    `json:"-"`
	Message  string `json:"error"`
	Detail   string `json:"detail,omitempty"`
	Internal error  `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Internal != nil {
		return e.Message + ": " + e.Internal.Error()
	}
	return e.Message
}

// WriteError writes a structured JSON error response to the client.
// It sets Content-Type: application/json and the given HTTP status code.
func WriteError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(APIError{Code: code, Message: message})
}

// WriteErrorDetail writes a structured JSON error response with
// additional detail field (e.g., "invalid_request" error type).
func WriteErrorDetail(w http.ResponseWriter, code int, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(APIError{Code: code, Message: message, Detail: detail})
}
