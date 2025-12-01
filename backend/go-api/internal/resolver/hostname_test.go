// ==============================================================================
// hostname_test.go — Unit tests for hostname parsing
//
// Covers both patterns (preferred and alias), edge cases, and error
// conditions as required by VISION.md §16 and the acceptance criteria:
//   - Correct host resolves to correct slugs
//   - Both hostname patterns parse correctly
//   - Invalid hostnames return ErrInvalidHost
// ==============================================================================

package resolver_test

import (
	"errors"
	"testing"

	"backend/go-api/internal/resolver"
)

const testSitesDomain = "sites.example.com"

// ==============================================================================
// Preferred pattern — {siteSlug}.{userSlug}.sites...
// ==============================================================================

func TestParseHostPreferred(t *testing.T) {
	tests := []struct {
		name           string
		hostname       string
		aliasEnabled   bool
		wantSiteSlug   string
		wantUserSlug   string
		wantPattern    resolver.HostPattern
		wantErr        error
	}{
		{
			name:         "basic preferred pattern",
			hostname:     "foobar.myname.sites.example.com",
			aliasEnabled: false,
			wantSiteSlug: "foobar",
			wantUserSlug: "myname",
			wantPattern:  resolver.PatternPreferred,
		},
		{
			name:         "preferred with alias enabled still parses as preferred",
			hostname:     "myblog.janedoe.sites.example.com",
			aliasEnabled: true,
			wantSiteSlug: "myblog",
			wantUserSlug: "janedoe",
			wantPattern:  resolver.PatternPreferred,
		},
		{
			name:         "single char slugs",
			hostname:     "a.b.sites.example.com",
			aliasEnabled: false,
			wantSiteSlug: "a",
			wantUserSlug: "b",
			wantPattern:  resolver.PatternPreferred,
		},
		{
			name:         "slugs with hyphens",
			hostname:     "my-site.my-user.sites.example.com",
			aliasEnabled: false,
			wantSiteSlug: "my-site",
			wantUserSlug: "my-user",
			wantPattern:  resolver.PatternPreferred,
		},
		{
			name:         "slugs with digits",
			hostname:     "site123.user456.sites.example.com",
			aliasEnabled: false,
			wantSiteSlug: "site123",
			wantUserSlug: "user456",
			wantPattern:  resolver.PatternPreferred,
		},
		{
			name:         "hostname with port suffix",
			hostname:     "foobar.myname.sites.example.com:443",
			aliasEnabled: false,
			wantSiteSlug: "foobar",
			wantUserSlug: "myname",
			wantPattern:  resolver.PatternPreferred,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ParseHost(tt.hostname, testSitesDomain, tt.aliasEnabled)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.SiteSlug != tt.wantSiteSlug {
				t.Errorf("siteSlug = %q, want %q", got.SiteSlug, tt.wantSiteSlug)
			}
			if got.UserSlug != tt.wantUserSlug {
				t.Errorf("userSlug = %q, want %q", got.UserSlug, tt.wantUserSlug)
			}
			if got.Pattern != tt.wantPattern {
				t.Errorf("pattern = %d, want %d", got.Pattern, tt.wantPattern)
			}
		})
	}
}

// ==============================================================================
// Alias pattern — {siteSlug}--{userSlug}.sites...
// ==============================================================================

func TestParseHostAlias(t *testing.T) {
	tests := []struct {
		name         string
		hostname     string
		wantSiteSlug string
		wantUserSlug string
	}{
		{
			name:         "basic alias pattern",
			hostname:     "foobar--myname.sites.example.com",
			wantSiteSlug: "foobar",
			wantUserSlug: "myname",
		},
		{
			name:         "alias with hyphens in slugs",
			hostname:     "my-site--my-user.sites.example.com",
			wantSiteSlug: "my-site",
			wantUserSlug: "my-user",
		},
		{
			name:         "alias with digits",
			hostname:     "site123--user456.sites.example.com",
			wantSiteSlug: "site123",
			wantUserSlug: "user456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ParseHost(tt.hostname, testSitesDomain, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.SiteSlug != tt.wantSiteSlug {
				t.Errorf("siteSlug = %q, want %q", got.SiteSlug, tt.wantSiteSlug)
			}
			if got.UserSlug != tt.wantUserSlug {
				t.Errorf("userSlug = %q, want %q", got.UserSlug, tt.wantUserSlug)
			}
			if got.Pattern != resolver.PatternAlias {
				t.Errorf("pattern = %d, want PatternAlias (%d)", got.Pattern, resolver.PatternAlias)
			}
		})
	}
}

// ==============================================================================
// Alias disabled — when aliasEnabled=false, -- hostnames fall through
// to preferred parsing and fail if they don't have a dot.
// ==============================================================================

func TestParseHostAliasDisabled(t *testing.T) {
	// When alias is disabled, a hostname like "foo--bar.sites..." should
	// fall through to preferred parsing.  Since "foo--bar" has no dot,
	// it should return ErrInvalidHost.
	_, err := resolver.ParseHost("foobar--myname.sites.example.com", testSitesDomain, false)
	if err == nil {
		t.Fatal("expected error when alias is disabled for alias-format hostname")
	}
	if !errors.Is(err, resolver.ErrInvalidHost) {
		t.Fatalf("expected ErrInvalidHost, got %v", err)
	}
}

// ==============================================================================
// Error cases
// ==============================================================================

func TestParseHostErrors(t *testing.T) {
	tests := []struct {
		name         string
		hostname     string
		aliasEnabled bool
	}{
		{
			name:         "empty hostname",
			hostname:     "",
			aliasEnabled: false,
		},
		{
			name:         "no sites domain suffix",
			hostname:     "example.com",
			aliasEnabled: false,
		},
		{
			name:         "sites domain but no slug portion",
			hostname:     ".sites.example.com",
			aliasEnabled: false,
		},
		{
			name:         "only sites domain with leading dot",
			hostname:     ".sites.example.com",
			aliasEnabled: true,
		},
		{
			name:         "wrong domain suffix",
			hostname:     "foobar.myname.other.example.com",
			aliasEnabled: false,
		},
		{
			name:         "not enough slug segments in preferred",
			hostname:     "onlyslug.sites.example.com",
			aliasEnabled: false,
		},
		{
			name:         "empty site slug in alias",
			hostname:     "--myname.sites.example.com",
			aliasEnabled: true,
		},
		{
			name:         "empty user slug in alias",
			hostname:     "foobar--.sites.example.com",
			aliasEnabled: true,
		},
		{
			name:         "empty site slug in preferred",
			hostname:     ".myname.sites.example.com",
			aliasEnabled: false,
		},
		{
			name:         "empty user slug in preferred",
			hostname:     "foobar..sites.example.com",
			aliasEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.ParseHost(tt.hostname, testSitesDomain, tt.aliasEnabled)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, resolver.ErrInvalidHost) {
				t.Fatalf("expected ErrInvalidHost, got %v", err)
			}
		})
	}
}

// ==============================================================================
// Different sites domains
// ==============================================================================

func TestParseHostDifferentDomain(t *testing.T) {
	got, err := resolver.ParseHost("app.user.sites.example.com", "sites.example.com", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteSlug != "app" || got.UserSlug != "user" {
		t.Errorf("got siteSlug=%q userSlug=%q, want app/user", got.SiteSlug, got.UserSlug)
	}

	got, err = resolver.ParseHost("app--user.sites.example.com", "sites.example.com", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteSlug != "app" || got.UserSlug != "user" {
		t.Errorf("got siteSlug=%q userSlug=%q, want app/user", got.SiteSlug, got.UserSlug)
	}
}
