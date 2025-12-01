// ==============================================================================
// s3store.go — S3 content storage abstraction for backend-initiated writes
//
// The ContentStore interface allows the backend to write content directly to
// S3 staging.  This is used by the pasted-HTML flow (VISION.md §11.3) where
// the frontend sends HTML content to the backend rather than uploading via a
// presigned URL.
//
// All other upload paths use presigned URLs — the backend signs, the client
// uploads.  This interface exists only for flows where the backend receives
// the content directly.
// ==============================================================================

package handler

import "context"

// ==============================================================================
// ContentStore — Write objects directly to S3 staging
//
// Abstracts the S3 PutObject operation so handler tests can inject a mock
// without importing the AWS SDK.  The adapter in cmd/api/main.go delegates
// to the real s3.Client.
// ==============================================================================

// ContentStore writes content directly to an S3 key.  Used by the
// pasted-HTML endpoint to store the submitted HTML as a staged index.html.
//
// The caller (handler) derives the key from authenticated identity and a
// server-generated upload ID — the client never provides the key.
type ContentStore interface {
	// PutContent writes data to the given S3 key with the specified content type.
	// Returns an error if the write fails.
	PutContent(ctx context.Context, req ContentStoreRequest) error

	// GetContent reads an object (used to publish staged uploads).
	GetContent(ctx context.Context, bucket, key string) ([]byte, error)
}

// ==============================================================================
// ContentStoreRequest — Input parameters for direct S3 object writes
// ==============================================================================

// ContentStoreRequest carries the parameters needed to write an object to S3.
// Every field is populated server-side; no field comes from the HTTP client.
type ContentStoreRequest struct {
	Bucket      string // S3 bucket name
	Key         string // S3 object key (staging prefix derived by the handler)
	Body        []byte // Raw content bytes
	ContentType string // MIME type (e.g. "text/html")
}
