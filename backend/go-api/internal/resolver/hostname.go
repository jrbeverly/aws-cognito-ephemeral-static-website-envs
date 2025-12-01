// ==============================================================================
// hostname.go — Hostname parsing for the gateway
//
// Extracts site and user slugs from request hostnames following the
// patterns defined in VISION.md §6:
//
//	Preferred:  {siteSlug}.{userSlug}.{sitesDomain}
//	Alias:      {siteSlug}--{userSlug}.{sitesDomain}
//
// These functions are pure — no I/O, no dependencies beyond the standard
// library — so they can be tested exhaustively without a mock.
// ==============================================================================

package resolver

import (
	"fmt"
	"strings"
)

// ==============================================================================
// ParseHost — extract siteSlug and userSlug from a request hostname
//
// sitesDomain is the sites subdomain root (e.g. "sites.example.com").
// aliasEnabled controls whether the temporary -- slug separator is accepted.
//
// Returns ErrInvalidHost when the hostname does not match either pattern
// or when an extracted slug is empty.
// ==============================================================================

// ErrInvalidHost is returned when a hostname does not match any known pattern
// or contains empty slug components.
var ErrInvalidHost = fmt.Errorf("invalid hostname for site resolution")

// HostPattern describes which hostname pattern was matched.
type HostPattern int

const (
	PatternPreferred HostPattern = iota // {siteSlug}.{userSlug}.{sitesDomain}
	PatternAlias                        // {siteSlug}--{userSlug}.{sitesDomain}
)

// ParsedHost holds the result of parsing a request hostname.
type ParsedHost struct {
	SiteSlug string
	UserSlug string
	Pattern  HostPattern
}

// ParseHost extracts siteSlug and userSlug from a request hostname.
//
// The hostname may include an optional port suffix (e.g. ":443") which
// is stripped before parsing.
//
// sitesDomain is the sites subdomain root (e.g. "sites.example.com").
// aliasEnabled controls whether the temporary -- slug separator is accepted.
//
// Returns a non-nil error wrapping ErrInvalidHost when the hostname does
// not match either supported pattern or when an extracted slug is empty.
func ParseHost(hostname, sitesDomain string, aliasEnabled bool) (*ParsedHost, error) {
	// Strip optional port suffix.
	if colon := strings.LastIndex(hostname, ":"); colon != -1 {
		hostname = hostname[:colon]
	}

	// The sitesDomain suffix must be present with a leading dot.
	suffix := "." + sitesDomain
	if !strings.HasSuffix(hostname, suffix) {
		return nil, fmt.Errorf("%w: hostname %q does not end with %q", ErrInvalidHost, hostname, suffix)
	}

	// Extract the slug portion — everything before the sites domain.
	slugPart := hostname[:len(hostname)-len(suffix)]
	if slugPart == "" {
		return nil, fmt.Errorf("%w: missing slug portion in %q", ErrInvalidHost, hostname)
	}

	var siteSlug, userSlug string

	// Try alias pattern first when enabled.
	if aliasEnabled && strings.Contains(slugPart, "--") {
		parts := strings.SplitN(slugPart, "--", 2)
		if len(parts) == 2 {
			siteSlug, userSlug = parts[0], parts[1]
			if err := validateSlugs(siteSlug, userSlug, hostname); err != nil {
				return nil, err
			}
			return &ParsedHost{
				SiteSlug: siteSlug,
				UserSlug: userSlug,
				Pattern:  PatternAlias,
			}, nil
		}
	}

	// Preferred (dot-separated) pattern.
	// Split on the first dot to get siteSlug; the remainder is userSlug.
	if dot := strings.Index(slugPart, "."); dot != -1 {
		siteSlug = slugPart[:dot]
		userSlug = slugPart[dot+1:]
	} else {
		return nil, fmt.Errorf("%w: hostname %q does not match expected slug pattern", ErrInvalidHost, hostname)
	}

	if err := validateSlugs(siteSlug, userSlug, hostname); err != nil {
		return nil, err
	}

	return &ParsedHost{
		SiteSlug: siteSlug,
		UserSlug: userSlug,
		Pattern:  PatternPreferred,
	}, nil
}

// ==============================================================================
// helpers
// ==============================================================================

func validateSlugs(siteSlug, userSlug, hostname string) error {
	if siteSlug == "" {
		return fmt.Errorf("%w: empty site slug in %q", ErrInvalidHost, hostname)
	}
	if userSlug == "" {
		return fmt.Errorf("%w: empty user slug in %q", ErrInvalidHost, hostname)
	}
	return nil
}
