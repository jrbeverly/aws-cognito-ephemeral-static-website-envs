// ==============================================================================
// validate.go — Upload validation logic
//
// Implements the content validation requirements from VISION.md §8.5:
//   - Verify upload ownership against the authenticated user
//   - Safely extract zip files; prevent path traversal; reject absolute paths
//   - Reject symlinks
//   - Enforce maximum compressed size, uncompressed size, and file count
//   - Require index.html after normalization
//   - Handle single-file and pasted-HTML through the same flow
//   - Reject dangerous file extensions
//
// Each failure mode from VISION.md §5.4 produces a clear, actionable error
// message.  The validation result is designed to be stored on the upload
// record's ValidationResult field (metadata.types.go).
//
// VISION.md §5.2 — Upload modes; §5.4 — Failure messages; §8.5 — Worker.
// ==============================================================================

package validate

import (
	"fmt"
	"strings"
)

// ==============================================================================
// Configuration
// ==============================================================================

// Config holds the thresholds and extension policies for validation.
// The zero value is NOT safe — use DefaultConfig() to get production defaults.
type Config struct {
	// Size limits
	MaxZipCompressedBytes   int64 // Max compressed zip size (100 MiB default)
	MaxSingleHTMLBytes      int64 // Max single-HTML / pasted payload size (5 MiB)
	MaxUncompressedBytes    int64 // Max total uncompressed content from a zip
	MaxEntryBytes           int64 // Max uncompressed size of a single zip entry
	MaxFileCount            int   // Max number of files in a zip archive

	// Blocked file extensions (lowercase, with leading dot)
	// These are extensions that are never acceptable in a static site.
	BlockedExtensions map[string]bool
}

// DefaultConfig returns the standard production validation configuration.
// These values are aligned with the backend handler constants
// (backend/go-api/internal/handler/uploads.go).
func DefaultConfig() Config {
	return Config{
		MaxZipCompressedBytes: 100 * 1024 * 1024, // 100 MiB
		MaxSingleHTMLBytes:    5 * 1024 * 1024,   //   5 MiB
		MaxUncompressedBytes:  150 * 1024 * 1024, // 150 MiB
		MaxEntryBytes:         50 * 1024 * 1024,  //  50 MiB per file
		MaxFileCount:          1000,

		BlockedExtensions: map[string]bool{
			// Executables
			".exe":   true,
			".dll":   true,
			".so":    true,
			".dylib": true,
			".bin":   true,
			".com":   true,
			".msi":   true,
			".bat":   true,
			".cmd":   true,
			".ps1":   true,
			".vbs":   true,
			".wsf":   true,

			// Server-side scripts — must never be executed by the static gateway
			".php":  true,
			".phtml": true,
			".php3": true,
			".php4": true,
			".php5": true,
			".jsp":  true,
			".asp":  true,
			".aspx": true,
			".cgi":  true,
			".fcgi": true,
			".pl":   true,
			".py":   true,
			".rb":   true,

			// Shell scripts
			".sh":   true,
			".bash": true,
			".zsh":  true,
			".fish": true,
			".ksh":  true,

			// Configuration files that might leak information
			".env":    true,
			".htaccess": true,
			".htpasswd": true,
		},
	}
}

// ==============================================================================
// Input — What the caller provides for validation
// ==============================================================================

// Input describes the upload to validate.  The caller populates all fields
// from the upload record and staged content before calling Validate.
type Input struct {
	// Upload type — "zip", "index", or "paste" (aligns with handler.UploadType).
	UploadType string

	// Raw staged content bytes (the entire .zip, .html, or pasted HTML).
	Content []byte

	// Compressed size of the upload as stored on S3.
	// For zip this is the compressed archive size; for single/paste this
	// equals len(Content).
	CompressedSizeBytes int64
}

// ==============================================================================
// Result — What validation produces
// ==============================================================================

// Result holds the outcome of content validation together with the validated
// file map that the publisher will write to the versioned S3 prefix.
//
// The Valid and Errors fields match the shape of metadata.ValidationResult
// so they can be stored directly on the upload record.
type Result struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`

	// ValidatedFiles maps normalized relative path → file content.
	// Only populated when Valid is true.  The caller uses this to publish
	// files into the immutable versioned prefix.
	ValidatedFiles map[string][]byte `json:"-"`
}

// ==============================================================================
// Validate — Main entry point
//
// Routes to the appropriate validator based on UploadType:
//   - "zip"   → ValidateZip
//   - "index" → ValidateSingleHTML
//   - "paste" → ValidateSingleHTML (same flow as single file)
//
// Returns a Result where Valid is true only when all checks pass.
// ==============================================================================

func Validate(input Input, cfg Config) *Result {
	switch input.UploadType {
	case "zip":
		return ValidateZip(input, cfg)
	case "index", "paste":
		return ValidateSingleHTML(input, cfg)
	default:
		return &Result{
			Valid:  false,
			Errors: []string{fmt.Sprintf("unknown upload type: %q", input.UploadType)},
		}
	}
}

// ==============================================================================
// ValidateZip — Full zip archive validation
//
// Checks (in order):
//   1. Compressed size does not exceed the limit.
//   2. The zip is valid and can be opened.
//   3. Every entry passes the safety checks in extractZip.
//   4. File count is within bounds.
//   5. Total uncompressed size is within bounds.
//   6. No blocked file extensions.
//   7. An index.html exists at the virtual root after normalization.
//
// VISION.md §5.4 failure messages produced:
//   - "zip could not be extracted"
//   - "path traversal detected"
//   - "file count exceeds allowed limit"
//   - "total uncompressed size exceeds allowed limit"
//   - "unsupported file type"
//   - "missing index.html"
// ==============================================================================

func ValidateZip(input Input, cfg Config) *Result {
	// 1. Compressed size check.
	if int64(len(input.Content)) > cfg.MaxZipCompressedBytes {
		return &Result{
			Valid: false,
			Errors: []string{
				fmt.Sprintf("compressed zip size %d bytes exceeds maximum %d bytes",
					len(input.Content), cfg.MaxZipCompressedBytes),
			},
		}
	}

	// 2–6. Safe extraction with all entry-level checks.
	files, _, err := extractZip(input.Content, cfg.BlockedExtensions, extractionConfig{
		maxUncompressedBytes: cfg.MaxUncompressedBytes,
		maxEntryBytes:        cfg.MaxEntryBytes,
		maxFileCount:         cfg.MaxFileCount,
	})
	if err != nil {
		return &Result{
			Valid:  false,
			Errors: []string{err.Error()},
		}
	}

	// 7. Require index.html after normalization.
	if _, ok := files["index.html"]; !ok {
		return &Result{
			Valid:  false,
			Errors: []string{"missing index.html"},
		}
	}

	return &Result{
		Valid:          true,
		ValidatedFiles: files,
	}
}

// ==============================================================================
// ValidateSingleHTML — Single-file or pasted-HTML validation
//
// Checks:
//   1. Size does not exceed the single-file limit.
//   2. Content is not empty.
//   3. Content starts with a reasonable HTML-like token (best-effort
//      HTML detection — we do not attempt full HTML parsing).
//
// On success, the ValidatedFiles map contains a single entry:
//   "index.html" → content
//
// The single-file and paste uploads are treated identically — both produce
// exactly one index.html at the site root (VISION.md §5.2).
// ==============================================================================

func ValidateSingleHTML(input Input, cfg Config) *Result {
	// 1. Size check.
	if int64(len(input.Content)) > cfg.MaxSingleHTMLBytes {
		return &Result{
			Valid: false,
			Errors: []string{
				fmt.Sprintf("HTML content size %d bytes exceeds maximum %d bytes",
					len(input.Content), cfg.MaxSingleHTMLBytes),
			},
		}
	}

	// 2. Non-empty check.
	if len(input.Content) == 0 {
		return &Result{
			Valid:  false,
			Errors: []string{"HTML content is empty"},
		}
	}

	// 3. Best-effort HTML detection — look for an HTML-like opening after
	//    trimming whitespace.  We accept: <!DOCTYPE, <html, <head, <body,
	//    <title, <meta, <div, <p, <h1-h6, <a, etc.
	trimmed := strings.TrimSpace(string(input.Content))
	if len(trimmed) == 0 {
		return &Result{
			Valid:  false,
			Errors: []string{"HTML content is whitespace only"},
		}
	}

	// The content must at least start with '<' to be plausible HTML.
	if trimmed[0] != '<' {
		return &Result{
			Valid: false,
			Errors: []string{
				"content does not appear to be HTML (must start with '<')",
			},
		}
	}

	// Also check that after the opening '<' there is a plausible tag character.
	if len(trimmed) < 2 || !isTagStartChar(trimmed[1]) {
		return &Result{
			Valid: false,
			Errors: []string{
				"content does not appear to be HTML (unexpected character after '<')",
			},
		}
	}

	return &Result{
		Valid: true,
		ValidatedFiles: map[string][]byte{
			"index.html": input.Content,
		},
	}
}

// ==============================================================================
// Helpers
// ==============================================================================

// isTagStartChar returns true when c is a valid first character of an HTML
// tag name: a letter (a-z, A-Z), '!' (for DOCTYPE and comments), or '/' (for
// closing tags).
func isTagStartChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		c == '!' ||
		c == '/'
}
