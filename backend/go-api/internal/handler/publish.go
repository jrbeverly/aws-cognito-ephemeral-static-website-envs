// ==============================================================================
// publish.go — Synchronous publish from staging to the served prefix
//
// There is no separate worker: once an upload's content is staged, the
// backend validates it, copies it to an immutable versioned prefix
//
//	published/users/{userId}/sites/{siteId}/{versionId}/
//
// flips the site and host mappings to that version (the gateway resolves
// Host → s3PublishedPrefix), and registers the site's ALB callback URL on
// the Cognito app client so the ALB login works on the site hostname.
// ==============================================================================

package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path"
	"strings"
	"time"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/metadata"
)

// CallbackRegistrar adds a URL to the ALB Cognito app client's callback URLs.
// Cognito has no wildcard callbacks, so each site hostname is added explicitly.
type CallbackRegistrar interface {
	AddCallbackURL(ctx context.Context, url string) error
}

const maxUncompressedBytes = 200 * 1024 * 1024

func init() {
	// Go's built-in table lacks these; Lambda has no /etc/mime.types.
	for ext, typ := range map[string]string{
		".woff":        "font/woff",
		".woff2":       "font/woff2",
		".ttf":         "font/ttf",
		".otf":         "font/otf",
		".ico":         "image/x-icon",
		".txt":         "text/plain; charset=utf-8",
		".map":         "application/json",
		".webmanifest": "application/manifest+json",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

// publish reads the staged upload, publishes it, and records the final
// status (published, or failed with validation errors) on the upload.
func (h *UploadsHandler) publish(ctx context.Context, id *auth.Identity, upload metadata.Upload) (metadata.UploadStatus, error) {
	staged, err := h.contentStore.GetContent(ctx, h.bucket, upload.S3StagingKey)
	if err != nil {
		return "", fmt.Errorf("read staged content: %w", err)
	}

	var files map[string][]byte
	if uploadTypeFromKey(upload.S3StagingKey) == UploadTypeZip {
		files, err = extractSite(staged)
	} else if len(bytes.TrimSpace(staged)) == 0 {
		err = fmt.Errorf("index.html is empty")
	} else {
		files = map[string][]byte{"index.html": staged}
	}
	if err != nil {
		result := &metadata.ValidationResult{Valid: false, Errors: []string{err.Error()}}
		if uerr := h.uploadRepo.UpdateUploadStatus(ctx, id.UserID, upload.UploadID, metadata.UploadStatusFailed, result); uerr != nil {
			return "", fmt.Errorf("record validation failure: %w", uerr)
		}
		slog.Info("upload failed validation", "user_id", id.UserID, "upload_id", upload.UploadID, "error", err)
		h.emitter.EmitUploadEvent("failed", string(uploadTypeFromKey(upload.S3StagingKey)), err.Error())
		return metadata.UploadStatusFailed, nil
	}

	versionID := newVersionID()
	prefix := fmt.Sprintf("published/users/%s/sites/%s/%s/", id.UserID, upload.SiteID, versionID)
	for name, body := range files {
		ct := mime.TypeByExtension(path.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		if err := h.contentStore.PutContent(ctx, ContentStoreRequest{Bucket: h.bucket, Key: prefix + name, Body: body, ContentType: ct}); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	// UpdateActiveVersion builds hostnames from the stored user slug, and
	// nothing else creates the user record outside local seeding.
	user, err := h.siteRepo.GetUser(ctx, id.UserID)
	if err != nil {
		return "", fmt.Errorf("read user: %w", err)
	}
	if user == nil {
		user = &metadata.User{UserID: id.UserID, UserSlug: id.UserSlug, CognitoSub: id.UserID}
		if err := h.siteRepo.CreateUser(ctx, *user); err != nil {
			return "", fmt.Errorf("create user: %w", err)
		}
	}
	if err := h.siteRepo.UpdateActiveVersion(ctx, id.UserID, upload.SiteID, versionID, prefix, h.sitesDomain); err != nil {
		return "", fmt.Errorf("activate version: %w", err)
	}

	// Only the one-label alias host is covered by the *.sites wildcard cert.
	site, err := h.siteRepo.GetSite(ctx, id.UserID, upload.SiteID)
	if err != nil || site == nil {
		return "", fmt.Errorf("read site after publish: %v", err)
	}
	host := fmt.Sprintf("%s--%s.%s", site.SiteSlug, user.UserSlug, h.sitesDomain)
	if err := h.callbacks.AddCallbackURL(ctx, "https://"+host+"/oauth2/idpresponse"); err != nil {
		return "", fmt.Errorf("register callback for %s: %w", host, err)
	}

	result := &metadata.ValidationResult{Valid: true, Errors: []string{}}
	if err := h.uploadRepo.UpdateUploadStatus(ctx, id.UserID, upload.UploadID, metadata.UploadStatusPublished, result); err != nil {
		return "", fmt.Errorf("record published: %w", err)
	}
	slog.Info("upload published", "user_id", id.UserID, "upload_id", upload.UploadID, "site_id", upload.SiteID, "prefix", prefix, "files", len(files), "host", host)
	return metadata.UploadStatusPublished, nil
}

// extractSite unzips a site archive.  If index.html is not at the root but the
// archive has a single top-level directory (e.g. "public/"), that directory
// becomes the root.
func extractSite(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive")
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		clean := path.Clean("/" + name)[1:]
		if clean == "" || clean != strings.TrimPrefix(name, "./") || strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("archive contains an unsafe path: %q", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxUncompressedBytes {
			return nil, fmt.Errorf("archive expands beyond %d MiB", maxUncompressedBytes/1024/1024)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("read %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(io.LimitReader(rc, maxUncompressedBytes))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %v", f.Name, err)
		}
		files[clean] = body
	}

	if _, ok := files["index.html"]; !ok {
		roots := map[string]bool{}
		for name := range files {
			roots[strings.SplitN(name, "/", 2)[0]] = true
		}
		if len(roots) == 1 {
			for root := range roots {
				if _, ok := files[root+"/index.html"]; ok {
					stripped := map[string][]byte{}
					for name, body := range files {
						stripped[strings.TrimPrefix(name, root+"/")] = body
					}
					files = stripped
				}
			}
		}
	}
	if _, ok := files["index.html"]; !ok {
		return nil, fmt.Errorf("archive has no index.html at its root")
	}
	return files, nil
}

func newVersionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("v%s-%x", time.Now().UTC().Format("20060102T150405"), b)
}
