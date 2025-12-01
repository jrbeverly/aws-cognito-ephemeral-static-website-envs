// ==============================================================================
// presign.go — S3 presigned-URL abstraction for upload grants
//
// Defines a minimal interface for presigned PUT URL generation.  The handler
// package depends on this interface, NOT on the AWS S3 SDK.  This keeps
// handler code testable without a real S3 client and avoids coupling the
// business logic to a specific AWS SDK type.
//
// The real S3 presign client is wired in cmd/api/main.go via an adapter that
// implements this interface using the AWS SDK v2 s3 package.
// ==============================================================================

package handler

import (
	"context"
	"time"
)

// ==============================================================================
// PresignClient — Generate presigned S3 PUT URLs
//
// Abstracts the S3 presign operation so handler tests can inject a mock
// without importing the AWS SDK.  The adapter in cmd/api/main.go delegates
// to the real s3.PresignClient.
// ==============================================================================

// PresignClient generates time-limited presigned URLs for uploading objects
// to a specific S3 key.  The URL is scoped to a single key — the client
// cannot influence the destination path.
type PresignClient interface {
	// PresignPutObject returns a presigned URL that grants temporary PUT
	// access to a single S3 object at the given key.  The caller specifies
	// the bucket, key, content type, and how long the URL is valid.
	//
	// The returned URL is scoped exclusively to the given key.  The caller
	// (handler) derives the key from authenticated identity and a
	// server-generated upload ID — the client never provides it.
	PresignPutObject(ctx context.Context, req PresignRequest) (*PresignResult, error)
}

// ==============================================================================
// PresignRequest — Input parameters for presigned URL generation
// ==============================================================================

// PresignRequest carries the parameters needed to generate a presigned PUT URL.
// Every field is populated server-side; no field comes from the HTTP client.
type PresignRequest struct {
	Bucket      string        // S3 bucket name
	Key         string        // S3 object key (staging prefix derived by the handler)
	ContentType string        // MIME type for the upload (e.g. "application/zip")
	Expires     time.Duration // How long the presigned URL is valid
}

// ==============================================================================
// PresignResult — Output from presigned URL generation
// ==============================================================================

// PresignResult holds the generated presigned URL and its expiration.
type PresignResult struct {
	URL       string    // Presigned PUT URL
	ExpiresAt time.Time // When the URL expires (absolute wall-clock time)
}
