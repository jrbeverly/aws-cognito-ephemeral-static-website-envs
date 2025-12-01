// ==============================================================================
// uploads_test.go — Authorization and grant tests for upload handlers
//
// Covers the acceptance criteria from issue #21:
//   - The client cannot influence the destination S3 key.
//   - A grant is bound to the authenticated user and a specific upload record.
//   - Frontend never receives AWS credentials.
//   - Prefix derivation and rejection cases.
// ==============================================================================

package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/handler"
	"backend/go-api/internal/metadata"
	metadb "backend/go-api/internal/metadata/dynamodb"
	"backend/go-api/internal/metadata/dynamodb/testutil"
	"backend/go-api/internal/metrics"
)

const testSitesBucket = "test-bucket"

// ==============================================================================
// Mock presign client
// ==============================================================================

// mockPresignClient implements handler.PresignClient for testing.
// It records every PresignPutObject call so tests can inspect the request
// parameters and verify prefix derivation.
type mockPresignClient struct {
	mu      sync.Mutex
	calls   []handler.PresignRequest
	nextURL string // URL to return on the next call
}

func newMockPresignClient() *mockPresignClient {
	return &mockPresignClient{}
}

func (m *mockPresignClient) PresignPutObject(_ context.Context, req handler.PresignRequest) (*handler.PresignResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	url := m.nextURL
	if url == "" {
		url = "https://s3.amazonaws.com/" + req.Bucket + "/" + req.Key + "?X-Amz-Signature=test"
	}
	m.mu.Unlock()

	return &handler.PresignResult{
		URL:       url,
		ExpiresAt: time.Now().Add(req.Expires),
	}, nil
}

// lastCall returns the most recent PresignRequest, or nil if none.
func (m *mockPresignClient) lastCall() *handler.PresignRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	cp := m.calls[len(m.calls)-1]
	return &cp
}

// callCount returns the number of PresignPutObject invocations.
func (m *mockPresignClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ==============================================================================
// Mock content store
// ==============================================================================

// mockContentStore implements handler.ContentStore for testing.
// It records every PutContent call so tests can inspect the stored content.
type mockContentStore struct {
	mu      sync.Mutex
	calls   []handler.ContentStoreRequest
	objects map[string][]byte
}

func newMockContentStore() *mockContentStore {
	return &mockContentStore{objects: map[string][]byte{}}
}

func (m *mockContentStore) PutContent(_ context.Context, req handler.ContentStoreRequest) error {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.objects[req.Key] = req.Body
	m.mu.Unlock()
	return nil
}

func (m *mockContentStore) GetContent(_ context.Context, _, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("no object %s", key)
	}
	return body, nil
}

// firstCall returns the first ContentStoreRequest, or nil if none.
func (m *mockContentStore) firstCall() *handler.ContentStoreRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	cp := m.calls[0]
	return &cp
}

// callFor returns the ContentStoreRequest for key, or nil if none.
func (m *mockContentStore) callFor(key string) *handler.ContentStoreRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.Key == key {
			cp := c
			return &cp
		}
	}
	return nil
}

// mockCallbacks records callback URLs registered on publish.
type mockCallbacks struct {
	mu   sync.Mutex
	urls []string
}

func (m *mockCallbacks) AddCallbackURL(_ context.Context, url string) error {
	m.mu.Lock()
	m.urls = append(m.urls, url)
	m.mu.Unlock()
	return nil
}

// lastCall returns the most recent ContentStoreRequest, or nil if none.
func (m *mockContentStore) lastCall() *handler.ContentStoreRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	cp := m.calls[len(m.calls)-1]
	return &cp
}

// callCount returns the number of PutContent invocations.
func (m *mockContentStore) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ==============================================================================
// Test fixtures
// ==============================================================================

func setupUploadsHandler(t *testing.T) (*handler.UploadsHandler, *mockPresignClient, *mockContentStore, metadata.SiteMetadataRepository, metadata.UploadRecordsRepository) {
	t.Helper()
	mock := testutil.NewMockDynamoDB()
	siteRepo := metadb.NewSiteMetadataRepository(mock, "test-metadata")
	uploadRepo := metadb.NewUploadRecordsRepository(mock, "test-uploads")
	presign := newMockPresignClient()
	contentStore := newMockContentStore()
	h := handler.NewUploadsHandler(siteRepo, uploadRepo, presign, contentStore, &mockCallbacks{}, testSitesBucket, testSitesDomain, metrics.NewEmitter())
	return h, presign, contentStore, siteRepo, uploadRepo
}

// mustCreateUserAndSite creates a user and site in the given repo for test setup.
func mustCreateUserAndSite(t *testing.T, repo metadata.SiteMetadataRepository, userID, userSlug, siteID, siteSlug string) {
	t.Helper()
	mustCreateUser(t, repo, userID, userSlug)
	mustCreateSite(t, repo, userID, siteID, siteSlug)
}

// mustCreateUpload creates an upload record in the given repo for test setup.
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
		t.Fatalf("mustCreateUpload %s: %v", uploadID, err)
	}
}

// ==============================================================================
// Grant issuance — happy path
// ==============================================================================

func TestCreateUpload_ZipSuccess(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `{"site_id":"site-blog","type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Decode the response.
	var resp map[string]any
	decodeBody(t, w, &resp)

	if resp["upload_id"] == nil || resp["upload_id"] == "" {
		t.Error("upload_id must not be empty")
	}
	uploadID, _ := resp["upload_id"].(string)
	if !strings.HasPrefix(uploadID, "upload_") {
		t.Errorf("upload_id = %q, want prefix 'upload_'", uploadID)
	}

	presignedURL, _ := resp["presigned_url"].(string)
	if presignedURL == "" {
		t.Error("presigned_url must not be empty")
	}
	if !strings.Contains(presignedURL, "X-Amz-Signature=test") {
		t.Errorf("presigned_url = %q, should contain signature", presignedURL)
	}

	stagingKey, _ := resp["staging_key"].(string)
	wantKeyPrefix := "staging/users/sub-alice/uploads/" + uploadID + "/"
	if !strings.HasPrefix(stagingKey, wantKeyPrefix) {
		t.Errorf("staging_key = %q, want prefix %q", stagingKey, wantKeyPrefix)
	}
	if !strings.HasSuffix(stagingKey, "source.zip") {
		t.Errorf("staging_key = %q, want suffix 'source.zip' for zip upload", stagingKey)
	}

	expiresAt, _ := resp["expires_at"].(string)
	if expiresAt == "" {
		t.Error("expires_at must not be empty")
	}

	// Verify the presign call received the correct key (server-derived).
	call := presign.lastCall()
	if call == nil {
		t.Fatal("expected a presign call")
	}
	if call.Key != stagingKey {
		t.Errorf("presign key = %q, want %q", call.Key, stagingKey)
	}
	if call.Bucket != testSitesBucket {
		t.Errorf("presign bucket = %q, want %q", call.Bucket, testSitesBucket)
	}
	if call.ContentType != "application/zip" {
		t.Errorf("presign content type = %q, want 'application/zip'", call.ContentType)
	}
}

func TestCreateUpload_IndexSuccess(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `{"site_id":"site-blog","type":"index"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	stagingKey, _ := resp["staging_key"].(string)
	if !strings.HasSuffix(stagingKey, "index.html") {
		t.Errorf("staging_key = %q, want suffix 'index.html' for index upload", stagingKey)
	}

	call := presign.lastCall()
	if call == nil {
		t.Fatal("expected a presign call")
	}
	if call.ContentType != "text/html" {
		t.Errorf("presign content type = %q, want 'text/html'", call.ContentType)
	}
	if !strings.HasSuffix(call.Key, "index.html") {
		t.Errorf("presign key = %q, want suffix 'index.html'", call.Key)
	}
}

// ==============================================================================
// Grant issuance — rejection cases
// ==============================================================================

func TestCreateUpload_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, presign, _, _, _ := setupUploadsHandler(t)

	r := requestWithoutIdentity("POST", "/api/uploads")
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for unauthenticated requests")
	}
}

func TestCreateUpload_SiteNotFound(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUser(t, siteRepo, "sub-alice", "alice")

	// Site doesn't exist — the user has no sites.
	body := `{"site_id":"nonexistent","type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for nonexistent site")
	}
}

func TestCreateUpload_WrongUserSite(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-bob", "bob", "site-bob", "bob-blog")

	// Alice tries to upload to Bob's site.
	body := `{"site_id":"site-bob","type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for another user's site")
	}
}

func TestCreateUpload_DisabledSite(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	// Disable the site.
	if err := siteRepo.DeleteSite(context.Background(), "sub-alice", "site-blog"); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	body := `{"site_id":"site-blog","type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for disabled site")
	}
}

func TestCreateUpload_InvalidType(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `{"site_id":"site-blog","type":"pdf"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for invalid type")
	}
}

func TestCreateUpload_MissingSiteID(t *testing.T) {
	t.Parallel()
	h, presign, _, _, _ := setupUploadsHandler(t)

	body := `{"type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made when site_id is missing")
	}
}

func TestCreateUpload_MalformedJSON(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `not valid json`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if presign.callCount() != 0 {
		t.Error("no presign call should be made for malformed JSON")
	}
}

// ==============================================================================
// Prefix derivation — acceptance criterion "client cannot influence the key"
// ==============================================================================

func TestCreateUpload_ClientCannotInfluenceStagingKey(t *testing.T) {
	t.Parallel()
	h, presign, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	// The client sends site_id and type.  They do NOT send an S3 key, prefix,
	// or any path component.  The test verifies that even if the client sends
	// extra fields, they are ignored and the key is derived from auth identity.
	body := `{"site_id":"site-blog","type":"zip","s3_key":"/evil/path","prefix":"admin/"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	stagingKey, _ := resp["staging_key"].(string)

	// The key must contain the authenticated user's ID, NOT anything the
	// client tried to inject.
	if strings.Contains(stagingKey, "evil") || strings.Contains(stagingKey, "admin") {
		t.Errorf("staging_key %q contains client-supplied values — key must be server-derived", stagingKey)
	}
	if !strings.Contains(stagingKey, "sub-alice") {
		t.Errorf("staging_key %q must contain the authenticated user ID", stagingKey)
	}

	// Verify the presign call used the server-derived key.
	call := presign.lastCall()
	if call == nil {
		t.Fatal("expected a presign call")
	}
	if call.Key != stagingKey {
		t.Errorf("presign key = %q, want %q", call.Key, stagingKey)
	}
	// The presign key must not contain any client-supplied path values.
	if strings.Contains(call.Key, "evil") || strings.Contains(call.Key, "admin") {
		t.Errorf("presign key %q contains client-supplied values", call.Key)
	}
}

func TestCreateUpload_UserIDInKeyMatchesAuthIdentity(t *testing.T) {
	t.Parallel()
	h, _, _, siteRepo, uploadRepo := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-bob", "bob", "site-bob", "bob-site")

	body := `{"site_id":"site-bob","type":"index"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-bob",
		UserSlug: "bob",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	stagingKey, _ := resp["staging_key"].(string)

	// The staging key embeds the user ID (sub-bob) — Bob's identity.
	if !strings.Contains(stagingKey, "sub-bob") {
		t.Errorf("staging_key %q must contain user ID 'sub-bob', got %q", stagingKey, "sub-bob")
	}

	// Verify the upload record in DynamoDB is scoped to Bob.
	uploadID, _ := resp["upload_id"].(string)
	upload, err := uploadRepo.GetUpload(context.Background(), "sub-bob", uploadID)
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if upload == nil {
		t.Fatal("upload record should exist")
	}
	if upload.UserID != "sub-bob" {
		t.Errorf("upload record userID = %q, want 'sub-bob'", upload.UserID)
	}
	if upload.S3StagingKey != stagingKey {
		t.Errorf("upload record staging key = %q, want %q", upload.S3StagingKey, stagingKey)
	}
}

// ==============================================================================
// Upload record creation — acceptance criterion "bound to user and upload ID"
// ==============================================================================

func TestCreateUpload_CreatesUploadRecord(t *testing.T) {
	t.Parallel()
	h, _, _, siteRepo, uploadRepo := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `{"site_id":"site-blog","type":"zip"}`
	r := requestWithIdentity("POST", "/api/uploads", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreateUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)
	uploadID, _ := resp["upload_id"].(string)

	// Verify the upload record exists with status pending.
	upload, err := uploadRepo.GetUpload(context.Background(), "sub-alice", uploadID)
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if upload == nil {
		t.Fatal("upload record should exist in DynamoDB")
	}
	if upload.Status != metadata.UploadStatusPending {
		t.Errorf("upload status = %q, want %q", upload.Status, metadata.UploadStatusPending)
	}
	if upload.UserID != "sub-alice" {
		t.Errorf("upload userID = %q, want 'sub-alice'", upload.UserID)
	}
	if upload.SiteID != "site-blog" {
		t.Errorf("upload siteID = %q, want 'site-blog'", upload.SiteID)
	}
	if upload.S3StagingKey == "" {
		t.Error("upload staging key must not be empty")
	}
	// The staging key in the record must match the one returned to the client.
	stagingKey, _ := resp["staging_key"].(string)
	if upload.S3StagingKey != stagingKey {
		t.Errorf("record staging key = %q, response staging key = %q", upload.S3StagingKey, stagingKey)
	}
}

// ==============================================================================
// Upload status retrieval
// ==============================================================================

func TestGetUploadStatus_Success(t *testing.T) {
	t.Parallel()
	h, _, _, siteRepo, uploadRepo := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	// Create an upload record directly.
	mustCreateUpload(t, uploadRepo, "sub-alice", "upload-001", "site-blog",
		"staging/users/sub-alice/uploads/upload-001/source.zip")

	r := requestWithIdentity("GET", "/api/uploads/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "upload-001")
	w := httptest.NewRecorder()
	h.GetUploadStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	if resp["upload_id"] != "upload-001" {
		t.Errorf("upload_id = %q, want 'upload-001'", resp["upload_id"])
	}
	if resp["status"] != string(metadata.UploadStatusPending) {
		t.Errorf("status = %q, want %q", resp["status"], metadata.UploadStatusPending)
	}
	if resp["site_id"] != "site-blog" {
		t.Errorf("site_id = %q, want 'site-blog'", resp["site_id"])
	}
}

func TestGetUploadStatus_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := setupUploadsHandler(t)

	r := requestWithoutIdentity("GET", "/api/uploads/{id}")
	r.SetPathValue("id", "upload-001")
	w := httptest.NewRecorder()
	h.GetUploadStatus(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestGetUploadStatus_NotFound(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := setupUploadsHandler(t)

	r := requestWithIdentity("GET", "/api/uploads/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "nonexistent")
	w := httptest.NewRecorder()
	h.GetUploadStatus(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestGetUploadStatus_WrongUser(t *testing.T) {
	t.Parallel()
	h, _, _, _, uploadRepo := setupUploadsHandler(t)

	// Bob owns upload-001.
	mustCreateUpload(t, uploadRepo, "sub-bob", "upload-001", "site-bob",
		"staging/users/sub-bob/uploads/upload-001/source.zip")

	// Alice tries to access it — her PK search won't find it.
	r := requestWithIdentity("GET", "/api/uploads/{id}", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "upload-001")
	w := httptest.NewRecorder()
	h.GetUploadStatus(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// ==============================================================================
// Two grants for the same site produce different keys
// ==============================================================================

func TestCreateUpload_DifferentUploadsDifferentKeys(t *testing.T) {
	t.Parallel()
	h, _, _, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	// First upload.
	r1 := requestWithIdentity("POST", "/api/uploads", `{"site_id":"site-blog","type":"zip"}`, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w1 := httptest.NewRecorder()
	h.CreateUpload(w1, r1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first upload: status = %d, want %d", w1.Code, http.StatusCreated)
	}
	var resp1 map[string]any
	decodeBody(t, w1, &resp1)
	key1, _ := resp1["staging_key"].(string)
	id1, _ := resp1["upload_id"].(string)

	// Second upload.
	r2 := requestWithIdentity("POST", "/api/uploads", `{"site_id":"site-blog","type":"index"}`, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w2 := httptest.NewRecorder()
	h.CreateUpload(w2, r2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second upload: status = %d, want %d", w2.Code, http.StatusCreated)
	}
	var resp2 map[string]any
	decodeBody(t, w2, &resp2)
	key2, _ := resp2["staging_key"].(string)
	id2, _ := resp2["upload_id"].(string)

	// Unique upload IDs.
	if id1 == id2 {
		t.Errorf("upload IDs should be unique, got %q and %q", id1, id2)
	}

	// Different staging keys (different upload IDs).
	if key1 == key2 {
		t.Error("different uploads should produce different staging keys")
	}

	// Each key contains its own upload ID.
	if !strings.Contains(key1, id1) {
		t.Errorf("key %q must contain upload ID %q", key1, id1)
	}
	if !strings.Contains(key2, id2) {
		t.Errorf("key %q must contain upload ID %q", key2, id2)
	}
}

// ==============================================================================
// Upload completion — acceptance criterion "triggered exactly once per upload"
// ==============================================================================

// mustCreatePublishableSite creates the user, site and both host mappings that
// UpdateActiveVersion updates atomically on publish.
func mustCreatePublishableSite(t *testing.T, repo metadata.SiteMetadataRepository) {
	t.Helper()
	mustCreateUserAndSite(t, repo, "sub-alice", "alice", "site-blog", "my-blog")
	for _, host := range []string{"my-blog.alice." + testSitesDomain, "my-blog--alice." + testSitesDomain} {
		mustCreateHostMapping(t, repo, host, "sub-alice", "alice", "site-blog", "my-blog")
	}
}

func TestCompleteUpload_Success(t *testing.T) {
	t.Parallel()
	h, _, contentStore, siteRepo, uploadRepo := setupUploadsHandler(t)

	mustCreatePublishableSite(t, siteRepo)
	stagingKey := "staging/users/sub-alice/uploads/upload-001/index.html"
	mustCreateUpload(t, uploadRepo, "sub-alice", "upload-001", "site-blog", stagingKey)
	contentStore.objects[stagingKey] = []byte("<h1>hi</h1>")

	r := requestWithIdentity("POST", "/api/uploads/{id}/complete", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "upload-001")
	w := httptest.NewRecorder()
	h.CompleteUpload(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	if resp["upload_id"] != "upload-001" {
		t.Errorf("upload_id = %q, want 'upload-001'", resp["upload_id"])
	}
	if resp["status"] != string(metadata.UploadStatusPublished) {
		t.Errorf("status = %q, want %q", resp["status"], metadata.UploadStatusPublished)
	}

	// Verify the upload record was updated.
	upload, err := uploadRepo.GetUpload(context.Background(), "sub-alice", "upload-001")
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if upload.Status != metadata.UploadStatusPublished {
		t.Errorf("upload status = %q, want %q", upload.Status, metadata.UploadStatusPublished)
	}

	// The alias host mapping now points at the published version.
	mapping, err := siteRepo.GetHostMapping(context.Background(), "my-blog--alice."+testSitesDomain)
	if err != nil || mapping == nil {
		t.Fatalf("GetHostMapping: %v", err)
	}
	if !strings.HasPrefix(mapping.S3PublishedPrefix, "published/users/sub-alice/sites/site-blog/") {
		t.Errorf("mapping prefix = %q, want published/users/sub-alice/sites/site-blog/...", mapping.S3PublishedPrefix)
	}
	if got := contentStore.callFor(mapping.S3PublishedPrefix + "index.html"); got == nil || string(got.Body) != "<h1>hi</h1>" {
		t.Errorf("published index.html not written under %s", mapping.S3PublishedPrefix)
	}
}

func TestCompleteUpload_Idempotent(t *testing.T) {
	t.Parallel()
	h, _, _, _, uploadRepo := setupUploadsHandler(t)

	// Create an upload that's already uploaded.
	mustCreateUpload(t, uploadRepo, "sub-alice", "upload-001", "site-blog",
		"staging/users/sub-alice/uploads/upload-001/source.zip")
	if err := uploadRepo.UpdateUploadStatus(context.Background(), "sub-alice", "upload-001",
		metadata.UploadStatusUploaded, nil); err != nil {
		t.Fatalf("UpdateUploadStatus: %v", err)
	}

	r := requestWithIdentity("POST", "/api/uploads/{id}/complete", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "upload-001")
	w := httptest.NewRecorder()
	h.CompleteUpload(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)
	if resp["status"] != string(metadata.UploadStatusUploaded) {
		t.Errorf("status = %q, want %q", resp["status"], metadata.UploadStatusUploaded)
	}
}

func TestCompleteUpload_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := setupUploadsHandler(t)

	r := requestWithoutIdentity("POST", "/api/uploads/{id}/complete")
	r.SetPathValue("id", "upload-001")
	w := httptest.NewRecorder()
	h.CompleteUpload(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCompleteUpload_NotFound(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := setupUploadsHandler(t)

	r := requestWithIdentity("POST", "/api/uploads/{id}/complete", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "nonexistent")
	w := httptest.NewRecorder()
	h.CompleteUpload(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestCompleteUpload_WrongUser(t *testing.T) {
	t.Parallel()
	h, _, _, _, uploadRepo := setupUploadsHandler(t)

	// Bob owns the upload.
	mustCreateUpload(t, uploadRepo, "sub-bob", "upload-001", "site-bob",
		"staging/users/sub-bob/uploads/upload-001/source.zip")

	// Alice tries to complete it.
	r := requestWithIdentity("POST", "/api/uploads/{id}/complete", "", &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	}, "id", "upload-001")
	w := httptest.NewRecorder()
	h.CompleteUpload(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// ==============================================================================
// Paste upload — acceptance criterion "pasted-HTML path stores content as staged index.html"
// ==============================================================================

func TestCreatePasteUpload_Success(t *testing.T) {
	t.Parallel()
	h, _, contentStore, siteRepo, uploadRepo := setupUploadsHandler(t)

	mustCreatePublishableSite(t, siteRepo)

	body := `{"site_id":"site-blog","html":"<html><body><h1>Hello</h1></body></html>"}`
	r := requestWithIdentity("POST", "/api/uploads/paste", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	decodeBody(t, w, &resp)

	if resp["upload_id"] == nil || resp["upload_id"] == "" {
		t.Error("upload_id must not be empty")
	}
	uploadID, _ := resp["upload_id"].(string)
	if !strings.HasPrefix(uploadID, "upload_") {
		t.Errorf("upload_id = %q, want prefix 'upload_'", uploadID)
	}

	if resp["status"] != string(metadata.UploadStatusPublished) {
		t.Errorf("status = %q, want %q", resp["status"], metadata.UploadStatusPublished)
	}

	if resp["site_id"] != "site-blog" {
		t.Errorf("site_id = %q, want 'site-blog'", resp["site_id"])
	}

	stagingKey, _ := resp["staging_key"].(string)
	if !strings.Contains(stagingKey, "sub-alice") {
		t.Errorf("staging_key %q must contain user ID", stagingKey)
	}
	if !strings.HasSuffix(stagingKey, "index.html") {
		t.Errorf("staging_key = %q, want suffix 'index.html'", stagingKey)
	}

	// Verify content was staged first (publishing writes after it).
	call := contentStore.firstCall()
	if call == nil {
		t.Fatal("expected a PutContent call")
	}
	if call.Key != stagingKey {
		t.Errorf("content store key = %q, want %q", call.Key, stagingKey)
	}
	if string(call.Body) != "<html><body><h1>Hello</h1></body></html>" {
		t.Errorf("content store body = %q, want HTML content", string(call.Body))
	}
	if call.ContentType != "text/html" {
		t.Errorf("content store content type = %q, want 'text/html'", call.ContentType)
	}

	// Verify the upload record exists with status "published".
	upload, err := uploadRepo.GetUpload(context.Background(), "sub-alice", uploadID)
	if err != nil {
		t.Fatalf("GetUpload: %v", err)
	}
	if upload == nil {
		t.Fatal("upload record should exist")
	}
	if upload.Status != metadata.UploadStatusPublished {
		t.Errorf("upload status = %q, want %q", upload.Status, metadata.UploadStatusPublished)
	}
	if upload.UserID != "sub-alice" {
		t.Errorf("upload userID = %q, want 'sub-alice'", upload.UserID)
	}
}

func TestCreatePasteUpload_EmptyHTML(t *testing.T) {
	t.Parallel()
	h, _, contentStore, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")

	body := `{"site_id":"site-blog","html":""}`
	r := requestWithIdentity("POST", "/api/uploads/paste", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if contentStore.callCount() != 0 {
		t.Error("no PutContent call should be made for empty HTML")
	}
}

func TestCreatePasteUpload_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _, contentStore, _, _ := setupUploadsHandler(t)

	r := requestWithoutIdentity("POST", "/api/uploads/paste")
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if contentStore.callCount() != 0 {
		t.Error("no PutContent call should be made for unauthenticated requests")
	}
}

func TestCreatePasteUpload_DisabledSite(t *testing.T) {
	t.Parallel()
	h, _, contentStore, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-alice", "alice", "site-blog", "my-blog")
	if err := siteRepo.DeleteSite(context.Background(), "sub-alice", "site-blog"); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}

	body := `{"site_id":"site-blog","html":"<html></html>"}`
	r := requestWithIdentity("POST", "/api/uploads/paste", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if contentStore.callCount() != 0 {
		t.Error("no PutContent call should be made for disabled site")
	}
}

func TestCreatePasteUpload_WrongUserSite(t *testing.T) {
	t.Parallel()
	h, _, contentStore, siteRepo, _ := setupUploadsHandler(t)

	mustCreateUserAndSite(t, siteRepo, "sub-bob", "bob", "site-bob", "bob-blog")

	body := `{"site_id":"site-bob","html":"<html></html>"}`
	r := requestWithIdentity("POST", "/api/uploads/paste", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if contentStore.callCount() != 0 {
		t.Error("no PutContent call should be made for another user's site")
	}
}

func TestCreatePasteUpload_MissingSiteID(t *testing.T) {
	t.Parallel()
	h, _, contentStore, _, _ := setupUploadsHandler(t)

	body := `{"html":"<html></html>"}`
	r := requestWithIdentity("POST", "/api/uploads/paste", body, &auth.Identity{
		UserID:   "sub-alice",
		UserSlug: "alice",
	})
	w := httptest.NewRecorder()
	h.CreatePasteUpload(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if contentStore.callCount() != 0 {
		t.Error("no PutContent call should be made when site_id is missing")
	}
}
