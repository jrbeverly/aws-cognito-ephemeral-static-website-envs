// ==============================================================================
// resolver_test.go — Tests for host-to-site resolution
//
// Uses the in-memory MockDynamoDB from the dynamodb/testutil package to
// verify the full resolution chain:
//   - Correct host resolves to correct S3 prefix and active version
//   - Deleted/disabled sites do not serve
//   - Deleted/disabled users do not serve
//   - Missing active version returns ErrNoActiveVersion
//   - Unknown host returns ErrHostNotFound
// ==============================================================================

package resolver_test

import (
	"context"
	"errors"
	"testing"

	"backend/go-api/internal/metadata"
	"backend/go-api/internal/metadata/dynamodb"
	"backend/go-api/internal/metadata/dynamodb/testutil"
	"backend/go-api/internal/resolver"
)

const testTable = "test-metadata"

// ==============================================================================
// Test helpers
// ==============================================================================

func setupRepo(t *testing.T) metadata.SiteMetadataRepository {
	t.Helper()
	mock := testutil.NewMockDynamoDB()
	return dynamodb.NewSiteMetadataRepository(mock, testTable)
}

func createUser(t *testing.T, repo metadata.SiteMetadataRepository, userID, userSlug string) {
	t.Helper()
	err := repo.CreateUser(context.Background(), metadata.User{
		UserID:     userID,
		UserSlug:   userSlug,
		CognitoSub: userID,
	})
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
}

func createSite(t *testing.T, repo metadata.SiteMetadataRepository, userID, siteID, siteSlug string) {
	t.Helper()
	err := repo.CreateSite(context.Background(), metadata.Site{
		SiteID:   siteID,
		UserID:   userID,
		SiteSlug: siteSlug,
	})
	if err != nil {
		t.Fatalf("createSite: %v", err)
	}
}

func createHostMapping(t *testing.T, repo metadata.SiteMetadataRepository, hostname, userID, userSlug, siteID, siteSlug, versionID, s3Prefix string) {
	t.Helper()
	err := repo.CreateHostMapping(context.Background(), metadata.HostMapping{
		Hostname:          hostname,
		UserID:            userID,
		UserSlug:          userSlug,
		SiteID:            siteID,
		SiteSlug:          siteSlug,
		VersionID:         versionID,
		S3PublishedPrefix: s3Prefix,
	})
	if err != nil {
		t.Fatalf("createHostMapping: %v", err)
	}
}

// fullFixture creates a complete setup: user, site, host mapping with an active version.
func fullFixture(t *testing.T) (repo metadata.SiteMetadataRepository, hostname, s3Prefix string) {
	t.Helper()
	repo = setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	versionID := "v1"
	s3Prefix = "published/users/myname/sites/foobar/versions/v1/"
	hostname = "foobar.myname.sites.example.com"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, versionID, s3Prefix)

	return repo, hostname, s3Prefix
}

// ==============================================================================
// Happy path — correct host resolves to the correct S3 prefix and version
// ==============================================================================

func TestResolveSuccess(t *testing.T) {
	repo, hostname, wantPrefix := fullFixture(t)

	got, err := resolver.Resolve(context.Background(), repo, hostname)
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", hostname, err)
	}
	if got.S3PublishedPrefix != wantPrefix {
		t.Errorf("S3PublishedPrefix = %q, want %q", got.S3PublishedPrefix, wantPrefix)
	}
	if got.VersionID != "v1" {
		t.Errorf("VersionID = %q, want %q", got.VersionID, "v1")
	}
	if got.SiteSlug != "foobar" {
		t.Errorf("SiteSlug = %q, want %q", got.SiteSlug, "foobar")
	}
	if got.UserSlug != "myname" {
		t.Errorf("UserSlug = %q, want %q", got.UserSlug, "myname")
	}
	if got.Hostname != hostname {
		t.Errorf("Hostname = %q, want %q", got.Hostname, hostname)
	}
}

// ==============================================================================
// Unknown host
// ==============================================================================

func TestResolveUnknownHost(t *testing.T) {
	repo := setupRepo(t)

	_, err := resolver.Resolve(context.Background(), repo, "nonexistent.sites.example.com")
	if err == nil {
		t.Fatal("expected error for unknown host, got nil")
	}
	if !errors.Is(err, resolver.ErrHostNotFound) {
		t.Fatalf("expected ErrHostNotFound, got %v", err)
	}
}

// ==============================================================================
// Deleted/disabled site does not serve (VISION.md §12)
// ==============================================================================

func TestResolveSiteDisabled(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	hostname := "foobar.myname.sites.example.com"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, "v1", "published/users/myname/sites/foobar/versions/v1/")

	// Soft-delete the site.
	err := repo.DeleteSite(context.Background(), userID, siteID)
	if err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), repo, hostname)
	if err == nil {
		t.Fatal("expected error for disabled site, got nil")
	}
	if !errors.Is(err, resolver.ErrSiteDisabled) {
		t.Fatalf("expected ErrSiteDisabled, got %v", err)
	}
}

// ==============================================================================
// Deleted/disabled user does not serve
// ==============================================================================

func TestResolveUserDisabled(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	hostname := "foobar.myname.sites.example.com"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, "v1", "published/users/myname/sites/foobar/versions/v1/")

	// Soft-delete the user.
	err := repo.DeleteUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), repo, hostname)
	if err == nil {
		t.Fatal("expected error for disabled user, got nil")
	}
	if !errors.Is(err, resolver.ErrUserDisabled) {
		t.Fatalf("expected ErrUserDisabled, got %v", err)
	}
}

// ==============================================================================
// Missing active version
// ==============================================================================

func TestResolveNoActiveVersion(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	hostname := "foobar.myname.sites.example.com"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	// Create host mapping with NO version (empty VersionID and S3PublishedPrefix).
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, "", "")

	_, err := resolver.Resolve(context.Background(), repo, hostname)
	if err == nil {
		t.Fatal("expected error for missing active version, got nil")
	}
	if !errors.Is(err, resolver.ErrNoActiveVersion) {
		t.Fatalf("expected ErrNoActiveVersion, got %v", err)
	}
}

// ==============================================================================
// Alias hostname pattern resolution
// ==============================================================================

func TestResolveAliasHostname(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	hostname := "foobar--myname.sites.example.com"
	wantPrefix := "published/users/myname/sites/foobar/versions/v1/"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, "v1", wantPrefix)

	got, err := resolver.Resolve(context.Background(), repo, hostname)
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", hostname, err)
	}
	if got.S3PublishedPrefix != wantPrefix {
		t.Errorf("S3PublishedPrefix = %q, want %q", got.S3PublishedPrefix, wantPrefix)
	}
	// Alias hostname resolved correctly — correct prefix, site, user.
	if got.SiteSlug != "foobar" {
		t.Errorf("SiteSlug = %q, want %q", got.SiteSlug, "foobar")
	}
	if got.UserSlug != "myname" {
		t.Errorf("UserSlug = %q, want %q", got.UserSlug, "myname")
	}
}

// ==============================================================================
// Both hostname patterns point to the same site
// ==============================================================================

func TestResolveBothPatternsSameSite(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	prefix := "published/users/myname/sites/foobar/versions/v1/"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)

	// Both hostnames map to the same site/version.
	createHostMapping(t, repo, "foobar.myname.sites.example.com", userID, userSlug, siteID, siteSlug, "v1", prefix)
	createHostMapping(t, repo, "foobar--myname.sites.example.com", userID, userSlug, siteID, siteSlug, "v1", prefix)

	// Preferred pattern.
	got1, err := resolver.Resolve(context.Background(), repo, "foobar.myname.sites.example.com")
	if err != nil {
		t.Fatalf("Resolve(preferred) unexpected error: %v", err)
	}
	if got1.S3PublishedPrefix != prefix {
		t.Errorf("preferred: S3PublishedPrefix = %q, want %q", got1.S3PublishedPrefix, prefix)
	}

	// Alias pattern.
	got2, err := resolver.Resolve(context.Background(), repo, "foobar--myname.sites.example.com")
	if err != nil {
		t.Fatalf("Resolve(alias) unexpected error: %v", err)
	}
	if got2.S3PublishedPrefix != prefix {
		t.Errorf("alias: S3PublishedPrefix = %q, want %q", got2.S3PublishedPrefix, prefix)
	}
}

// ==============================================================================
// Site disabled but user active — only site error returned
// ==============================================================================

func TestResolveSiteDisabledUserActive(t *testing.T) {
	repo := setupRepo(t)

	userID := "user-123"
	userSlug := "myname"
	siteID := "site-456"
	siteSlug := "foobar"
	hostname := "foobar.myname.sites.example.com"

	createUser(t, repo, userID, userSlug)
	createSite(t, repo, userID, siteID, siteSlug)
	createHostMapping(t, repo, hostname, userID, userSlug, siteID, siteSlug, "v1", "published/users/myname/sites/foobar/versions/v1/")

	// Disable only the site.
	err := repo.DeleteSite(context.Background(), userID, siteID)
	if err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), repo, hostname)
	if !errors.Is(err, resolver.ErrSiteDisabled) {
		t.Fatalf("expected ErrSiteDisabled, got %v", err)
	}
}

// ==============================================================================
// Verify resolution struct fields
// ==============================================================================

func TestResolveAllFields(t *testing.T) {
	repo, hostname, _ := fullFixture(t)

	got, err := resolver.Resolve(context.Background(), repo, hostname)
	if err != nil {
		t.Fatalf("Resolve unexpected error: %v", err)
	}

	// Every field must be populated.
	if got.Hostname == "" {
		t.Error("Hostname is empty")
	}
	if got.UserID == "" {
		t.Error("UserID is empty")
	}
	if got.UserSlug == "" {
		t.Error("UserSlug is empty")
	}
	if got.SiteID == "" {
		t.Error("SiteID is empty")
	}
	if got.SiteSlug == "" {
		t.Error("SiteSlug is empty")
	}
	if got.VersionID == "" {
		t.Error("VersionID is empty")
	}
	if got.S3PublishedPrefix == "" {
		t.Error("S3PublishedPrefix is empty")
	}
}
