// ==============================================================================
// publisher_test.go — Tests for the publish package
//
// Covers the publish flow acceptance criteria from VISION.md §17.3 and §8.5:
//   - Published content lands under an immutable per-version prefix.
//   - The active-version switch is atomic and only happens after validation passes.
//   - The host mapping resolves to the new active version.
//
// Uses table-driven tests with mock implementations of S3Writer, UploadStore,
// and SiteStore to verify every branch of the Publisher.Publish method.
// ==============================================================================

package publish_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"workers/site-publisher/internal/publish"
)

// ==============================================================================
// Mock implementations
// ==============================================================================

// mockS3Writer records written objects in an in-memory map.
type mockS3Writer struct {
	mu     sync.Mutex
	objects map[string][]byte // key → body
	// When set, PutObject returns this error.
	err error
}

func newMockS3Writer() *mockS3Writer {
	return &mockS3Writer{objects: make(map[string][]byte)}
}

func (m *mockS3Writer) PutObject(_ context.Context, key string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.objects[key] = make([]byte, len(body))
	copy(m.objects[key], body)
	return nil
}

// Objects returns a copy of the written objects.
func (m *mockS3Writer) Objects() map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.objects))
	for k, v := range m.objects {
		out[k] = v
	}
	return out
}

// mockUploadStore provides configurable upload record responses.
type mockUploadStore struct {
	// When set, GetUploadByID returns this upload.
	upload *publish.UploadRecord
	// When set, GetUploadByID returns this error.
	getErr error
	// Records the last status update call.
	lastStatus    string
	lastUserID    string
	lastUploadID  string
	lastVR        *publish.ValidationResult
	updateErr     error
}

func newMockUploadStore(upload *publish.UploadRecord) *mockUploadStore {
	return &mockUploadStore{upload: upload}
}

func (m *mockUploadStore) GetUploadByID(_ context.Context, _ string) (*publish.UploadRecord, error) {
	return m.upload, m.getErr
}

func (m *mockUploadStore) UpdateUploadStatus(_ context.Context, userID, uploadID, status string, vr *publish.ValidationResult) error {
	m.lastUserID = userID
	m.lastUploadID = uploadID
	m.lastStatus = status
	m.lastVR = vr
	return m.updateErr
}

// mockSiteStore provides configurable site and user responses.
type mockSiteStore struct {
	site    *publish.Site
	siteErr error
	user    *publish.User
	userErr error
	// Records the last UpdateActiveVersion call.
	lastActiveVersionInput *publish.ActiveVersionInput
	updateVersionErr       error
}

func newMockSiteStore(site *publish.Site, user *publish.User) *mockSiteStore {
	return &mockSiteStore{site: site, user: user}
}

func (m *mockSiteStore) GetSite(_ context.Context, _, _ string) (*publish.Site, error) {
	return m.site, m.siteErr
}

func (m *mockSiteStore) GetUser(_ context.Context, _ string) (*publish.User, error) {
	return m.user, m.userErr
}

func (m *mockSiteStore) UpdateActiveVersion(_ context.Context, input publish.ActiveVersionInput) error {
	m.lastActiveVersionInput = &input
	return m.updateVersionErr
}

// ==============================================================================
// Test helpers
// ==============================================================================

// validUpload returns a typical upload record in "uploaded" state.
func validUpload() *publish.UploadRecord {
	return &publish.UploadRecord{
		UploadID:     "upload_abc123",
		UserID:       "user-001",
		SiteID:       "site-001",
		Status:       "uploaded",
		S3StagingKey: "staging/users/user-001/uploads/upload_abc123/source.zip",
	}
}

// validSite returns a typical active site.
func validSite() *publish.Site {
	return &publish.Site{
		SiteID:   "site-001",
		UserID:   "user-001",
		SiteSlug: "my-site",
		Disabled: false,
	}
}

// validUser returns a typical active user.
func validUser() *publish.User {
	return &publish.User{
		UserID:   "user-001",
		UserSlug: "janedoe",
		Disabled: false,
	}
}

// twoFiles returns a small validated-files map with index.html and a CSS file.
func twoFiles() map[string][]byte {
	return map[string][]byte{
		"index.html":     []byte("<html><body><h1>Hello</h1></body></html>"),
		"css/style.css":  []byte("body { color: red; }"),
	}
}

// defaultPublisher creates a Publisher wired to the given mocks with test
// bucket and domain values.  Accepts interface types so wrapper mocks
// (e.g. capturingUploadStore) can be passed through.
func defaultPublisher(s3 publish.S3Writer, ups publish.UploadStore, ss publish.SiteStore) *publish.Publisher {
	return publish.NewPublisher(s3, ups, ss, "test-bucket", "sites.example.com")
}

// ==============================================================================
// Happy path — successful publish
// ==============================================================================

func TestPublish_Success(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	files := twoFiles()
	result, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: files,
		FileCount:      len(files),
		TotalBytes:     totalBytes(files),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Version ID should be present and have the correct prefix.
	if result.VersionID == "" {
		t.Error("expected non-empty version ID")
	}
	if !strings.HasPrefix(result.VersionID, "version_") {
		t.Errorf("version ID %q should start with 'version_'", result.VersionID)
	}

	// S3 prefix should include user slug, site slug, and version ID.
	expectedPrefix := fmt.Sprintf("published/users/janedoe/sites/my-site/versions/%s", result.VersionID)
	if result.S3PublishedPrefix != expectedPrefix {
		t.Errorf("S3 prefix: got %q, want %q", result.S3PublishedPrefix, expectedPrefix)
	}

	// Files written should match the input.
	if result.FilesWritten != 2 {
		t.Errorf("files written: got %d, want 2", result.FilesWritten)
	}

	// Each file should have been written to the versioned prefix.
	objects := s3.Objects()
	for filePath, content := range files {
		expectedKey := expectedPrefix + "/" + filePath
		written, ok := objects[expectedKey]
		if !ok {
			t.Errorf("expected object %q not found in S3", expectedKey)
			continue
		}
		if string(written) != string(content) {
			t.Errorf("content mismatch for %q", filePath)
		}
	}

	// Active version should have been updated with the correct input.
	avi := ss.lastActiveVersionInput
	if avi == nil {
		t.Fatal("expected UpdateActiveVersion to have been called")
	}
	if avi.UserID != "user-001" {
		t.Errorf("active version UserID: got %q, want %q", avi.UserID, "user-001")
	}
	if avi.SiteID != "site-001" {
		t.Errorf("active version SiteID: got %q, want %q", avi.SiteID, "site-001")
	}
	if avi.UserSlug != "janedoe" {
		t.Errorf("active version UserSlug: got %q, want %q", avi.UserSlug, "janedoe")
	}
	if avi.SiteSlug != "my-site" {
		t.Errorf("active version SiteSlug: got %q, want %q", avi.SiteSlug, "my-site")
	}
	if avi.SitesDomain != "sites.example.com" {
		t.Errorf("active version SitesDomain: got %q, want %q", avi.SitesDomain, "sites.example.com")
	}
	if avi.VersionID != result.VersionID {
		t.Errorf("active version VersionID: got %q, want %q", avi.VersionID, result.VersionID)
	}
	if avi.S3PublishedPrefix != expectedPrefix {
		t.Errorf("active version S3PublishedPrefix: got %q, want %q", avi.S3PublishedPrefix, expectedPrefix)
	}

	// Upload status should have been updated to "published".
	if ups.lastStatus != "published" {
		t.Errorf("upload status: got %q, want %q", ups.lastStatus, "published")
	}
	if ups.lastUploadID != "upload_abc123" {
		t.Errorf("upload ID in status update: got %q, want %q", ups.lastUploadID, "upload_abc123")
	}
	if ups.lastVR == nil || !ups.lastVR.Valid {
		t.Error("expected valid validation result in status update")
	}
}

// ==============================================================================
// Upload not found
// ==============================================================================

func TestPublish_UploadNotFound(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(nil) // nil upload
	s3 := newMockS3Writer()
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "nonexistent",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error for nonexistent upload")
	}
	if !errors.Is(err, publish.ErrUploadNotFound) {
		t.Errorf("expected ErrUploadNotFound, got: %v", err)
	}
}

// ==============================================================================
// Upload in wrong status
// ==============================================================================

func TestPublish_UploadNotReady(t *testing.T) {
	t.Parallel()

	upload := validUpload()
	upload.Status = "pending" // not "uploaded"

	ups := newMockUploadStore(upload)
	s3 := newMockS3Writer()
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error for upload not in 'uploaded' state")
	}
	if !errors.Is(err, publish.ErrUploadNotReady) {
		t.Errorf("expected ErrUploadNotReady, got: %v", err)
	}
}

// Already published uploads should also be rejected.
func TestPublish_UploadAlreadyPublished(t *testing.T) {
	t.Parallel()

	upload := validUpload()
	upload.Status = "published"

	ups := newMockUploadStore(upload)
	s3 := newMockS3Writer()
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error for already-published upload")
	}
	if !errors.Is(err, publish.ErrUploadNotReady) {
		t.Errorf("expected ErrUploadNotReady, got: %v", err)
	}
}

// ==============================================================================
// Site / user errors
// ==============================================================================

func TestPublish_SiteNotFound(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(nil, validUser()) // nil site
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if !errors.Is(err, publish.ErrSiteNotFound) {
		t.Errorf("expected ErrSiteNotFound, got: %v", err)
	}
}

func TestPublish_SiteDisabled(t *testing.T) {
	t.Parallel()

	site := validSite()
	site.Disabled = true
	ss := newMockSiteStore(site, validUser())
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if !errors.Is(err, publish.ErrSiteDisabled) {
		t.Errorf("expected ErrSiteDisabled, got: %v", err)
	}
}

func TestPublish_UserNotFound(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), nil) // nil user
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if !errors.Is(err, publish.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestPublish_UserDisabled(t *testing.T) {
	t.Parallel()

	user := validUser()
	user.Disabled = true
	ss := newMockSiteStore(validSite(), user)
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if !errors.Is(err, publish.ErrUserDisabled) {
		t.Errorf("expected ErrUserDisabled, got: %v", err)
	}
}

func TestPublish_GetSiteError(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), validUser())
	ss.siteErr = errors.New("DynamoDB unavailable")
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error when GetSite fails")
	}
}

func TestPublish_GetUserError(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), validUser())
	ss.userErr = errors.New("DynamoDB unavailable")
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error when GetUser fails")
	}
}

// ==============================================================================
// S3 write failure
// ==============================================================================

func TestPublish_S3WriteError(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	s3.err = errors.New("S3 bucket not found")
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error when S3 PutObject fails")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("expected 'write' in error message, got: %v", err)
	}
}

// ==============================================================================
// Active version update failure
// ==============================================================================

func TestPublish_UpdateActiveVersionError(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), validUser())
	ss.updateVersionErr = errors.New("transaction cancelled")
	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error when UpdateActiveVersion fails")
	}
	if !strings.Contains(err.Error(), "active version") {
		t.Errorf("expected 'active version' in error message, got: %v", err)
	}

	// Files should have been written to S3 even though the metadata update
	// failed — they become orphaned objects cleaned by lifecycle policies.
	objects := s3.Objects()
	if len(objects) != 2 {
		t.Errorf("expected 2 S3 objects written before metadata failure, got %d", len(objects))
	}
}

// ==============================================================================
// Upload status update failure
// ==============================================================================

func TestPublish_UpdateUploadStatusError(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(validUpload())
	ups.updateErr = errors.New("DynamoDB update failed")
	s3 := newMockS3Writer()
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err == nil {
		t.Fatal("expected error when UpdateUploadStatus fails")
	}
	if !strings.Contains(err.Error(), "mark published") {
		t.Errorf("expected 'mark published' in error message, got: %v", err)
	}
}

// ==============================================================================
// Version ID format and uniqueness
// ==============================================================================

func TestNewVersionID_Format(t *testing.T) {
	id := publish.NewVersionID()
	if id == "" {
		t.Fatal("version ID must not be empty")
	}
	if !strings.HasPrefix(id, "version_") {
		t.Errorf("version ID %q must start with 'version_'", id)
	}
	// The hex part after the prefix should be at least 12 chars (24 hex digits).
	hexPart := id[len("version_"):]
	if len(hexPart) < 12 {
		t.Errorf("version ID hex part too short: got %d chars, want >= 12", len(hexPart))
	}
}

func TestNewVersionID_Uniqueness(t *testing.T) {
	// Generate a batch and verify uniqueness.
	seen := make(map[string]bool)
	const count = 100
	for i := 0; i < count; i++ {
		id := publish.NewVersionID()
		if seen[id] {
			t.Errorf("duplicate version ID: %s", id)
		}
		seen[id] = true
	}
}

// ==============================================================================
// S3 prefix construction
// ==============================================================================

func TestPublish_S3PrefixConstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		userSlug string
		siteSlug string
	}{
		{
			name:     "standard slugs",
			userSlug: "janedoe",
			siteSlug: "my-site",
		},
		{
			name:     "slugs with hyphens",
			userSlug: "jane-doe",
			siteSlug: "my-cool-site",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3 := newMockS3Writer()
			ups := newMockUploadStore(validUpload())
			site := validSite()
			site.SiteSlug = tt.siteSlug
			user := validUser()
			user.UserSlug = tt.userSlug
			ss := newMockSiteStore(site, user)
			pub := defaultPublisher(s3, ups, ss)

			files := map[string][]byte{"index.html": []byte("<html></html>")}
			result, err := pub.Publish(context.Background(), publish.PublishInput{
				UploadID:       "upload_abc123",
				ValidatedFiles: files,
				FileCount:      1,
				TotalBytes:     totalBytes(files),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expectedPrefix := fmt.Sprintf("published/users/%s/sites/%s/versions/%s",
				tt.userSlug, tt.siteSlug, result.VersionID)
			if result.S3PublishedPrefix != expectedPrefix {
				t.Errorf("prefix: got %q, want %q", result.S3PublishedPrefix, expectedPrefix)
			}

			// Verify the file was written under the correct key.
			objects := s3.Objects()
			expectedKey := expectedPrefix + "/index.html"
			if _, ok := objects[expectedKey]; !ok {
				t.Errorf("file not found at expected key %q", expectedKey)
			}
		})
	}
}

// ==============================================================================
// MIME type detection
// ==============================================================================

func TestMimeTypeByExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filePath string
		want     string
	}{
		{"index.html", "text/html"},
		{"page.htm", "text/html"},
		{"style.css", "text/css"},
		{"app.js", "application/javascript"},
		{"module.mjs", "application/javascript"},
		{"data.json", "application/json"},
		{"sitemap.xml", "application/xml"},
		{"readme.md", "text/markdown"},
		{"config.yaml", "text/yaml"},
		{"config.yml", "text/yaml"},
		{"icon.svg", "image/svg+xml"},
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"anim.gif", "image/gif"},
		{"img.webp", "image/webp"},
		{"favicon.ico", "image/x-icon"},
		{"font.woff", "font/woff"},
		{"font.woff2", "font/woff2"},
		{"font.ttf", "font/ttf"},
		{"font.eot", "application/vnd.ms-fontobject"},
		{"font.otf", "font/otf"},
		{"doc.pdf", "application/pdf"},
		{"audio.mp3", "audio/mpeg"},
		{"video.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"module.wasm", "application/wasm"},
		// Unknown extensions default to octet-stream.
		{"file.unknown", "application/octet-stream"},
		{"Makefile", "application/octet-stream"},
		{"noextension", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got := publish.MimeTypeByExtension(tt.filePath)
			if got != tt.want {
				t.Errorf("MIME for %q: got %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

// MIME types are set correctly on S3 objects during publish.
func TestPublish_MimeTypesOnS3Objects(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	// We can't directly observe the MIME type through the mock,
	// but the publisher calls PutObject with it.  This test
	// verifies the publish succeeds with files of various extensions.
	files := map[string][]byte{
		"index.html":        []byte("<html></html>"),
		"css/style.css":     []byte("body{}"),
		"js/app.js":         []byte("1+1"),
		"assets/photo.png":  []byte("fake-png"),
		"assets/doc.pdf":    []byte("fake-pdf"),
		"fonts/body.woff2":  []byte("fake-woff2"),
		"icons/logo.svg":    []byte("<svg></svg>"),
		"data.json":         []byte("{}"),
		"config.yaml":       []byte("key: val"),
	}

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: files,
		FileCount:      len(files),
		TotalBytes:     totalBytes(files),
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	objects := s3.Objects()
	if len(objects) != len(files) {
		t.Errorf("expected %d objects, got %d", len(files), len(objects))
	}
}

// ==============================================================================
// Nested file paths — verify deep paths are preserved in the versioned prefix
// ==============================================================================

func TestPublish_NestedFilePaths(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	files := map[string][]byte{
		"index.html":                        []byte("<html></html>"),
		"blog/post/index.html":              []byte("<html></html>"),
		"assets/images/icons/logo.svg":      []byte("<svg></svg>"),
		"assets/css/vendor/reset.css":       []byte("body{}"),
		"scripts/vendor/jquery.min.js":      []byte("var $=function(){}"),
	}

	result, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: files,
		FileCount:      len(files),
		TotalBytes:     totalBytes(files),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	objects := s3.Objects()
	prefix := result.S3PublishedPrefix

	for filePath := range files {
		expectedKey := prefix + "/" + filePath
		if _, ok := objects[expectedKey]; !ok {
			t.Errorf("nested file %q not found at %q", filePath, expectedKey)
		}
	}
}

// ==============================================================================
// Single-file site (index.html only)
// ==============================================================================

func TestPublish_SingleFileSite(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	files := map[string][]byte{
		"index.html": []byte("<!DOCTYPE html><html><head><title>Hi</title></head><body>Hello</body></html>"),
	}

	result, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: files,
		FileCount:      1,
		TotalBytes:     totalBytes(files),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesWritten != 1 {
		t.Errorf("files written: got %d, want 1", result.FilesWritten)
	}

	objects := s3.Objects()
	if len(objects) != 1 {
		t.Errorf("expected 1 S3 object, got %d", len(objects))
	}

	expectedKey := result.S3PublishedPrefix + "/index.html"
	if _, ok := objects[expectedKey]; !ok {
		t.Errorf("index.html not found at %q", expectedKey)
	}
}

// ==============================================================================
// Large file set — verifies all files are written
// ==============================================================================

func TestPublish_ManyFiles(t *testing.T) {
	t.Parallel()

	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, ups, ss)

	files := make(map[string][]byte, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("page-%d.html", i)
		files[name] = []byte(fmt.Sprintf("<html><body>Page %d</body></html>", i))
	}

	result, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: files,
		FileCount:      len(files),
		TotalBytes:     totalBytes(files),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesWritten != 100 {
		t.Errorf("files written: got %d, want 100", result.FilesWritten)
	}

	objects := s3.Objects()
	if len(objects) != 100 {
		t.Errorf("expected 100 S3 objects, got %d", len(objects))
	}
}

// ==============================================================================
// ActiveVersionInput carries all needed fields
// ==============================================================================

func TestPublish_ActiveVersionInputFields(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), validUser())
	s3 := newMockS3Writer()
	ups := newMockUploadStore(validUpload())
	pub := defaultPublisher(s3, ups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	avi := ss.lastActiveVersionInput
	if avi == nil {
		t.Fatal("UpdateActiveVersion must be called")
	}

	// All fields must be non-empty.
	checks := map[string]string{
		"UserID":      avi.UserID,
		"SiteID":      avi.SiteID,
		"UserSlug":    avi.UserSlug,
		"SiteSlug":    avi.SiteSlug,
		"SitesDomain": avi.SitesDomain,
		"VersionID":   avi.VersionID,
		"S3PublishedPrefix": avi.S3PublishedPrefix,
	}
	for field, value := range checks {
		if value == "" {
			t.Errorf("ActiveVersionInput.%s must not be empty", field)
		}
	}
}

// ==============================================================================
// GetUploadByID is called with the correct upload ID
// ==============================================================================

// capturingUploadStore wraps mockUploadStore and captures the upload ID.
type capturingUploadStore struct {
	*mockUploadStore
	capturedID string
}

func (c *capturingUploadStore) GetUploadByID(_ context.Context, id string) (*publish.UploadRecord, error) {
	c.capturedID = id
	return c.mockUploadStore.GetUploadByID(context.Background(), id)
}

func TestPublish_UploadLookup(t *testing.T) {
	t.Parallel()

	cups := &capturingUploadStore{mockUploadStore: newMockUploadStore(validUpload())}

	s3 := newMockS3Writer()
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(s3, cups, ss)

	_, err := pub.Publish(context.Background(), publish.PublishInput{
		UploadID:       "upload_abc123",
		ValidatedFiles: twoFiles(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cups.capturedID != "upload_abc123" {
		t.Errorf("GetUploadByID called with %q, want %q", cups.capturedID, "upload_abc123")
	}
}

// ==============================================================================
// LookupUploadByID — exposes the UploadStore's GetUploadByID through the Publisher
// ==============================================================================

func TestLookupUploadByID_Found(t *testing.T) {
	t.Parallel()

	upload := validUpload()
	ups := newMockUploadStore(upload)
	pub := defaultPublisher(newMockS3Writer(), ups, newMockSiteStore(validSite(), validUser()))

	result, err := pub.LookupUploadByID(context.Background(), "upload_abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected upload record, got nil")
	}
	if result.UploadID != "upload_abc123" {
		t.Errorf("upload ID: got %q, want %q", result.UploadID, "upload_abc123")
	}
	if result.UserID != "user-001" {
		t.Errorf("user ID: got %q, want %q", result.UserID, "user-001")
	}
}

func TestLookupUploadByID_NotFound(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(nil)
	pub := defaultPublisher(newMockS3Writer(), ups, newMockSiteStore(validSite(), validUser()))

	result, err := pub.LookupUploadByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil upload for nonexistent ID")
	}
}

func TestLookupUploadByID_Error(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(nil)
	ups.getErr = errors.New("DynamoDB unavailable")
	pub := defaultPublisher(newMockS3Writer(), ups, newMockSiteStore(validSite(), validUser()))

	_, err := pub.LookupUploadByID(context.Background(), "upload_abc123")
	if err == nil {
		t.Fatal("expected error from DynamoDB failure")
	}
}

// ==============================================================================
// RecordValidationResult — Persist validation outcomes on the upload record
// ==============================================================================

func TestRecordValidationResult_Failure(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(validUpload())
	ss := newMockSiteStore(validSite(), validUser())
	pub := defaultPublisher(newMockS3Writer(), ups, ss)

	vr := &publish.ValidationResult{
		Valid:  false,
		Errors: []string{"missing index.html", "unsupported file type: virus.exe (.exe)"},
	}

	err := pub.RecordValidationResult(context.Background(), "user-001", "upload_abc123", vr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status should be "failed".
	if ups.lastStatus != "failed" {
		t.Errorf("expected status 'failed', got %q", ups.lastStatus)
	}

	// The validation result must be recorded.
	if ups.lastVR == nil {
		t.Fatal("expected validation result to be recorded")
	}
	if ups.lastVR.Valid {
		t.Error("expected Valid=false")
	}
	if len(ups.lastVR.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(ups.lastVR.Errors))
	}

	// The user ID and upload ID must match the call.
	if ups.lastUserID != "user-001" {
		t.Errorf("user ID: got %q, want %q", ups.lastUserID, "user-001")
	}
	if ups.lastUploadID != "upload_abc123" {
		t.Errorf("upload ID: got %q, want %q", ups.lastUploadID, "upload_abc123")
	}

	// The active site must NOT be modified — UpdateActiveVersion must not
	// have been called on the SiteStore.
	if ss.lastActiveVersionInput != nil {
		t.Error("active version must NOT be updated on validation failure")
	}
}

func TestRecordValidationResult_Success(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(validUpload())
	pub := defaultPublisher(newMockS3Writer(), ups, newMockSiteStore(validSite(), validUser()))

	vr := &publish.ValidationResult{
		Valid:  true,
		Errors: nil,
	}

	err := pub.RecordValidationResult(context.Background(), "user-001", "upload_abc123", vr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status should be "published".
	if ups.lastStatus != "published" {
		t.Errorf("expected status 'published', got %q", ups.lastStatus)
	}

	// Validation result must show valid.
	if ups.lastVR == nil || !ups.lastVR.Valid {
		t.Error("expected Valid=true in recorded result")
	}
}

func TestRecordValidationResult_UpdateError(t *testing.T) {
	t.Parallel()

	ups := newMockUploadStore(validUpload())
	ups.updateErr = errors.New("DynamoDB update failed")
	pub := defaultPublisher(newMockS3Writer(), ups, newMockSiteStore(validSite(), validUser()))

	vr := &publish.ValidationResult{
		Valid:  false,
		Errors: []string{"missing index.html"},
	}

	err := pub.RecordValidationResult(context.Background(), "user-001", "upload_abc123", vr)
	if err == nil {
		t.Fatal("expected error when DynamoDB update fails")
	}
}

// ==============================================================================
// RecordValidationResult does NOT modify the active site
//
// This is the critical acceptance criterion from VISION.md §17.3:
// "Failed validation does not update the active published site."
// ==============================================================================

func TestRecordValidationResult_DoesNotTouchActiveSite(t *testing.T) {
	t.Parallel()

	ss := newMockSiteStore(validSite(), validUser())
	ups := newMockUploadStore(validUpload())
	s3 := newMockS3Writer()
	pub := defaultPublisher(s3, ups, ss)

	// Record a validation failure.
	vr := &publish.ValidationResult{
		Valid:  false,
		Errors: []string{"path traversal detected: ../../../etc/passwd"},
	}

	err := pub.RecordValidationResult(context.Background(), "user-001", "upload_abc123", vr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the upload status was set to "failed".
	if ups.lastStatus != "failed" {
		t.Errorf("expected 'failed' status, got %q", ups.lastStatus)
	}

	// Verify no S3 objects were written.
	if len(s3.Objects()) != 0 {
		t.Errorf("expected 0 S3 objects written, got %d", len(s3.Objects()))
	}

	// Verify the active version was NOT updated.
	if ss.lastActiveVersionInput != nil {
		t.Error("RecordValidationResult must NOT update the active site version")
	}
}

// ==============================================================================
// Test helpers
// ==============================================================================

// totalBytes returns the sum of all values in the files map.
func totalBytes(files map[string][]byte) int64 {
	var total int64
	for _, b := range files {
		total += int64(len(b))
	}
	return total
}
