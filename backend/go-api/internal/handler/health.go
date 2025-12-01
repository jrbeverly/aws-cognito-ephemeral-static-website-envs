// ==============================================================================
// health.go — Health check endpoint
//
// GET /health returns 200 OK.  This is the ALB target group health check
// endpoint.  It is intentionally unauthenticated.
// ==============================================================================

package handler

import (
	"net/http"

	"backend/go-api/internal/response"
)

// Health responds with a simple OK status.
// Used by ALB target group health checks — does not require authentication.
func Health(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
