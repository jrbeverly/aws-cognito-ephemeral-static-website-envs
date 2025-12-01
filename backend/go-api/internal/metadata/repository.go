// ==============================================================================
// repository.go — Persistence interfaces for the metadata store
//
// These interfaces define the contract between application logic and the
// DynamoDB persistence layer.  Both the backend API and the validation
// worker depend on these interfaces, so they must remain provider-agnostic.
// ==============================================================================

package metadata

import "context"

// ==============================================================================
// SiteMetadataRepository — Users, sites, host mappings, and versions
//
// Backed by the site_metadata DynamoDB table (single-table design).
// Covers the three entity types:
//   - User        (PK=USER#<id>,  SK=PROFILE)
//   - Site        (PK=USER#<id>,  SK=SITE#<id>)
//   - HostMapping (PK=HOST#<host>, SK=MAPPING)
// ==============================================================================

type SiteMetadataRepository interface {
	// --------------------------------------------------------------------------
	// User operations
	// --------------------------------------------------------------------------

	// CreateUser persists a new platform user.  Returns an error if a user
	// with the same UserID already exists.
	CreateUser(ctx context.Context, user User) error

	// GetUser retrieves a user by their platform identifier (Cognito sub).
	// Returns nil, nil when the user does not exist.
	GetUser(ctx context.Context, userID string) (*User, error)

	// UpdateUser modifies mutable fields (slug, disabled) on an existing user.
	// Returns an error if the user does not exist.
	UpdateUser(ctx context.Context, user User) error

	// DeleteUser soft-deletes a user by setting the disabled flag.
	// Returns an error if the user does not exist.
	DeleteUser(ctx context.Context, userID string) error

	// --------------------------------------------------------------------------
	// Site operations
	// --------------------------------------------------------------------------

	// CreateSite persists a new site owned by the given user.
	// Returns an error if a site with the same ID already exists.
	CreateSite(ctx context.Context, site Site) error

	// GetSite retrieves a single site by owner and site ID.
	// Returns nil, nil when the site does not exist.
	GetSite(ctx context.Context, userID, siteID string) (*Site, error)

	// ListSites returns all sites belonging to a user.
	// Returns an empty slice when the user has no sites.
	ListSites(ctx context.Context, userID string) ([]Site, error)

	// UpdateSite modifies mutable fields on an existing site.
	// Returns an error if the site does not exist or belongs to a different user.
	UpdateSite(ctx context.Context, site Site) error

	// DeleteSite soft-deletes a site by setting the disabled flag.
	// Returns an error if the site does not exist.
	DeleteSite(ctx context.Context, userID, siteID string) error

	// --------------------------------------------------------------------------
	// Active version management
	// --------------------------------------------------------------------------

	// UpdateActiveVersion atomically updates the active version pointer on
	// a site and the corresponding host mappings' version and S3 prefix.
	//
	// This must be atomic: the site activeVersionId, site s3PublishedPrefix,
	// and host-mapping pointers must all update together or none of them do.
	// Uses DynamoDB TransactWriteItems to guarantee atomicity.
	//
	// The sitesDomain is required to construct the full hostnames from slugs
	// (e.g. "sites.example.com").  Both the preferred and alias hostname
	// patterns are updated:
	//   {siteSlug}.{userSlug}.{sitesDomain}
	//   {siteSlug}--{userSlug}.{sitesDomain}
	UpdateActiveVersion(ctx context.Context, userID, siteID, versionID, s3PublishedPrefix, sitesDomain string) error

	// --------------------------------------------------------------------------
	// Host mapping operations
	// --------------------------------------------------------------------------

	// CreateHostMapping persists a new hostname→site mapping.
	// Returns an error if a mapping for the same hostname already exists.
	CreateHostMapping(ctx context.Context, mapping HostMapping) error

	// GetHostMapping resolves a hostname to the full serving chain.
	// Returns nil, nil when no mapping exists for the hostname.
	// This is the single-GetItem lookup described in VISION.md §6.
	GetHostMapping(ctx context.Context, hostname string) (*HostMapping, error)

	// UpdateHostMapping updates mutable fields on an existing host mapping.
	// Primarily used to sync denormalized userDisabled and siteDisabled flags
	// when the corresponding User or Site item is soft-deleted or restored.
	// Returns an error if the mapping does not exist.
	UpdateHostMapping(ctx context.Context, hostname string, mapping HostMapping) error

	// DeleteHostMapping removes a hostname mapping.
	// Returns an error if the mapping does not exist.
	DeleteHostMapping(ctx context.Context, hostname string) error
}

// ==============================================================================
// UploadRecordsRepository — Upload lifecycle tracking
//
// Backed by the upload_records DynamoDB table.
// Supports two access patterns:
//   - List a user's uploads:       Query pk=USER#<id>,  sk begins_with UPLOAD#
//   - Look up upload by upload ID:  Query GSI upload-by-id, pk=<uploadId>
//   - Get a specific upload:        GetItem pk=USER#<id>, sk=UPLOAD#<uploadId>
// ==============================================================================

type UploadRecordsRepository interface {
	// CreateUpload persists a new upload record.
	// Returns an error if an upload with the same ID already exists.
	CreateUpload(ctx context.Context, upload Upload) error

	// GetUpload retrieves an upload by owner and upload ID.
	// Returns nil, nil when the upload does not exist.
	GetUpload(ctx context.Context, userID, uploadID string) (*Upload, error)

	// GetUploadByID looks up an upload by its ID alone (uses the upload-by-id GSI).
	// Returns nil, nil when the upload does not exist.
	GetUploadByID(ctx context.Context, uploadID string) (*Upload, error)

	// ListUploads returns all uploads belonging to a user, ordered by
	// creation time (most recent first where the implementation allows).
	// Returns an empty slice when the user has no uploads.
	ListUploads(ctx context.Context, userID string) ([]Upload, error)

	// UpdateUploadStatus transitions an upload to a new status and optionally
	// records the validation result.  This is the primary mechanism by which
	// the validation worker reports outcomes.
	UpdateUploadStatus(ctx context.Context, userID, uploadID string, status UploadStatus, result *ValidationResult) error
}
