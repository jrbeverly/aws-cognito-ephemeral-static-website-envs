// ==============================================================================
// repository_test.go — Tests for the DynamoDB metadata repositories
//
// Uses the in-memory MockDynamoDB to verify all access patterns listed
// in the issue acceptance criteria:
//   - List user sites
//   - Resolve host → owner → site → active version → S3 prefix
//   - Read upload status and validation result
//   - Atomic active-version pointer updates
// ==============================================================================

package dynamodb_test

import (
	"context"
	"testing"
	"time"

	"backend/go-api/internal/metadata"
	"backend/go-api/internal/metadata/dynamodb"
	"backend/go-api/internal/metadata/dynamodb/testutil"
)

const (
	testSiteMetadataTable = "test-metadata"
	testUploadRecordsTable = "test-uploads"
)

// ==============================================================================
// Test helpers
// ==============================================================================

func setupSiteMetadataRepo(t *testing.T) (metadata.SiteMetadataRepository, *testutil.MockDynamoDB) {
	t.Helper()
	mock := testutil.NewMockDynamoDB()
	repo := dynamodb.NewSiteMetadataRepository(mock, testSiteMetadataTable)
	return repo, mock
}

func setupUploadRecordsRepo(t *testing.T) (metadata.UploadRecordsRepository, *testutil.MockDynamoDB) {
	t.Helper()
	mock := testutil.NewMockDynamoDB()
	repo := dynamodb.NewUploadRecordsRepository(mock, testUploadRecordsTable)
	return repo, mock
}

func mustCreateUser(t *testing.T, repo metadata.SiteMetadataRepository, userID, userSlug string) {
	t.Helper()
	err := repo.CreateUser(context.Background(), metadata.User{
		UserID:     userID,
		UserSlug:   userSlug,
		CognitoSub: userID,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
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
		t.Fatalf("failed to create site: %v", err)
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
		t.Fatalf("failed to create host mapping: %v", err)
	}
}

func mustCreateUpload(t *testing.T, repo metadata.UploadRecordsRepository, userID, uploadID, siteID, stagingKey string) {
	t.Helper()
	err := repo.CreateUpload(context.Background(), metadata.Upload{
		UploadID:     uploadID,
		UserID:       userID,
		SiteID:       siteID,
		Status:       metadata.UploadStatusPending,
		S3StagingKey: stagingKey,
	})
	if err != nil {
		t.Fatalf("failed to create upload: %v", err)
	}
}

// ==============================================================================
// User CRUD
// ==============================================================================

func TestUserCreateAndGet(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	u := metadata.User{
		UserID:     "sub-abc-123",
		UserSlug:   "alice",
		CognitoSub: "sub-abc-123",
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := repo.GetUser(ctx, "sub-abc-123")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.UserSlug != "alice" {
		t.Errorf("expected slug 'alice', got %q", got.UserSlug)
	}
	if got.CognitoSub != "sub-abc-123" {
		t.Errorf("expected cognitoSub 'sub-abc-123', got %q", got.CognitoSub)
	}
	if got.Disabled {
		t.Error("new user should not be disabled")
	}
	if got.CreatedAt.IsZero() {
		t.Error("createdAt should be set")
	}
}

func TestUserCreateDuplicate(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	err := repo.CreateUser(ctx, metadata.User{UserID: "sub-1", UserSlug: "alice"})
	if err == nil {
		t.Fatal("expected duplicate user error")
	}
}

func TestUserNotFound(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	got, err := repo.GetUser(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent user")
	}
}

func TestUserUpdate(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")

	err := repo.UpdateUser(ctx, metadata.User{
		UserID:   "sub-1",
		UserSlug: "alice-new",
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, _ := repo.GetUser(ctx, "sub-1")
	if got.UserSlug != "alice-new" {
		t.Errorf("expected slug 'alice-new', got %q", got.UserSlug)
	}
}

func TestUserDelete(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")

	if err := repo.DeleteUser(ctx, "sub-1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	got, _ := repo.GetUser(ctx, "sub-1")
	if !got.Disabled {
		t.Error("user should be disabled after soft delete")
	}
}

// ==============================================================================
// Site CRUD and listing
// ==============================================================================

func TestSiteCreateListAndGet(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")

	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")
	mustCreateSite(t, repo, "sub-1", "site-b", "docs-site")
	mustCreateSite(t, repo, "sub-1", "site-c", "landing-page")

	sites, err := repo.ListSites(ctx, "sub-1")
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}
	if len(sites) != 3 {
		t.Errorf("expected 3 sites, got %d", len(sites))
	}

	// Verify each site is retrievable.
	site, err := repo.GetSite(ctx, "sub-1", "site-b")
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	if site == nil {
		t.Fatal("expected site-b to exist")
	}
	if site.SiteSlug != "docs-site" {
		t.Errorf("expected slug 'docs-site', got %q", site.SiteSlug)
	}
	if site.UserID != "sub-1" {
		t.Errorf("expected owner 'sub-1', got %q", site.UserID)
	}
}

func TestListSitesEmptyUser(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-2", "bob")

	sites, err := repo.ListSites(ctx, "sub-2")
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestSiteNotFound(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")

	got, err := repo.GetSite(ctx, "sub-1", "nonexistent")
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent site")
	}
}

func TestSiteDelete(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")

	if err := repo.DeleteSite(ctx, "sub-1", "site-a"); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	got, _ := repo.GetSite(ctx, "sub-1", "site-a")
	if !got.Disabled {
		t.Error("site should be disabled after soft delete")
	}
}

// ==============================================================================
// Host mapping — host → owner → site → active version → S3 prefix
// ==============================================================================

func TestHostMappingCreateAndResolve(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")

	err := repo.CreateHostMapping(ctx, metadata.HostMapping{
		Hostname:          "my-blog.alice.sites.example.com",
		UserID:            "sub-1",
		UserSlug:          "alice",
		SiteID:            "site-a",
		SiteSlug:          "my-blog",
		VersionID:         "v1",
		S3PublishedPrefix: "published/users/alice/sites/my-blog/versions/v1/",
	})
	if err != nil {
		t.Fatalf("CreateHostMapping: %v", err)
	}

	// Host resolution should return the full chain.
	mapping, err := repo.GetHostMapping(ctx, "my-blog.alice.sites.example.com")
	if err != nil {
		t.Fatalf("GetHostMapping: %v", err)
	}
	if mapping == nil {
		t.Fatal("expected host mapping, got nil")
	}
	if mapping.UserID != "sub-1" {
		t.Errorf("expected userID 'sub-1', got %q", mapping.UserID)
	}
	if mapping.SiteID != "site-a" {
		t.Errorf("expected siteID 'site-a', got %q", mapping.SiteID)
	}
	if mapping.VersionID != "v1" {
		t.Errorf("expected versionID 'v1', got %q", mapping.VersionID)
	}
	if mapping.S3PublishedPrefix != "published/users/alice/sites/my-blog/versions/v1/" {
		t.Errorf("unexpected S3 prefix: %q", mapping.S3PublishedPrefix)
	}
	// Verify the full resolution chain.
	if mapping.UserSlug != "alice" {
		t.Errorf("expected userSlug 'alice', got %q", mapping.UserSlug)
	}
	if mapping.SiteSlug != "my-blog" {
		t.Errorf("expected siteSlug 'my-blog', got %q", mapping.SiteSlug)
	}
	// Denormalized disabled flags should be false on creation.
	if mapping.UserDisabled {
		t.Error("new host mapping should have userDisabled=false")
	}
	if mapping.SiteDisabled {
		t.Error("new host mapping should have siteDisabled=false")
	}
}

func TestHostMappingNotFound(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	got, err := repo.GetHostMapping(ctx, "unknown.sites.example.com")
	if err != nil {
		t.Fatalf("GetHostMapping: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown hostname")
	}
}

func TestHostMappingDuplicate(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateHostMapping(t, repo, "test.sites.example.com", "sub-1", "alice", "site-a", "my-blog")

	err := repo.CreateHostMapping(ctx, metadata.HostMapping{
		Hostname: "test.sites.example.com",
		UserID:   "sub-2",
	})
	if err == nil {
		t.Fatal("expected duplicate host mapping error")
	}
}

func TestHostMappingDelete(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")
	mustCreateHostMapping(t, repo, "test.sites.example.com", "sub-1", "alice", "site-a", "my-blog")

	if err := repo.DeleteHostMapping(ctx, "test.sites.example.com"); err != nil {
		t.Fatalf("DeleteHostMapping: %v", err)
	}

	got, _ := repo.GetHostMapping(ctx, "test.sites.example.com")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestHostMappingUpdateDisabledFlags(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")
	mustCreateHostMapping(t, repo, "test.sites.example.com", "sub-1", "alice", "site-a", "my-blog")

	// Simulate soft-deleting the site — set siteDisabled on the host mapping.
	mapping, _ := repo.GetHostMapping(ctx, "test.sites.example.com")
	mapping.SiteDisabled = true
	if err := repo.UpdateHostMapping(ctx, "test.sites.example.com", *mapping); err != nil {
		t.Fatalf("UpdateHostMapping: %v", err)
	}

	// Re-read and verify the flag was persisted.
	updated, err := repo.GetHostMapping(ctx, "test.sites.example.com")
	if err != nil {
		t.Fatalf("GetHostMapping after update: %v", err)
	}
	if updated == nil {
		t.Fatal("expected host mapping to exist after update")
	}
	if !updated.SiteDisabled {
		t.Error("siteDisabled should be true after update")
	}
	if updated.UserDisabled {
		t.Error("userDisabled should still be false")
	}
	// Other fields should be preserved.
	if updated.UserID != "sub-1" {
		t.Errorf("expected userID 'sub-1', got %q", updated.UserID)
	}
	if updated.SiteSlug != "my-blog" {
		t.Errorf("expected siteSlug 'my-blog', got %q", updated.SiteSlug)
	}
}

func TestHostMappingUpdateNotFound(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	err := repo.UpdateHostMapping(ctx, "nonexistent.sites.example.com",
		metadata.HostMapping{SiteDisabled: true})
	if err == nil {
		t.Fatal("expected error for nonexistent host mapping")
	}
}

// ==============================================================================
// Atomic active-version updates
// ==============================================================================

func TestUpdateActiveVersionAtomicSuccess(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")
	// Create both hostname patterns to match UpdateActiveVersion's transaction
	// (the handler creates both; the transaction expects both to exist).
	mustCreateHostMapping(t, repo, "my-blog.alice.sites.test", "sub-1", "alice", "site-a", "my-blog")
	mustCreateHostMapping(t, repo, "my-blog--alice.sites.test", "sub-1", "alice", "site-a", "my-blog")

	newVersionID := "v2"
	newPrefix := "published/users/alice/sites/my-blog/versions/v2/"

	err := repo.UpdateActiveVersion(ctx, "sub-1", "site-a", newVersionID, newPrefix, "sites.test")
	if err != nil {
		t.Fatalf("UpdateActiveVersion: %v", err)
	}

	// Site should reflect the new active version.
	site, err := repo.GetSite(ctx, "sub-1", "site-a")
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	if site.ActiveVersionID != newVersionID {
		t.Errorf("expected activeVersionId 'v2', got %q", site.ActiveVersionID)
	}
	if site.S3PublishedPrefix != newPrefix {
		t.Errorf("expected s3PublishedPrefix %q, got %q", newPrefix, site.S3PublishedPrefix)
	}

	// Host mapping should also reflect the new version.
	mapping, err := repo.GetHostMapping(ctx, "my-blog--alice.sites.test")
	if err != nil {
		t.Fatalf("GetHostMapping: %v", err)
	}
	if mapping.VersionID != newVersionID {
		t.Errorf("expected host mapping versionId 'v2', got %q", mapping.VersionID)
	}
	if mapping.S3PublishedPrefix != newPrefix {
		t.Errorf("expected host mapping prefix %q, got %q", newPrefix, mapping.S3PublishedPrefix)
	}
}

func TestUpdateActiveVersionSiteNotFound(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	err := repo.UpdateActiveVersion(ctx, "sub-unknown", "site-unknown", "v1", "prefix/", "sites.test")
	if err == nil {
		t.Fatal("expected error for nonexistent site")
	}
}

func TestUpdateActiveVersionDisabledSite(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")
	mustCreateSite(t, repo, "sub-1", "site-a", "my-blog")
	mustCreateHostMapping(t, repo, "my-blog.alice.sites.test", "sub-1", "alice", "site-a", "my-blog")
	mustCreateHostMapping(t, repo, "my-blog--alice.sites.test", "sub-1", "alice", "site-a", "my-blog")

	// Disable the site first.
	if err := repo.DeleteSite(ctx, "sub-1", "site-a"); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	// Updating the active version on a disabled site should fail.
	err := repo.UpdateActiveVersion(ctx, "sub-1", "site-a", "v2", "prefix/v2/", "sites.test")
	if err == nil {
		t.Fatal("expected error for disabled site")
	}
}

// ==============================================================================
// Upload CRUD and status tracking
// ==============================================================================

func TestUploadCreateAndGet(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	u := metadata.Upload{
		UploadID:     "upload-001",
		UserID:       "sub-1",
		SiteID:       "site-a",
		Status:       metadata.UploadStatusPending,
		S3StagingKey: "staging/users/sub-1/uploads/upload-001/source.zip",
	}
	if err := repo.CreateUpload(ctx, u); err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}

	got, err := repo.GetUpload(ctx, "sub-1", "upload-001")
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if got == nil {
		t.Fatal("expected upload, got nil")
	}
	if got.Status != metadata.UploadStatusPending {
		t.Errorf("expected status pending, got %q", got.Status)
	}
	if got.S3StagingKey != u.S3StagingKey {
		t.Errorf("unexpected staging key: %q", got.S3StagingKey)
	}
	if got.CreatedAt.IsZero() {
		t.Error("createdAt should be set")
	}
}

func TestUploadGetByIDViaGSI(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	mustCreateUpload(t, repo, "sub-1", "upload-gsi", "site-a", "staging/...")

	// Look up by upload ID alone (GSI query).
	got, err := repo.GetUploadByID(ctx, "upload-gsi")
	if err != nil {
		t.Fatalf("GetUploadByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected upload, got nil")
	}
	if got.UploadID != "upload-gsi" {
		t.Errorf("expected uploadID 'upload-gsi', got %q", got.UploadID)
	}
	if got.UserID != "sub-1" {
		t.Errorf("expected userID 'sub-1', got %q", got.UserID)
	}
}

func TestUploadGetByIDNotFound(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	got, err := repo.GetUploadByID(ctx, "nonexistent-upload")
	if err != nil {
		t.Fatalf("GetUploadByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown upload ID")
	}
}

func TestListUploadsForUser(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	mustCreateUpload(t, repo, "sub-1", "upload-a", "site-a", "staging/a")
	mustCreateUpload(t, repo, "sub-1", "upload-b", "site-b", "staging/b")
	mustCreateUpload(t, repo, "sub-2", "upload-c", "site-c", "staging/c") // different user

	uploads, err := repo.ListUploads(ctx, "sub-1")
	if err != nil {
		t.Fatalf("ListUploads: %v", err)
	}
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads for sub-1, got %d", len(uploads))
	}

	// User sub-2 should only see their own upload.
	uploads2, err := repo.ListUploads(ctx, "sub-2")
	if err != nil {
		t.Fatalf("ListUploads: %v", err)
	}
	if len(uploads2) != 1 {
		t.Errorf("expected 1 upload for sub-2, got %d", len(uploads2))
	}
}

func TestUpdateUploadStatus(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	mustCreateUpload(t, repo, "sub-1", "upload-status", "site-a", "staging/...")

	// Transition to validating.
	err := repo.UpdateUploadStatus(ctx, "sub-1", "upload-status", metadata.UploadStatusValidating, nil)
	if err != nil {
		t.Fatalf("UpdateUploadStatus (validating): %v", err)
	}
	got, _ := repo.GetUpload(ctx, "sub-1", "upload-status")
	if got.Status != metadata.UploadStatusValidating {
		t.Errorf("expected status validating, got %q", got.Status)
	}

	// Transition to published with validation result.
	vr := &metadata.ValidationResult{
		Valid:  true,
		Errors: nil,
	}
	err = repo.UpdateUploadStatus(ctx, "sub-1", "upload-status", metadata.UploadStatusPublished, vr)
	if err != nil {
		t.Fatalf("UpdateUploadStatus (published): %v", err)
	}
	got, _ = repo.GetUpload(ctx, "sub-1", "upload-status")
	if got.Status != metadata.UploadStatusPublished {
		t.Errorf("expected status published, got %q", got.Status)
	}
	if got.ValidationResult == nil {
		t.Fatal("expected validation result, got nil")
	}
	if !got.ValidationResult.Valid {
		t.Error("expected valid=true")
	}
}

func TestUpdateUploadStatusWithErrors(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	mustCreateUpload(t, repo, "sub-1", "upload-fail", "site-a", "staging/...")

	vr := &metadata.ValidationResult{
		Valid:  false,
		Errors: []string{"missing index.html", "path traversal detected"},
	}
	err := repo.UpdateUploadStatus(ctx, "sub-1", "upload-fail", metadata.UploadStatusFailed, vr)
	if err != nil {
		t.Fatalf("UpdateUploadStatus (failed): %v", err)
	}

	got, _ := repo.GetUpload(ctx, "sub-1", "upload-fail")
	if got.Status != metadata.UploadStatusFailed {
		t.Errorf("expected status failed, got %q", got.Status)
	}
	if got.ValidationResult == nil {
		t.Fatal("expected validation result, got nil")
	}
	if got.ValidationResult.Valid {
		t.Error("expected valid=false")
	}
	if len(got.ValidationResult.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(got.ValidationResult.Errors))
	}
}

func TestUploadNotFound(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	got, err := repo.GetUpload(ctx, "sub-1", "nonexistent")
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent upload")
	}
}

func TestUploadStatusUpdateNotFound(t *testing.T) {
	repo, _ := setupUploadRecordsRepo(t)
	ctx := context.Background()

	err := repo.UpdateUploadStatus(ctx, "sub-1", "nonexistent", metadata.UploadStatusPublished, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent upload")
	}
}

// ==============================================================================
// Lifecycle integration test — upload → validate → publish → serve
// ==============================================================================

func TestFullUploadToPublishLifecycle(t *testing.T) {
	ctx := context.Background()
	mock := testutil.NewMockDynamoDB()
	siteRepo := dynamodb.NewSiteMetadataRepository(mock, testSiteMetadataTable)
	uploadRepo := dynamodb.NewUploadRecordsRepository(mock, testUploadRecordsTable)

	// 1. Create user and site.
	mustCreateUser(t, siteRepo, "sub-alice", "alice")
	mustCreateSite(t, siteRepo, "sub-alice", "site-blog", "my-blog")

	// 2. Create both hostname patterns (handler creates both; UpdateActiveVersion expects both).
	mustCreateHostMapping(t, siteRepo, "my-blog.alice.sites.test", "sub-alice", "alice", "site-blog", "my-blog")
	mustCreateHostMapping(t, siteRepo, "my-blog--alice.sites.test", "sub-alice", "alice", "site-blog", "my-blog")

	// 3. Create an upload record (backend issues presigned URL).
	mustCreateUpload(t, uploadRepo, "sub-alice", "upload-001", "site-blog", "staging/users/sub-alice/uploads/upload-001/source.zip")

	// 4. Worker validates the upload.
	vr := &metadata.ValidationResult{
		Valid:  true,
		Errors: nil,
	}
	if err := uploadRepo.UpdateUploadStatus(ctx, "sub-alice", "upload-001", metadata.UploadStatusPublished, vr); err != nil {
		t.Fatalf("update status: %v", err)
	}

	// 5. Worker publishes — update active version atomically.
	newPrefix := "published/users/alice/sites/my-blog/versions/v1/"
	if err := siteRepo.UpdateActiveVersion(ctx, "sub-alice", "site-blog", "v1", newPrefix, "sites.test"); err != nil {
		t.Fatalf("update active version: %v", err)
	}

	// 6. Gateway resolves host to S3 prefix.
	mapping, err := siteRepo.GetHostMapping(ctx, "my-blog--alice.sites.test")
	if err != nil {
		t.Fatalf("get host mapping: %v", err)
	}
	if mapping == nil {
		t.Fatal("host mapping should exist")
	}
	if mapping.S3PublishedPrefix != newPrefix {
		t.Errorf("gateway should serve from %q, got %q", newPrefix, mapping.S3PublishedPrefix)
	}
	if mapping.VersionID != "v1" {
		t.Errorf("expected version v1, got %q", mapping.VersionID)
	}

	// 7. Verify upload status reflects publish.
	upload, err := uploadRepo.GetUpload(ctx, "sub-alice", "upload-001")
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if upload.Status != metadata.UploadStatusPublished {
		t.Errorf("expected upload published, got %q", upload.Status)
	}

	// 8. Verify the site shows the active version.
	site, err := siteRepo.GetSite(ctx, "sub-alice", "site-blog")
	if err != nil {
		t.Fatalf("get site: %v", err)
	}
	if site.ActiveVersionID != "v1" {
		t.Errorf("expected active version v1, got %q", site.ActiveVersionID)
	}
}

// ==============================================================================
// Edge cases
// ==============================================================================

func TestTimestampFormat(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-time", "tim")

	got, _ := repo.GetUser(ctx, "sub-time")
	if got.CreatedAt.IsZero() {
		t.Error("createdAt should be a valid timestamp")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("updatedAt should be a valid timestamp")
	}
	// Verify the timestamps are recent.
	if time.Since(got.CreatedAt) > 5*time.Second {
		t.Errorf("createdAt seems stale: %v", got.CreatedAt)
	}
}

func TestUserSitesIsolation(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-alice", "alice")
	mustCreateUser(t, repo, "sub-bob", "bob")

	mustCreateSite(t, repo, "sub-alice", "site-a1", "alice-site")
	mustCreateSite(t, repo, "sub-bob", "site-b1", "bob-site")

	// Alice should not see Bob's site.
	aliceSites, _ := repo.ListSites(ctx, "sub-alice")
	if len(aliceSites) != 1 {
		t.Errorf("alice should have 1 site, got %d", len(aliceSites))
	}
	if aliceSites[0].SiteSlug != "alice-site" {
		t.Errorf("expected alice-site, got %q", aliceSites[0].SiteSlug)
	}

	bobSites, _ := repo.ListSites(ctx, "sub-bob")
	if len(bobSites) != 1 {
		t.Errorf("bob should have 1 site, got %d", len(bobSites))
	}
	if bobSites[0].SiteSlug != "bob-site" {
		t.Errorf("expected bob-site, got %q", bobSites[0].SiteSlug)
	}

	// Alice should not be able to get Bob's site (wrong user ID).
	site, _ := repo.GetSite(ctx, "sub-alice", "site-b1")
	if site != nil {
		t.Error("alice should not be able to get bob's site")
	}
}

func TestDisabledFlagPersistence(t *testing.T) {
	repo, _ := setupSiteMetadataRepo(t)
	ctx := context.Background()

	mustCreateUser(t, repo, "sub-1", "alice")

	// User should start not disabled.
	got, _ := repo.GetUser(ctx, "sub-1")
	if got.Disabled {
		t.Error("new user should not be disabled")
	}

	// Disable via UpdateUser.
	if err := repo.UpdateUser(ctx, metadata.User{UserID: "sub-1", UserSlug: "alice", Disabled: true}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, _ = repo.GetUser(ctx, "sub-1")
	if !got.Disabled {
		t.Error("user should be disabled")
	}
}
