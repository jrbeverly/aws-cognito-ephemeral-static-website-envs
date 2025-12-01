// ==============================================================================
// router.go — HTTP request router
//
// The router uses Go 1.22's enhanced http.ServeMux with method-based routing
// (GET /path, POST /path).  All write endpoints require authentication via
// the Authenticate middleware.
//
// The same http.Handler works both as a standalone HTTP server for local
// development and behind a Lambda function via an API Gateway V2 adapter
// in cmd/api/main.go.
// ==============================================================================

package api

import (
	"log/slog"
	"net/http"
	"time"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/handler"
	"backend/go-api/internal/metrics"
)

// ==============================================================================
// NewRouter — Build the handler tree
//
// All endpoints that operate on user data are wrapped with Authenticate.
// The /health endpoint is unauthenticated — it is used by ALB target group
// health checks.
// ==============================================================================

// NewRouter builds the HTTP handler tree with all routes registered.
// sitesHandler provides the site lifecycle CRUD endpoints backed by the
// metadata repository.  uploadsHandler provides presigned-URL grant
// issuance and upload status tracking.
// emitter is used to record HTTP-level metrics (request count, latency, errors).
func NewRouter(sitesHandler *handler.SitesHandler, uploadsHandler *handler.UploadsHandler, emitter *metrics.Emitter) http.Handler {
	mux := http.NewServeMux()

	// ---------------------------------------------------------------------------
	// Health — unauthenticated, used by ALB target group health checks
	// ---------------------------------------------------------------------------
	mux.HandleFunc("GET /health", handler.Health)

	// ---------------------------------------------------------------------------
	// Sites — all endpoints require authentication
	// ---------------------------------------------------------------------------
	mux.HandleFunc("GET /api/sites", wrapAuth(sitesHandler.ListSites))
	mux.HandleFunc("POST /api/sites", wrapAuth(sitesHandler.CreateSite))
	mux.HandleFunc("GET /api/sites/{id}", wrapAuth(sitesHandler.GetSite))
	mux.HandleFunc("DELETE /api/sites/{id}", wrapAuth(sitesHandler.DeleteSite))

	// ---------------------------------------------------------------------------
	// Uploads — all endpoints require authentication
	// ---------------------------------------------------------------------------
	mux.HandleFunc("POST /api/uploads", wrapAuth(uploadsHandler.CreateUpload))
	mux.HandleFunc("GET /api/uploads/{id}", wrapAuth(uploadsHandler.GetUploadStatus))
	mux.HandleFunc("POST /api/uploads/{id}/complete", wrapAuth(uploadsHandler.CompleteUpload))
	mux.HandleFunc("POST /api/uploads/paste", wrapAuth(uploadsHandler.CreatePasteUpload))

	// Wrap the mux with request logging and metrics
	return requestLogger(mux, emitter)
}

// wrapAuth applies the Authenticate middleware to a handler function.
// This is a convenience helper to keep route registration concise.
func wrapAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Authenticate(http.HandlerFunc(fn)).ServeHTTP(w, r)
	}
}

// ==============================================================================
// Request logging middleware — logs each request and emits HTTP metrics
// ==============================================================================

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.statusCode = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.statusCode = http.StatusOK
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

func requestLogger(next http.Handler, emitter *metrics.Emitter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		id := auth.RequestIdentity(r)
		userID := ""
		if id != nil {
			userID = id.UserID
		}

		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		latency := time.Since(start)
		operation := r.Method + " " + r.URL.Path

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"user_id", userID,
			"remote_addr", r.RemoteAddr,
			"status", rec.statusCode,
			"latency_ms", latency.Milliseconds(),
		)

		// Emit HTTP metrics via EMF for CloudWatch.
		emitter.EmitHTTPMetrics(operation, rec.statusCode, latency)
	})
}
