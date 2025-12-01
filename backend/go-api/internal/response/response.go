// ==============================================================================
// response.go — Structured JSON responses and error handling
//
// All API responses use a consistent JSON envelope.  Errors include a
// machine-readable code and a human-readable message.
//
// The backend never trusts client-supplied owner IDs, S3 keys, or publishing
// destinations — all such values are derived from authenticated identity or
// server-side state (VISION.md §8.2, §13.2).
//
// This package is separate from the api package to avoid import cycles:
// both api (router/middleware) and handler (endpoint stubs) depend on these
// helpers, but handler must not import api.
// ==============================================================================

package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ==============================================================================
// Response envelope
// ==============================================================================

// Envelope wraps successful responses in a consistent JSON envelope.
type Envelope struct {
	Data any `json:"data,omitempty"`
}

// ErrorEnvelope wraps errors in a consistent JSON envelope.
type ErrorEnvelope struct {
	Error Error `json:"error"`
}

// Error represents a structured API error.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ==============================================================================
// Error codes
// ==============================================================================

const (
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeForbidden        = "FORBIDDEN"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeValidationFailed = "VALIDATION_FAILED"
	CodeInternalError    = "INTERNAL_ERROR"
	CodeBadRequest       = "BAD_REQUEST"
)

// ==============================================================================
// Response helpers
// ==============================================================================

// JSON writes a JSON response with the given status code and payload.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(Envelope{Data: data}); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// JSONError writes a structured JSON error response.
func JSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := ErrorEnvelope{
		Error: Error{
			Code:    code,
			Message: message,
		},
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}

// ==============================================================================
// Standard error responses
// ==============================================================================

// Unauthorized responds with 401 UNAUTHORIZED when no identity is present.
func Unauthorized(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Authentication required"
	}
	JSONError(w, http.StatusUnauthorized, CodeUnauthorized, message)
}

// Forbidden responds with 403 FORBIDDEN when the identity is known but lacks
// permission for the requested operation.
func Forbidden(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Access denied"
	}
	JSONError(w, http.StatusForbidden, CodeForbidden, message)
}

// NotFound responds with 404 NOT_FOUND.
func NotFound(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Resource not found"
	}
	JSONError(w, http.StatusNotFound, CodeNotFound, message)
}

// Conflict responds with 409 CONFLICT.
func Conflict(w http.ResponseWriter, message string) {
	JSONError(w, http.StatusConflict, CodeConflict, message)
}

// BadRequest responds with 400 BAD_REQUEST.
func BadRequest(w http.ResponseWriter, message string) {
	JSONError(w, http.StatusBadRequest, CodeBadRequest, message)
}

// InternalError logs the error and responds with 500 INTERNAL_ERROR.
// The message sent to the client is generic to avoid leaking internals.
func InternalError(w http.ResponseWriter, err error) {
	slog.Error("internal error", "error", err)
	JSONError(w, http.StatusInternalServerError, CodeInternalError, "An internal error occurred")
}
