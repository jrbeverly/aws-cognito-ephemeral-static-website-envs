// ==============================================================================
// types.go — Metadata domain model
//
// Defines the canonical types for platform users, sites, uploads,
// versions, and host mappings.  These types correspond to the DynamoDB
// single-table design documented in infrastructure/aws/modules/dynamodb/main.tf
// and the metadata requirements in VISION.md §8.3.
// ==============================================================================

package metadata

import "time"

// ==============================================================================
// User — A platform user identified by Cognito sub
//
// The Cognito-issued sub claim is the stable, immutable user identifier.
// The userSlug is derived from user input during registration and forms
// part of the hostname namespace (VISION.md §2.6, §6).
// ==============================================================================

type User struct {
	UserID     string    // Stable platform identifier (Cognito sub claim)
	UserSlug   string    // URL-safe namespace segment
	CognitoSub string    // Canonical Cognito subject; matches UserID
	CreatedAt  time.Time // When the user record was created
	UpdatedAt  time.Time // When the user record was last modified
	Disabled   bool      // Soft-delete flag: when true, login is rejected
}

// ==============================================================================
// Site — A published static site owned by a user
//
// Each site belongs to exactly one user.  The active version pointer
// identifies which published version is currently served.
// ==============================================================================

type Site struct {
	SiteID            string    // Platform-generated unique site identifier
	UserID            string    // Owner (maps to User.UserID)
	SiteSlug          string    // URL-safe namespace segment, unique per user
	ActiveVersionID   string    // Currently served version; empty before first publish
	S3PublishedPrefix string    // S3 prefix for the active published version
	CreatedAt         time.Time // When the site record was created
	UpdatedAt         time.Time // When the site record was last modified
	Disabled          bool      // Soft-delete flag: when true, serving stops
}

// ==============================================================================
// Upload — An upload record tracking a staging-to-publish lifecycle
//
// Uploads are created by the backend when it issues a presigned upload URL.
// The worker updates status and validation results as processing completes.
// ==============================================================================

type Upload struct {
	UploadID             string            // Unique upload identifier
	UserID               string            // Owner (maps to User.UserID)
	SiteID               string            // Target site (maps to Site.SiteID)
	Status               UploadStatus      // Current lifecycle stage
	ValidationResult     *ValidationResult // Set after validation completes; nil while pending
	S3StagingKey         string            // S3 key under staging/ prefix
	CompressedSizeBytes  int64             // Size on S3 (compressed zip, or raw HTML)
	UncompressedSizeBytes int64            // Size after extraction (validated content)
	FileCount            int               // Count of files in the uploaded archive
	CreatedAt            time.Time         // When the upload record was created
	UpdatedAt            time.Time         // When the upload record was last modified
}

// UploadStatus enumerates the stages of an upload's lifecycle.
type UploadStatus string

const (
	UploadStatusPending    UploadStatus = "pending"    // Grant issued, awaiting client upload
	UploadStatusUploaded   UploadStatus = "uploaded"   // Client upload complete, awaiting validation
	UploadStatusValidating UploadStatus = "validating" // Worker is actively validating
	UploadStatusPublished  UploadStatus = "published"  // Published successfully
	UploadStatusFailed     UploadStatus = "failed"     // Validation or publish error
)

// ==============================================================================
// ValidationResult — Outcome of content safety and structure checks
//
// Stored inline on the upload record after validation completes.
// VISION.md §5.4 lists the required failure messages.
// ==============================================================================

type ValidationResult struct {
	Valid  bool     `json:"valid"`  // True when all checks pass
	Errors []string `json:"errors"` // Human-readable failure reasons
}

// ==============================================================================
// HostMapping — Resolves a request hostname to the full serving chain
//
// This is a denormalised item supporting the single-GetItem lookup
// host → owner → site → active version → S3 prefix (VISION.md §6).
// ==============================================================================

type HostMapping struct {
	Hostname          string // Request Host header value (e.g. "foobar.myname.sites.example.com")
	UserID            string // Resolved owner
	UserSlug          string // Resolved owner slug
	SiteID            string // Resolved site
	SiteSlug          string // Resolved site slug
	VersionID         string // Currently active published version
	S3PublishedPrefix string // S3 prefix for the active published version
	UserDisabled      bool   // Denormalized from User.Disabled — enables single-GetItem gateway resolution
	SiteDisabled      bool   // Denormalized from Site.Disabled — enables single-GetItem gateway resolution
}

// ==============================================================================
// Version — Immutable published version of a site
//
// Versions are identified by a version ID and map to a specific S3 prefix.
// They are not stored as separate DynamoDB items — the version ID is stored
// on the Site and HostMapping items, and the S3 prefix encodes the version.
// ==============================================================================

type Version struct {
	VersionID         string    // Unique version identifier (e.g. UUID or timestamp)
	SiteID            string    // Owning site
	S3PublishedPrefix string    // S3 prefix for this version's content
	CreatedAt         time.Time // When the version was published
}
