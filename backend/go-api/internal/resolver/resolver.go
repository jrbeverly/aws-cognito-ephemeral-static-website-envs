// ==============================================================================
// resolver.go — Host-to-site resolution
//
// Consumes the SiteMetadataRepository to resolve a request hostname to
// the full serving chain: host → owner → site → active version → S3 prefix
// (VISION.md §6, §8.6).
//
// The resolver is the single integration point between the gateway's
// hostname parsing and the metadata store.  The gateway will use this
// to decide which S3 prefix to proxy a request to, or to reject the
// request when the host is unknown, the site is disabled, or no active
// version has been published.
// ==============================================================================

package resolver

import (
	"context"
	"errors"
	"fmt"

	"backend/go-api/internal/metadata"
)

// ==============================================================================
// Sentinel errors — callers may check with errors.Is
// ==============================================================================

// ErrHostNotFound is returned when no host mapping exists for the requested
// hostname.  The gateway should map this to a 404 response (VISION.md §12,
// §8.6 — "return clear 404 responses for missing sites").
var ErrHostNotFound = errors.New("host not found")

// ErrSiteDisabled is returned when the resolved site has been soft-deleted
// (disabled flag is true).  Deleted sites must stop serving (VISION.md §12).
var ErrSiteDisabled = errors.New("site is disabled")

// ErrUserDisabled is returned when the resolved user has been soft-deleted
// (disabled flag is true).  Disabled users must not have any sites served.
var ErrUserDisabled = errors.New("user is disabled")

// ErrNoActiveVersion is returned when the resolved site has no published
// version (activeVersionId is empty).  The gateway should map this to a
// 404 response.
var ErrNoActiveVersion = errors.New("no active version published")

// ==============================================================================
// Resolution — the result of a successful hostname lookup
// ==============================================================================

// Resolution contains the full serving chain resolved from a hostname.
// Every field is guaranteed populated on a successful resolution.
type Resolution struct {
	Hostname          string // The original request Host header value
	UserID            string // Resolved owner identifier
	UserSlug          string // Resolved owner slug
	SiteID            string // Resolved site identifier
	SiteSlug          string // Resolved site slug
	VersionID         string // Currently active published version
	S3PublishedPrefix string // S3 prefix for the active published version
}

// ==============================================================================
// Resolve — the primary entry point
//
// Resolve looks up a hostname through the metadata repository and verifies
// that the user and site are not disabled and that an active version exists.
//
// On success it returns a *Resolution that the gateway can use to construct
// the S3 proxy path.  On failure it returns one of the sentinel errors
// above, which the caller can test with errors.Is.
//
// The repository's GetHostMapping is the single GetItem lookup described
// in VISION.md §6.  The additional GetUser and GetSite calls verify the
// soft-delete state that the host mapping does not itself encode.
// ==============================================================================

func Resolve(ctx context.Context, repo metadata.SiteMetadataRepository, hostname string) (*Resolution, error) {
	// Step 1 — Resolve the host mapping.
	mapping, err := repo.GetHostMapping(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve: get host mapping: %w", err)
	}
	if mapping == nil {
		return nil, ErrHostNotFound
	}

	// Step 2 — Verify the user is not disabled.
	user, err := repo.GetUser(ctx, mapping.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve: get user %s: %w", mapping.UserID, err)
	}
	if user == nil {
		return nil, fmt.Errorf("resolve: user %s referenced by host mapping %q not found", mapping.UserID, hostname)
	}
	if user.Disabled {
		return nil, ErrUserDisabled
	}

	// Step 3 — Verify the site is not disabled.
	site, err := repo.GetSite(ctx, mapping.UserID, mapping.SiteID)
	if err != nil {
		return nil, fmt.Errorf("resolve: get site %s: %w", mapping.SiteID, err)
	}
	if site == nil {
		return nil, fmt.Errorf("resolve: site %s referenced by host mapping %q not found", mapping.SiteID, hostname)
	}
	if site.Disabled {
		return nil, ErrSiteDisabled
	}

	// Step 4 — Verify an active version is published.
	if mapping.VersionID == "" || mapping.S3PublishedPrefix == "" {
		return nil, ErrNoActiveVersion
	}

	return &Resolution{
		Hostname:          hostname,
		UserID:            mapping.UserID,
		UserSlug:          mapping.UserSlug,
		SiteID:            mapping.SiteID,
		SiteSlug:          mapping.SiteSlug,
		VersionID:         mapping.VersionID,
		S3PublishedPrefix: mapping.S3PublishedPrefix,
	}, nil
}
