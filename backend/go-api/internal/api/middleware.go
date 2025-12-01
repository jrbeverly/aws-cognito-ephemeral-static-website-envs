// ==============================================================================
// middleware.go — Authentication and authorization middleware
//
// Every protected endpoint shares this middleware.  It extracts the Cognito
// identity from request headers (ALB authenticate-cognito) and stores it in
// the request context.  Unauthenticated requests receive 401.
//
// Authorization is enforced independently of the frontend — the backend never
// trusts client-supplied owner IDs or S3 keys (VISION.md §8.8, §13.2).
// ==============================================================================

package api

import (
	"net/http"
	"strings"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/response"
)

// ==============================================================================
// Authentication middleware
// ==============================================================================

// Authenticate is middleware that extracts the Cognito identity from request
// headers and stores it in the request context.  If no valid identity is found,
// it responds with 401 and does not call the next handler.
//
// Identity sources (tried in order):
//  1. ALB authenticate-cognito headers (x-amzn-oidc-data)
//  2. Dev headers (x-dev-user-id) for local development
func Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http canonicalizes keys (X-Dev-User-Id); auth looks them up lowercase.
		headers := make(map[string]string, len(r.Header))
		for k := range r.Header {
			headers[strings.ToLower(k)] = r.Header.Get(k)
		}

		identity, err := auth.ParseHeaders(headers)
		if err != nil {
			response.Unauthorized(w, "Authentication required")
			return
		}

		ctx := auth.SetIdentity(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ==============================================================================
// Owner-only authorization
// ==============================================================================

// RequireOwnership checks that the authenticated user matches the resource owner.
// This is the primary authorization guard: a user may only operate on their own
// resources (VISION.md §13.2).
//
// The ownerID parameter is derived from server-side state (e.g., from a
// DynamoDB GetItem), NEVER from the client request body or query string.
func RequireOwnership(r *http.Request, ownerID string) bool {
	id := auth.RequestIdentity(r)
	if id == nil {
		return false
	}
	return id.UserID == ownerID
}
