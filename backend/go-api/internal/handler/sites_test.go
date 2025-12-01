// ==============================================================================
// sites_test.go — Authorization and lifecycle tests for site handlers
//
// Covers the acceptance criteria from issue #22:
//   - A user cannot list, modify, or delete another user's sites.
//   - Deletion stops serving via metadata state.
//   - Hostname/namespace conflicts are rejected.
//   - Slug validation rejects invalid slugs.
// ==============================================================================

package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/handler"
	"backend/go-api/internal/metadata"
	metadb "backend/go-api/internal/metadata/dynamodb"
	"backend/go-api/internal/metadata/dynamodb/testutil"
)

const testSitesDomain = "sites.example.com"

// ==============================================================================
// Test fixtures
// ==============================================================================

func setupSitesHandler(t *testing.T) (*handler.SitesHandler, metadata.SiteMetadataRepository) {
	t.Helper()
	mock := testutil.NewMockDynamoDB()
	repo := metadb.NewSiteMetadataRepository(mock, "test-metadata")
	h := handler.NewSitesHandler(repo, &mockCallbacks{}, testSitesDomain)
	return h, repo
}

// requestWithIdentity creates an HTTP request with an authenticated user
// identity stored in the request context.  This simulates what the
// Authenticate middleware does.
//
// pathValues is an alternating list of key-value pairs passed to
// r.SetPathValue for Go 1.22 path-parameter routing (e.g.,
// requestWithIdentity("GET", "/api/sites/{id}", "", id, "id", "site-1")).
func requestWithIdentity(method, path, body string, id *auth.Identity, pathValues ...string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for i := 0; i+1 < len(pathValues); i += 2 {
		r.SetPathValue(pathValues[i], pathValues[i+1])
	}
	if id != nil {
		ctx := auth.SetIdentity(context.Background(), id)
		r = r.WithContext(ctx)
	}
	return r
}

// requestWithoutIdentity creates an HTTP request with no identity set.
func requestWithoutIdentity(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

// decodeBody unmarshals the JSON response body into the provided target.
func decodeBody(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil && target != nil {
		// Error response — decode the error into the target (if it's an error type).
		// For success responses, decode the Data field.
		return
	}
	if target != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			t.Fatalf("decode data: %v", err)
		}
	}
}

// ==============================================================================
// Owner isolation tests — acceptance criterion "cannot list another user's sites"
// ==============================================================================

func TestListSites_ReturnsOnlyOwnSites(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	// Create two users, each with two sites.
	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateUser(t, repo, "sub-bob", "bob")
	mustCreateSite(t, repo, "sub-alice", "site-a1", "blog")
	mustCreateSite(t, repo, "sub-alice", "site-a2", "docs")
	mustCreateSite(t, repo, "sub-bob", "site-b1", "bob-site")

	// Alice requests her sites — should see 2.
	r := requestWithIdentity("GET", "/api/sites", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.ListSites(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var sites []map[string]any
	decodeBody(t, w, &sites)
	if len(sites) != 2 {
		t.Fatalf("alice sees %d sites, want 2", len(sites))
	}

	// Bob requests his sites — should see 1.
	r = requestWithIdentity("GET", "/api/sites", "", &auth.Identity{
		UserID:   "sub-bob",
		UserSlug: "bob",
	})
	w = httptest.NewRecorder()
	h.ListSites(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	decodeBody(t, w, &sites)
	if len(sites) != 1 {
		t.Fatalf("bob sees %d sites, want 1", len(sites))
	}
	if sites[0]["site_slug"] != "bob-site" {
		t.Errorf("bob's site slug = %q, want %q", sites[0]["site_slug"], "bob-site")
	}

	// Verify bob's response does not contain alice's site.
	for _, s := range sites {
		if s["user_id"] == "sub-alice" {
			t.Error("bob sees alice's site — ownership isolation broken")
		}
	}
}

func TestListSites_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _ := setupSitesHandler(t)

	r := requestWithoutIdentity("GET", "/api/sites")
	w := httptest.NewRecorder()
	h.ListSites(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ==============================================================================
// One site — acceptance criterion "cannot get another user's site"
// ==============================================================================

func TestGetSite_OwnSite(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateSite(t, repo, "sub-alice", "site-own", "my-blog")

	r := requestWithIdentity("GET", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "site-own")
	w := httptest.NewRecorder()
	h.GetSite(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var site map[string]any
	decodeBody(t, w, &site)
	if site["site_slug"] != "my-blog" {
		t.Errorf("site_slug = %q, want %q", site["site_slug"], "my-blog")
	}
}

func TestGetSite_OtherUserSite(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateUser(t, repo, "sub-bob", "bob")
	mustCreateSite(t, repo, "sub-bob", "site-bob", "bob-blog")

	// Alice tries to get Bob's site.  The PK includes the user ID, so
	// the repository returns nil → 404.  Alice cannot determine whether
	// the site exists.
	r := requestWithIdentity("GET", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "site-bob")
	w := httptest.NewRecorder()
	h.GetSite(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestGetSite_NotFound(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")

	r := requestWithIdentity("GET", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "nonexistent")
	w := httptest.NewRecorder()
	h.GetSite(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ==============================================================================
// Create site — acceptance criterion "hostname/namespace conflicts are rejected"
// ==============================================================================

func TestCreateSite_Success(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockDynamoDB()
	repo := metadb.NewSiteMetadataRepository(mock, "test-metadata")
	callbacks := &mockCallbacks{}
	h := handler.NewSitesHandler(repo, callbacks, testSitesDomain)

	mustCreateUser(t, repo, "sub-alice", "alice")

	r := requestWithIdentity("POST", "/api/sites", `{"site_slug":"my-blog"}`, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateSite(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var site map[string]any
	decodeBody(t, w, &site)

	if site["site_slug"] != "my-blog" {
		t.Errorf("site_slug = %q, want %q", site["site_slug"], "my-blog")
	}
	if site["user_id"] != "sub-alice" {
		t.Errorf("user_id = %q, want %q", site["user_id"], "sub-alice")
	}
	if site["site_id"] == "" {
		t.Error("site_id must not be empty")
	}
	hostnames, ok := site["hostnames"].([]any)
	if !ok || len(hostnames) != 1 || hostnames[0] != "my-blog--alice.sites.example.com" {
		t.Fatalf("expected only the alias hostname, got %v", site["hostnames"])
	}

	// Verify both host mappings were created.
	mapping1, _ := repo.GetHostMapping(context.Background(), "my-blog.alice.sites.example.com")
	if mapping1 == nil {
		t.Error("preferred host mapping was not created")
	}
	mapping2, _ := repo.GetHostMapping(context.Background(), "my-blog--alice.sites.example.com")
	if mapping2 == nil {
		t.Error("alias host mapping was not created")
	}

	callbacks.mu.Lock()
	defer callbacks.mu.Unlock()
	if len(callbacks.urls) != 1 || callbacks.urls[0] != "https://my-blog--alice.sites.example.com/oauth2/idpresponse" {
		t.Fatalf("registered callback URLs = %v", callbacks.urls)
	}
}

func TestCreateSite_InvalidSlug(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")

	badSlugs := []string{
		"",            // empty
		"_underscore", // underscore
		"-leading",    // leading dash
		"trailing-",   // trailing dash
		"my blog",     // space
		"über",        // non-ASCII
	}
	for _, slug := range badSlugs {
		body := `{"site_slug":"` + slug + `"}`
		r := requestWithIdentity("POST", "/api/sites", body, &auth.Identity{
			UserID:   "sub-alice",
			UserSlug: "alice",
		})
		w := httptest.NewRecorder()
		h.CreateSite(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("CreateSite(%q): status = %d, want %d", slug, w.Code, http.StatusBadRequest)
		}
	}
}

func TestCreateSite_HostnameConflict(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateUser(t, repo, "sub-bob", "bob")

	// Alice creates a site with slug "my-blog".
	r := requestWithIdentity("POST", "/api/sites", `{"site_slug":"my-blog"}`, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateSite(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Bob tries to create a site with the same slug "my-blog".
	// The hostname "my-blog--bob.sites..." is different from alice's,
	// so this should succeed — the namespace includes the user slug.
	r = requestWithIdentity("POST", "/api/sites", `{"site_slug":"my-blog"}`, &auth.Identity{
		UserID:   "sub-bob",
		UserSlug: "bob",
	})
	w = httptest.NewRecorder()
	h.CreateSite(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("bob's create: status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// But Alice cannot create the same slug again — it would produce
	// the same hostnames and conflict at the host-mapping layer.
	r = requestWithIdentity("POST", "/api/sites", `{"site_slug":"my-blog"}`, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w = httptest.NewRecorder()
	h.CreateSite(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("second alice create: status = %d, want %d. body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestCreateSite_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _ := setupSitesHandler(t)

	r := requestWithoutIdentity("POST", "/api/sites")
	w := httptest.NewRecorder()
	h.CreateSite(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ==============================================================================
// Delete site — acceptance criterion "deletion stops serving via metadata state"
// ==============================================================================

func TestDeleteSite_Success(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateSite(t, repo, "sub-alice", "site-del", "my-blog")

	// Create host mappings so we can verify disabled-flag propagation.
	preferredHost := fmt.Sprintf("%s.%s.%s", "my-blog", "alice", testSitesDomain)
	aliasHost := fmt.Sprintf("%s--%s.%s", "my-blog", "alice", testSitesDomain)
	mustCreateHostMapping(t, repo, preferredHost, "sub-alice", "alice", "site-del", "my-blog")
	mustCreateHostMapping(t, repo, aliasHost, "sub-alice", "alice", "site-del", "my-blog")

	// Verify the site and host mappings are not disabled before deletion.
	site, _ := repo.GetSite(context.Background(), "sub-alice", "site-del")
	if site.Disabled {
		t.Fatal("site should not be disabled before deletion")
	}
	for _, host := range []string{preferredHost, aliasHost} {
		m, _ := repo.GetHostMapping(context.Background(), host)
		if m == nil {
			t.Fatalf("host mapping %q should exist before deletion", host)
		}
		if m.SiteDisabled {
			t.Errorf("host mapping %q should not have siteDisabled before deletion", host)
		}
	}

	r := requestWithIdentity("DELETE", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "site-del")
	w := httptest.NewRecorder()
	h.DeleteSite(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify the site is now disabled.
	site, _ = repo.GetSite(context.Background(), "sub-alice", "site-del")
	if !site.Disabled {
		t.Error("site should be disabled after deletion")
	}

	// Verify host mappings have siteDisabled propagated.
	for _, host := range []string{preferredHost, aliasHost} {
		m, err := repo.GetHostMapping(context.Background(), host)
		if err != nil {
			t.Fatalf("GetHostMapping(%q): %v", host, err)
		}
		if m == nil {
			t.Fatalf("host mapping %q should still exist after soft-delete", host)
		}
		if !m.SiteDisabled {
			t.Errorf("host mapping %q should have siteDisabled=true after soft-delete, got false", host)
		}
	}
}

func TestDeleteSite_OtherUserSite(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateUser(t, repo, "sub-bob", "bob")
	mustCreateSite(t, repo, "sub-bob", "site-bob", "bob-blog")

	// Alice tries to delete Bob's site.  The preliminary GetSite returns
	// nil (PK is scoped to alice's user ID), so it responds 404.
	r := requestWithIdentity("DELETE", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "site-bob")
	w := httptest.NewRecorder()
	h.DeleteSite(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}

	// Verify Bob's site is still intact and not disabled.
	site, _ := repo.GetSite(context.Background(), "sub-bob", "site-bob")
	if site == nil {
		t.Fatal("bob's site should still exist")
	}
	if site.Disabled {
		t.Error("bob's site should not be disabled by alice's attempt")
	}
}

func TestDeleteSite_NotFound(t *testing.T) {
	t.Parallel()
	h, repo := setupSitesHandler(t)

	mustCreateUser(t, repo, "sub-alice", "alice")

	r := requestWithIdentity("DELETE", "/api/sites/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "nonexistent")
	w := httptest.NewRecorder()
	h.DeleteSite(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteSite_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _ := setupSitesHandler(t)

	r := requestWithoutIdentity("DELETE", "/api/sites/{id}")
	r.SetPathValue("id", "site-123")
	w := httptest.NewRecorder()
	h.DeleteSite(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ==============================================================================
// Helper — create test data
// ==============================================================================

func mustCreateUser(t *testing.T, repo metadata.SiteMetadataRepository, userID, userSlug string) {
	t.Helper()
	err := repo.CreateUser(context.Background(), metadata.User{
		UserID:     userID,
		UserSlug:   userSlug,
		CognitoSub: userID,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", userID, err)
	}
}

func mustCreateSite(t *testing.T, repo metadata.SiteMetadataRepository, userID, siteID, siteSlug string) {
	t.Helper()
	err := repo.CreateSite(context.Background(), metadata.Site{
		SiteID:   siteID,
		UserID:   userID,
		SiteSlug: siteSlug,
	})
	if err != nil {
		t.Fatalf("create site %s for %s: %v", siteID, userID, err)
	}
}

func mustCreateHostMapping(t *testing.T, repo metadata.SiteMetadataRepository, hostname, userID, userSlug, siteID, siteSlug string) {
	t.Helper()
	err := repo.CreateHostMapping(context.Background(), metadata.HostMapping{
		Hostname: hostname,
		UserID:   userID,
		UserSlug: userSlug,
		SiteID:   siteID,
		SiteSlug: siteSlug,
	})
	if err != nil {
		t.Fatalf("create host mapping %s: %v", hostname, err)
	}
}
