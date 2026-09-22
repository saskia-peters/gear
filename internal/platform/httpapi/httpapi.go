// Package httpapi provides the uniform JSON error envelope and response
// helpers every handler must use across all modules (see Consistency
// Conventions in the architecture spine):
//
//	{"error":{"code":"...","message":"...","details":{...}}}
//
// The HTTP status always matches the error code semantics (403, 429, 503,
// ...). No handler may return a body that violates this shape.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorBody is the inner error object of the uniform envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// ErrorEnvelope is the uniform error envelope returned by every handler on
// failure.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// WriteJSON writes v as JSON with the given status code. If encoding fails
// after the status was already committed, the failure is only logged — the
// response cannot be repaired at that point (http.Error would corrupt it).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Warn("httpapi: encoding JSON response failed",
			"error", err, "status", status)
	}
}

// WriteError writes the uniform error envelope with the matching HTTP status.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteErrorDetail(w, status, code, message, nil)
}

// WriteErrorDetail writes the uniform error envelope, optionally carrying
// machine-readable details alongside code and message.
func WriteErrorDetail(w http.ResponseWriter, status int, code, message string, details any) {
	WriteJSON(w, status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message, Details: details}})
}

// NotFoundHandler is the router-level 404 responder. It returns the uniform
// JSON envelope (chi's default NotFound emits plain text, violating the JSON
// error contract).
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "route not found")
	}
}

// MethodNotAllowedHandler is the router-level 405 responder, returning the
// uniform JSON envelope (chi's default MethodNotAllowed emits plain text).
func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
