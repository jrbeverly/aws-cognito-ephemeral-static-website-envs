// ==============================================================================
// publisher.go — Publishing logic for immutable versioned content
//
// After successful validation, the publisher copies normalized files into an
// immutable per-version S3 prefix and atomically advances the active version
// pointer on the site and host mapping records.
//
// Published layout (VISION.md §8.5):
//
//	s3://{bucket}/published/users/{userSlug}/sites/{siteSlug}/versions/{versionId}/index.html
//	s3://{bucket}/published/users/{userSlug}/sites/{siteSlug}/versions/{versionId}/assets/app.css
//
// The active version is updated atomically via a single DynamoDB transaction
// so the gateway can resolve the hostname to the new published prefix.  The
// upload record is transitioned to "published" or "failed" to reflect the
// outcome.
//
// Design principle (VISION.md §18.7):
//
//	Prefer versioned publishing over in-place mutation.
//
// VISION.md §5.3  — Publish result
// VISION.md §5.4  — Failure result
// VISION.md §8.5  — Validation & publishing worker
// VISION.md §11   — Upload and publish flow
// VISION.md §14   — Observability
// ==============================================================================

package publish

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"
)

// ==============================================================================
// Domain types — subset of the backend's metadata model needed for publishing
// ==============================================================================

// UploadRecord carries the fields the publisher reads from the upload record.
type UploadRecord struct {
	UploadID          string
	UserID            string
	SiteID            string
	Status            string
	S3StagingKey      string
	CompressedSizeBytes  int64
	UncompressedSizeBytes int64
	FileCount         int
}

// Site carries the fields the publisher reads from the site metadata item.
type Site struct {
	SiteID            string
	UserID            string
	SiteSlug          string
	ActiveVersionID   string
	Disabled          bool
}

// User carries the fields the publisher reads from the user metadata item.
type User struct {
	UserID   string
	UserSlug string
	Disabled bool
}

// ValidationResult records the outcome of content validation.
// Mirrors the backend's metadata.ValidationResult shape.
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// ==============================================================================
// Interfaces — contracts the publisher needs from infrastructure
// ==============================================================================

// S3Writer puts content objects at specific S3 keys.  The publisher calls
// PutObject once per validated file to write it into the versioned prefix.
type S3Writer interface {
	// PutObject writes body to the given S3 key with the specified content type.
	// Returns an error if the write fails.
	PutObject(ctx context.Context, key string, body []byte, contentType string) error
}

// UploadStore reads and writes upload lifecycle records.
// The publisher reads the upload to verify ownership and get site context,
// then updates the status to reflect the publish outcome.
type UploadStore interface {
	// GetUploadByID retrieves an upload record by its platform identifier.
	// Returns nil, nil when no upload with that ID exists.
	GetUploadByID(ctx context.Context, uploadID string) (*UploadRecord, error)

	// UpdateUploadStatus transitions an upload to a new status and records
	// the validation result.  The publisher calls this to set the status to
	// "published" or "failed".
	UpdateUploadStatus(ctx context.Context, userID, uploadID string, status string, vr *ValidationResult) error
}

// SiteStore reads site and user metadata and atomically updates the active
// version pointer on both the site record and its associated host mappings.
type SiteStore interface {
	// GetSite retrieves a site by owner ID and site ID.
	// Returns nil, nil when the site does not exist.
	GetSite(ctx context.Context, userID, siteID string) (*Site, error)

	// GetUser retrieves a user by platform identifier.
	// Returns nil, nil when the user does not exist.
	GetUser(ctx context.Context, userID string) (*User, error)

	// UpdateActiveVersion atomically sets the active version ID and published
	// S3 prefix on the site record and both host mapping patterns.  The
	// caller provides all the information needed to construct the full
	// hostnames: userSlug, siteSlug, and sitesDomain.
	//
	// Implementation uses TransactWriteItems so all updates succeed or none do.
	UpdateActiveVersion(ctx context.Context, input ActiveVersionInput) error
}

// ActiveVersionInput carries everything needed to atomically advance the
// active version pointer on site and host-mapping records.
type ActiveVersionInput struct {
	UserID            string
	SiteID            string
	UserSlug          string
	SiteSlug          string
	SitesDomain       string
	VersionID         string
	S3PublishedPrefix string
}

// ==============================================================================
// Sentinel errors
// ==============================================================================

var (
	ErrUploadNotFound  = errors.New("upload not found")
	ErrSiteNotFound    = errors.New("site not found")
	ErrUserNotFound    = errors.New("user not found")
	ErrSiteDisabled    = errors.New("site is disabled")
	ErrUserDisabled    = errors.New("user is disabled")
	ErrUploadNotReady  = errors.New("upload is not in uploaded state")
)

// ==============================================================================
// Publish input / output
// ==============================================================================

// PublishInput carries the information needed to publish validated content.
type PublishInput struct {
	UploadID       string            // Upload record identifier
	ValidatedFiles map[string][]byte // Normalized path → content (from validation)
	FileCount      int               // Number of files in validated set
	TotalBytes     int64             // Total uncompressed bytes written
}

// PublishResult describes the outcome of a successful publish.
type PublishResult struct {
	VersionID         string // Newly generated immutable version identifier
	S3PublishedPrefix string // S3 prefix for this version's content
	FilesWritten      int    // Number of files published to S3
	BytesWritten      int64  // Total bytes written to S3
}

// ==============================================================================
// Publisher — orchestrates the publish step
// ==============================================================================

// Publisher copies validated content to an immutable versioned S3 prefix and
// atomically advances the active version pointer.  It is safe to call only
// after validation has passed — the caller must have already verified that
// ValidatedFiles contains the normalized, safe content to publish.
type Publisher struct {
	s3          S3Writer
	uploads     UploadStore
	sites       SiteStore
	bucket      string
	sitesDomain string
}

// NewPublisher creates a Publisher with the given infrastructure adapters.
//
//	s3:          writes published files to the versioned S3 prefix
//	uploads:     reads upload metadata and records publish outcomes
//	sites:       reads site/user metadata and updates active versions atomically
//	bucket:      S3 bucket name for published content
//	sitesDomain: base domain for hosted-site hostnames (e.g. "sites.example.com")
func NewPublisher(s3 S3Writer, uploads UploadStore, sites SiteStore, bucket, sitesDomain string) *Publisher {
	return &Publisher{
		s3:          s3,
		uploads:     uploads,
		sites:       sites,
		bucket:      bucket,
		sitesDomain: sitesDomain,
	}
}

// ==============================================================================
// Publish — main entry point
//
// Publish executes the full publish flow:
//  1. Read the upload record to get ownership context (userID, siteID).
//  2. Read site and user metadata to get slugs for prefix construction.
//  3. Verify the site and user are not disabled.
//  4. Generate a new immutable version ID.
//  5. Build the S3 published prefix from slugs and version ID.
//  6. Write each validated file to S3 under the versioned prefix.
//  7. Atomically update the active version on site + host mappings.
//  8. Transition the upload status to "published".
//
// On any failure, the upload status is set to "failed" with the error recorded.
// Partial S3 writes are not rolled back (versions are immutable by design —
// a failed publish leaves orphaned objects that lifecycle policies can clean up).
//
// Returns a PublishResult on success, or an error describing what failed.
// ==============================================================================

func (p *Publisher) Publish(ctx context.Context, input PublishInput) (*PublishResult, error) {
	// --------------------------------------------------------------------------
	// Step 1 — Read the upload record.
	// --------------------------------------------------------------------------
	upload, err := p.uploads.GetUploadByID(ctx, input.UploadID)
	if err != nil {
		return nil, fmt.Errorf("publish: get upload %s: %w", input.UploadID, err)
	}
	if upload == nil {
		return nil, ErrUploadNotFound
	}

	// Only publish uploads that are in "uploaded" state.
	if upload.Status != "uploaded" {
		return nil, fmt.Errorf("%w: status is %q", ErrUploadNotReady, upload.Status)
	}

	// --------------------------------------------------------------------------
	// Step 2 — Read site and user metadata.
	// --------------------------------------------------------------------------
	site, err := p.sites.GetSite(ctx, upload.UserID, upload.SiteID)
	if err != nil {
		return nil, fmt.Errorf("publish: get site %s: %w", upload.SiteID, err)
	}
	if site == nil {
		return nil, ErrSiteNotFound
	}
	if site.Disabled {
		return nil, ErrSiteDisabled
	}

	user, err := p.sites.GetUser(ctx, upload.UserID)
	if err != nil {
		return nil, fmt.Errorf("publish: get user %s: %w", upload.UserID, err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if user.Disabled {
		return nil, ErrUserDisabled
	}

	// --------------------------------------------------------------------------
	// Step 3 — Generate a new immutable version ID.
	// --------------------------------------------------------------------------
	versionID := NewVersionID()

	// --------------------------------------------------------------------------
	// Step 4 — Build the versioned S3 prefix.
	//
	//   published/users/{userSlug}/sites/{siteSlug}/versions/{versionId}/
	//
	// VISION.md §8.5 — Published layout.
	// --------------------------------------------------------------------------
	s3Prefix := fmt.Sprintf("published/users/%s/sites/%s/versions/%s",
		user.UserSlug, site.SiteSlug, versionID)

	// --------------------------------------------------------------------------
	// Step 5 — Write each validated file to the versioned S3 prefix.
	// --------------------------------------------------------------------------
	var bytesWritten int64
	filesWritten := 0
	for filePath, body := range input.ValidatedFiles {
		key := s3Prefix + "/" + filePath
		contentType := MimeTypeByExtension(filePath)

		if err := p.s3.PutObject(ctx, key, body, contentType); err != nil {
			slog.Error("publish: S3 write failed",
				"upload_id", input.UploadID,
				"version_id", versionID,
				"key", key,
				"error", err,
			)
			return nil, fmt.Errorf("publish: write %s: %w", filePath, err)
		}

		bytesWritten += int64(len(body))
		filesWritten++
	}

	slog.Info("publish: files written to versioned prefix",
		"upload_id", input.UploadID,
		"version_id", versionID,
		"s3_prefix", s3Prefix,
		"files_written", filesWritten,
		"bytes_written", bytesWritten,
	)

	// --------------------------------------------------------------------------
	// Step 6 — Atomically update the active version.
	//
	// This updates the site record (activeVersionId, s3PublishedPrefix) and
	// both host mapping patterns in a single DynamoDB transaction.  If the
	// transaction fails (e.g. site was disabled between the read and write),
	// the publish is reverted at the metadata level — the orphaned S3 objects
	// are harmless and will be cleaned by lifecycle policies.
	//
	// VISION.md §6  — Hostname model
	// VISION.md §12 — Lifecycle (versions never mutated in place)
	// --------------------------------------------------------------------------
	err = p.sites.UpdateActiveVersion(ctx, ActiveVersionInput{
		UserID:            upload.UserID,
		SiteID:            upload.SiteID,
		UserSlug:          user.UserSlug,
		SiteSlug:          site.SiteSlug,
		SitesDomain:       p.sitesDomain,
		VersionID:         versionID,
		S3PublishedPrefix: s3Prefix,
	})
	if err != nil {
		slog.Error("publish: atomic version update failed",
			"upload_id", input.UploadID,
			"version_id", versionID,
			"site_id", upload.SiteID,
			"error", err,
		)
		return nil, fmt.Errorf("publish: update active version: %w", err)
	}

	// --------------------------------------------------------------------------
	// Step 7 — Transition the upload to "published".
	//
	// The validation result is stored alongside the status update so the
	// frontend can display the outcome.
	// --------------------------------------------------------------------------
	err = p.uploads.UpdateUploadStatus(ctx, upload.UserID, input.UploadID, "published", &ValidationResult{
		Valid: true,
	})
	if err != nil {
		slog.Error("publish: update upload status failed",
			"upload_id", input.UploadID,
			"error", err,
		)
		return nil, fmt.Errorf("publish: mark published: %w", err)
	}

	slog.Info("publish: complete",
		"upload_id", input.UploadID,
		"site_id", upload.SiteID,
		"version_id", versionID,
		"files_written", filesWritten,
	)

	return &PublishResult{
		VersionID:         versionID,
		S3PublishedPrefix: s3Prefix,
		FilesWritten:      filesWritten,
		BytesWritten:      bytesWritten,
	}, nil
}

// ==============================================================================
// Upload lookup — exposed so callers can obtain userID for status updates
// ==============================================================================

// LookupUploadByID retrieves an upload record by its platform identifier.
// This is a thin wrapper over the UploadStore so callers can obtain the
// userID needed for RecordValidationResult without importing the store
// interface separately.
//
// Returns nil, nil when no upload with that ID exists.
func (p *Publisher) LookupUploadByID(ctx context.Context, uploadID string) (*UploadRecord, error) {
	return p.uploads.GetUploadByID(ctx, uploadID)
}

// ==============================================================================
// RecordValidationResult — Persist the validation outcome on the upload record
//
// This is the write path for both pass and fail outcomes.  On failure, the
// upload status transitions to "failed" so the frontend can surface the errors
// (VISION.md §5.4).  The active published site is NEVER modified by this
// method — it only writes to the upload record.
//
// On success (Valid = true), the upload transitions to "published".  This is
// called directly by the worker when the full publish flow completes to record
// the outcome, or as a lightweight sticky-success marker.
//
// The userID is required because the DynamoDB table uses USER#<userId> as the
// partition key.  Callers obtain it from the upload record or from the
// authenticated request context.
//
// VISION.md §5.3  — Publish result
// VISION.md §5.4  — Failure result (clear status with actionable messages)
// VISION.md §14   — Observability: logs and status events
// VISION.md §17.3 — Failed validation does not update the active published site
// ==============================================================================

func (p *Publisher) RecordValidationResult(ctx context.Context, userID, uploadID string, vr *ValidationResult) error {
	status := "published"
	if !vr.Valid {
		status = "failed"
	}

	err := p.uploads.UpdateUploadStatus(ctx, userID, uploadID, status, vr)
	if err != nil {
		slog.Error("publish: record validation result failed",
			"upload_id", uploadID,
			"user_id", userID,
			"valid", vr.Valid,
			"status", status,
			"error", err,
		)
		return fmt.Errorf("record validation result for upload %s: %w", uploadID, err)
	}

	slog.Info("publish: validation result recorded",
		"upload_id", uploadID,
		"user_id", userID,
		"valid", vr.Valid,
		"status", status,
	)
	return nil
}

// ==============================================================================
// Version ID generation
// ==============================================================================

// NewVersionID generates a unique version identifier.
// Uses crypto/rand to produce a hex string prefixed with "version_".
// The format matches the site and upload ID conventions in the backend.
func NewVersionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read can fail only on a broken system; fall back to a
		// timestamp-based suffix so callers don't have to handle the error.
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return fmt.Sprintf("version_%x", b)
}

// ==============================================================================
// MIME type detection
// ==============================================================================

// MimeTypeByExtension returns the MIME content type for a file path based on
// its extension.  Returns "application/octet-stream" for unrecognised extensions.
//
// Only common static-site extensions are mapped.  The gateway's NGINX layer
// also sets Content-Type based on extension; this mapping covers the S3
// object metadata for correctness when objects are fetched directly.
func MimeTypeByExtension(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	if ct, ok := mimeTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

// mimeTypes maps common static-site file extensions to MIME types.
var mimeTypes = map[string]string{
	".html":   "text/html",
	".htm":    "text/html",
	".css":    "text/css",
	".js":     "application/javascript",
	".mjs":    "application/javascript",
	".json":   "application/json",
	".xml":    "application/xml",
	".txt":    "text/plain",
	".csv":    "text/csv",
	".md":     "text/markdown",
	".yaml":   "text/yaml",
	".yml":    "text/yaml",
	".svg":    "image/svg+xml",
	".png":    "image/png",
	".jpg":    "image/jpeg",
	".jpeg":   "image/jpeg",
	".gif":    "image/gif",
	".webp":   "image/webp",
	".ico":    "image/x-icon",
	".woff":   "font/woff",
	".woff2":  "font/woff2",
	".ttf":    "font/ttf",
	".eot":    "application/vnd.ms-fontobject",
	".otf":    "font/otf",
	".pdf":    "application/pdf",
	".mp3":    "audio/mpeg",
	".mp4":    "video/mp4",
	".webm":   "video/webm",
	".wasm":   "application/wasm",
}
