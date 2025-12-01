// ==============================================================================
// slug.go — Site slug validation
//
// Site slugs follow the same rules as user slugs (DNS label safety):
//   - Lowercase alphanumeric characters and hyphens only
//   - No leading or trailing hyphens
//   - Maximum 39 characters (RFC 1035 label length)
//   - Non-empty
//
// The slug forms part of the hostname namespace:
//   {siteSlug}.{userSlug}.{sitesDomain}
//   {siteSlug}--{userSlug}.{sitesDomain}
// ==============================================================================

package handler

import (
	"errors"
	"strings"
)

// Sentinels returned by ValidateSiteSlug.
var (
	ErrSlugEmpty        = errors.New("site slug must not be empty")
	ErrSlugTooLong      = errors.New("site slug must not exceed 39 characters")
	ErrSlugInvalidChars = errors.New("site slug must contain only lowercase letters, digits, and hyphens")
	ErrSlugLeadingDash  = errors.New("site slug must not start with a hyphen")
	ErrSlugTrailingDash = errors.New("site slug must not end with a hyphen")
)

const maxSiteSlugLength = 39

// ValidateSiteSlug checks that the given slug is a valid DNS-safe site
// namespace segment.  Returns nil when the slug is valid, or a non-nil
// error describing the violation.
func ValidateSiteSlug(slug string) error {
	if slug == "" {
		return ErrSlugEmpty
	}
	if len(slug) > maxSiteSlugLength {
		return ErrSlugTooLong
	}
	if slug[0] == '-' {
		return ErrSlugLeadingDash
	}
	if slug[len(slug)-1] == '-' {
		return ErrSlugTrailingDash
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return ErrSlugInvalidChars
	}
	return nil
}

// sanitizeSlug trims leading/trailing whitespace and lowercases the input.
// Used as a pre-processing step before validation so the API can accept
// reasonable user input (e.g., " My-Blog " → "my-blog").
func sanitizeSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
