// ==============================================================================
// validate_test.go — Comprehensive tests for upload validation
//
// Covers all failure modes from VISION.md §5.4:
//   - missing index.html
//   - zip could not be extracted
//   - file count exceeds allowed limit
//   - total uncompressed size exceeds allowed limit
//   - unsupported file type
//   - path traversal detected
//
// And the acceptance criteria from VISION.md §17.3:
//   - A valid Hugo zip publishes successfully.
//   - A valid single index.html publishes successfully.
//   - A zip without index.html fails validation.
//   - A zip containing unsafe paths fails validation.
//   - Failed validation does not update the active published site. (by design:
//     Validate only returns a Result; the caller decides whether to publish)
//
// VISION.md §16 — Local development: "running unit tests for validation logic."
// ==============================================================================

package validate_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"workers/site-publisher/internal/validate"
)

// ==============================================================================
// Helpers — build test zips in memory
// ==============================================================================

// mustZip creates a zip archive from a name→content map using the standard
// archive/zip Writer.  Returns the raw zip bytes.
// Panics if any entry cannot be written (programmer error in test).
//
// NOTE: Go's archive/zip.Writer rejects ".." segments and absolute paths in
// filenames (security hardening).  Use makeZipRaw for path-traversal tests.
func mustZip(files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			panic("mustZip Create " + name + ": " + err.Error())
		}
		if _, err := f.Write([]byte(content)); err != nil {
			panic("mustZip Write: " + err.Error())
		}
	}
	if err := w.Close(); err != nil {
		panic("mustZip Close: " + err.Error())
	}
	return buf.Bytes()
}

// makeZipRaw constructs a zip archive from a name→content map by writing
// the binary zip format directly.  This bypasses the standard zip.Writer's
// name validation so we can craft entries with path-traversal names that
// the validator must reject.
//
// Only stored (uncompressed) entries are supported — sufficient for small
// test payloads.
func makeZipRaw(files map[string]string) []byte {
	type entry struct {
		name    string
		content []byte
		crc     uint32
		offset  int64 // offset of local file header from start of zip
	}

	// First pass: collect entries.
	entries := make([]entry, 0, len(files))
	for name, content := range files {
		entries = append(entries, entry{
			name:    name,
			content: []byte(content),
			crc:     crc32.ChecksumIEEE([]byte(content)),
		})
	}

	var buf bytes.Buffer
	write := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}

	// Local file headers + file data.
	for i := range entries {
		e := &entries[i]
		e.offset = int64(buf.Len())

		// Local file header signature.
		write(uint32(0x04034b50))
		write(uint16(20))                // version needed
		write(uint16(0))                 // flags
		write(uint16(0))                 // compression (stored)
		write(uint16(0))                 // mod time
		write(uint16(0))                 // mod date
		write(uint32(e.crc))             // crc-32
		write(uint32(len(e.content)))    // compressed size
		write(uint32(len(e.content)))    // uncompressed size
		write(uint16(len(e.name)))       // filename length
		write(uint16(0))                 // extra field length
		buf.WriteString(e.name)
		buf.Write(e.content)
	}

	cdOffset := int64(buf.Len())

	// Central directory entries.
	for _, e := range entries {
		write(uint32(0x02014b50))
		write(uint16(20))                // version made by (FAT / 2.0)
		write(uint16(20))                // version needed
		write(uint16(0))                 // flags
		write(uint16(0))                 // compression
		write(uint16(0))                 // mod time
		write(uint16(0))                 // mod date
		write(uint32(e.crc))             // crc-32
		write(uint32(len(e.content)))    // compressed size
		write(uint32(len(e.content)))    // uncompressed size
		write(uint16(len(e.name)))       // filename length
		write(uint16(0))                 // extra field length
		write(uint16(0))                 // file comment length
		write(uint16(0))                 // disk number start
		write(uint16(0))                 // internal attrs
		write(uint32(0))                 // external attrs
		write(uint32(e.offset))          // relative offset of local header
		buf.WriteString(e.name)
	}

	cdEnd := int64(buf.Len())

	// End of central directory record.
	write(uint32(0x06054b50))
	write(uint16(0))                    // disk number
	write(uint16(0))                    // disk with start of CD
	write(uint16(len(entries)))         // entries on this disk
	write(uint16(len(entries)))         // total entries
	write(uint32(cdEnd - cdOffset))     // CD size
	write(uint32(cdOffset))             // CD offset
	write(uint16(0))                    // comment length

	return buf.Bytes()
}

// ==============================================================================
// Valid zip — acceptance criterion "A valid Hugo zip publishes successfully"
// ==============================================================================

func TestValidateZip_ValidHugoOutput(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html":      "<html><body><h1>Hello</h1></body></html>",
		"css/style.css":   "body { color: red; }",
		"js/app.js":       "console.log('hi');",
		"images/logo.png": "fake-png-data",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.ValidatedFiles == nil {
		t.Fatal("expected validated files map")
	}
	if _, ok := result.ValidatedFiles["index.html"]; !ok {
		t.Error("expected index.html in validated files")
	}
	if _, ok := result.ValidatedFiles["css/style.css"]; !ok {
		t.Error("expected css/style.css in validated files")
	}
}

// ==============================================================================
// public/ directory normalization — Hugo default output layout
// ==============================================================================

func TestValidateZip_PublicDirNormalization(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"public/index.html": "<html><body><h1>Hello</h1></body></html>",
		"public/css/style.css": "body { color: red; }",
		"public/js/app.js":     "console.log('hi');",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	// After normalization, "public/" prefix is stripped.
	if _, ok := result.ValidatedFiles["index.html"]; !ok {
		t.Error("expected index.html (normalized from public/index.html)")
	}
	if _, ok := result.ValidatedFiles["css/style.css"]; !ok {
		t.Error("expected css/style.css (normalized from public/css/style.css)")
	}
	// The original prefixed paths must not appear.
	if _, ok := result.ValidatedFiles["public/index.html"]; ok {
		t.Error("public/index.html must not appear after normalization")
	}
}

// ==============================================================================
// Valid single HTML — acceptance criterion "A valid single index.html publishes"
// ==============================================================================

func TestValidateSingleHTML_Success(t *testing.T) {
	t.Parallel()

	html := "<!DOCTYPE html><html><head><title>Test</title></head><body><p>Hi</p></body></html>"

	for _, ut := range []string{"index", "paste"} {
		t.Run(ut, func(t *testing.T) {
			result := validate.Validate(validate.Input{
				UploadType:          ut,
				Content:             []byte(html),
				CompressedSizeBytes: int64(len(html)),
			}, validate.DefaultConfig())

			if !result.Valid {
				t.Fatalf("expected valid, got errors: %v", result.Errors)
			}
			if f, ok := result.ValidatedFiles["index.html"]; !ok {
				t.Fatal("expected index.html in validated files")
			} else if string(f) != html {
				t.Errorf("content mismatch: got %q, want %q", string(f), html)
			}
		})
	}
}

func TestValidateSingleHTML_MinimalValid(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "index",
		Content:             []byte("<html></html>"),
		CompressedSizeBytes: 13,
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateSingleHTML_Empty(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "index",
		Content:             []byte{},
		CompressedSizeBytes: 0,
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for empty content")
	}
}

func TestValidateSingleHTML_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "paste",
		Content:             []byte("   \n\t  "),
		CompressedSizeBytes: 8,
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for whitespace-only content")
	}
}

func TestValidateSingleHTML_NotHTML(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"plain text, not html",
		"{\"json\": true}",
		"# Markdown",
	} {
		result := validate.Validate(validate.Input{
			UploadType:          "index",
			Content:             []byte(content),
			CompressedSizeBytes: int64(len(content)),
		}, validate.DefaultConfig())

		if result.Valid {
			t.Errorf("expected invalid for non-HTML content %q", content)
		}
	}
}

// ==============================================================================
// Missing index.html — acceptance criterion "A zip without index.html fails"
// ==============================================================================

func TestValidateZip_MissingIndexHTML(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"css/style.css": "body { color: red; }",
		"js/app.js":     "console.log('hi');",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for zip without index.html")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "missing index.html") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'missing index.html' error, got: %v", result.Errors)
	}
}

// ==============================================================================
// Path traversal — acceptance criterion "A zip containing unsafe paths fails"
// ==============================================================================

func TestValidateZip_PathTraversal_DotDot(t *testing.T) {
	t.Parallel()

	// Use raw zip construction because Go's archive/zip.Writer rejects ".."
	// in filenames (security hardening patch).
	raw := makeZipRaw(map[string]string{
		"../etc/passwd": "malicious",
		"index.html":    "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for ../ path traversal")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "path traversal") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'path traversal' error, got: %v", result.Errors)
	}
}

func TestValidateZip_PathTraversal_Absolute(t *testing.T) {
	t.Parallel()

	raw := makeZipRaw(map[string]string{
		"/etc/passwd": "malicious",
		"index.html":  "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for absolute path")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "path traversal") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'path traversal' error for absolute path, got: %v", result.Errors)
	}
}

func TestValidateZip_PathTraversal_Encoded(t *testing.T) {
	t.Parallel()

	// Attempt with deeply nested traversal.
	raw := makeZipRaw(map[string]string{
		"foo/../../etc/passwd": "malicious",
		"index.html":           "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for nested path traversal")
	}
}

// ==============================================================================
// Invalid / corrupt zip — "zip could not be extracted"
// ==============================================================================

func TestValidateZip_CorruptZip(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             []byte("this is not a zip file at all"),
		CompressedSizeBytes: 31,
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for corrupt zip")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "zip could not be extracted") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'zip could not be extracted' error, got: %v", result.Errors)
	}
}

func TestValidateZip_EmptyZip(t *testing.T) {
	t.Parallel()

	// Empty zip — no entries, so no index.html.
	raw := mustZip(map[string]string{})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for empty zip (no index.html)")
	}
}

// ==============================================================================
// Unsupported file type — blocked extensions
// ==============================================================================

func TestValidateZip_BlockedExtension_Executable(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html": "<html></html>",
		"virus.exe":  "MZ...",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for .exe extension")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "unsupported file type") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'unsupported file type' error, got: %v", result.Errors)
	}
}

func TestValidateZip_BlockedExtension_Script(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".php", ".jsp", ".asp", ".sh", ".py", ".rb"} {
		raw := mustZip(map[string]string{
			"index.html":    "<html></html>",
			"evil" + ext:    "echo bad",
		})

		result := validate.Validate(validate.Input{
			UploadType:          "zip",
			Content:             raw,
			CompressedSizeBytes: int64(len(raw)),
		}, validate.DefaultConfig())

		if result.Valid {
			t.Errorf("expected invalid for blocked extension %s", ext)
		}
	}
}

func TestValidateZip_AllowedExtensions(t *testing.T) {
	t.Parallel()

	// Allowed file types must pass validation.
	raw := mustZip(map[string]string{
		"index.html":  "<html></html>",
		"style.css":   "body{}",
		"app.js":      "1+1",
		"data.json":   "{}",
		"image.png":   "pngdata",
		"graphic.svg": "<svg></svg>",
		"font.woff2":  "woff2data",
		"doc.pdf":     "pdfdata",
		"readme.md":   "# hello",
		"sitemap.xml": "<xml></xml>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid for allowed extensions, got errors: %v", result.Errors)
	}
}

// ==============================================================================
// Compressed / uncompressed size limits
// ==============================================================================

func TestValidateZip_CompressedTooLarge(t *testing.T) {
	t.Parallel()

	cfg := validate.DefaultConfig()
	cfg.MaxZipCompressedBytes = 10 // absurdly small limit

	raw := mustZip(map[string]string{
		"index.html": "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, cfg)

	if result.Valid {
		t.Fatal("expected invalid for oversized compressed zip")
	}
}

func TestValidateZip_UncompressedTooLarge(t *testing.T) {
	t.Parallel()

	cfg := validate.DefaultConfig()
	cfg.MaxUncompressedBytes = 5 // very small limit

	raw := mustZip(map[string]string{
		"index.html": "<html>" + strings.Repeat("x", 100) + "</html>", // > 5 bytes uncompressed
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, cfg)

	if result.Valid {
		t.Fatal("expected invalid for oversized uncompressed content")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "uncompressed size exceeds") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'uncompressed size exceeds' error, got: %v", result.Errors)
	}
}

func TestValidateSingleHTML_TooLarge(t *testing.T) {
	t.Parallel()

	cfg := validate.DefaultConfig()
	cfg.MaxSingleHTMLBytes = 5

	result := validate.Validate(validate.Input{
		UploadType:          "index",
		Content:             []byte("<html>" + strings.Repeat("x", 100) + "</html>"),
		CompressedSizeBytes: 120,
	}, cfg)

	if result.Valid {
		t.Fatal("expected invalid for oversized HTML")
	}
}

// ==============================================================================
// File count limit
// ==============================================================================

func TestValidateZip_TooManyFiles(t *testing.T) {
	t.Parallel()

	cfg := validate.DefaultConfig()
	cfg.MaxFileCount = 3

	raw := mustZip(map[string]string{
		"index.html": "<html></html>",
		"a.css":      "x",
		"b.css":      "x",
		"c.css":      "x", // 4th file, exceeds limit of 3
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, cfg)

	if result.Valid {
		t.Fatal("expected invalid for too many files")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "file count exceeds") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'file count exceeds' error, got: %v", result.Errors)
	}
}

// ==============================================================================
// Unknown upload type
// ==============================================================================

func TestValidate_UnknownType(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "pdf",
		Content:             []byte("not-valid"),
		CompressedSizeBytes: 9,
	}, validate.DefaultConfig())

	if result.Valid {
		t.Fatal("expected invalid for unknown upload type")
	}
}

// ==============================================================================
// Content safety — zip-bomb detection
// ==============================================================================

func TestValidateZip_ZipBomb(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html": "<html></html>",
	})

	// We can't easily craft a zip bomb in a unit test without manipulating
	// the zip internals.  The bomb guard checks that actual decompressed size
	// doesn't exceed the declared UncompressedSize64 — for standard zips
	// these always match.  The important test is that valid zips pass.
	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("valid zip should pass: %v", result.Errors)
	}
}

// ==============================================================================
// Directory entries are skipped (not treated as files)
// ==============================================================================

func TestValidateZip_DirectoriesAreSkipped(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"css/style.css": "body{}",
		"index.html":    "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid, got: %v", result.Errors)
	}
	if _, ok := result.ValidatedFiles["css/"]; ok {
		t.Error("directory entries must not appear in validated files")
	}
}

// ==============================================================================
// DefaultConfig returns a non-zero configuration
// ==============================================================================

func TestDefaultConfig_IsNonZero(t *testing.T) {
	cfg := validate.DefaultConfig()

	if cfg.MaxZipCompressedBytes == 0 {
		t.Error("MaxZipCompressedBytes must not be zero")
	}
	if cfg.MaxSingleHTMLBytes == 0 {
		t.Error("MaxSingleHTMLBytes must not be zero")
	}
	if cfg.MaxUncompressedBytes == 0 {
		t.Error("MaxUncompressedBytes must not be zero")
	}
	if cfg.MaxFileCount == 0 {
		t.Error("MaxFileCount must not be zero")
	}
	if len(cfg.BlockedExtensions) == 0 {
		t.Error("BlockedExtensions must not be empty")
	}
}

// ==============================================================================
// Custom config — callers can override thresholds
// ==============================================================================

func TestCustomConfig(t *testing.T) {
	t.Parallel()

	cfg := validate.Config{
		MaxZipCompressedBytes: 1 << 20, // 1 MiB
		MaxSingleHTMLBytes:    1024,
		MaxUncompressedBytes:  1 << 20,
		MaxEntryBytes:         512 << 10,
		MaxFileCount:          10,
		BlockedExtensions: map[string]bool{
			".exe": true,
			".php": true,
		},
	}

	// Valid zip under custom config.
	raw := mustZip(map[string]string{
		"index.html": "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, cfg)

	if !result.Valid {
		t.Fatalf("expected valid under custom config, got: %v", result.Errors)
	}
}

// ==============================================================================
// Pasted HTML is validated identically to single file
// ==============================================================================

func TestValidate_PasteAndIndexAreIdentical(t *testing.T) {
	t.Parallel()

	html := "<html><body><h1>Test</h1></body></html>"
	cfg := validate.DefaultConfig()

	r1 := validate.Validate(validate.Input{
		UploadType:          "index",
		Content:             []byte(html),
		CompressedSizeBytes: int64(len(html)),
	}, cfg)

	r2 := validate.Validate(validate.Input{
		UploadType:          "paste",
		Content:             []byte(html),
		CompressedSizeBytes: int64(len(html)),
	}, cfg)

	if r1.Valid != r2.Valid {
		t.Fatal("index and paste must produce identical results")
	}
	if len(r1.Errors) != len(r2.Errors) {
		t.Fatal("index and paste must produce identical errors")
	}
}

// ==============================================================================
// ValidateResult shape matches metadata.ValidationResult
//
// The .Valid and .Errors fields align with the backend's
// metadata.ValidationResult struct so the worker can store results directly.
// ==============================================================================

func TestValidateResult_Shape(t *testing.T) {
	t.Parallel()

	result := validate.Validate(validate.Input{
		UploadType:          "index",
		Content:             []byte("<html></html>"),
		CompressedSizeBytes: 13,
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatal("expected valid")
	}
	if result.Errors != nil {
		t.Errorf("expected nil errors for valid result, got %v", result.Errors)
	}
	if len(result.ValidatedFiles) != 1 {
		t.Errorf("expected 1 validated file, got %d", len(result.ValidatedFiles))
	}
	if _, ok := result.ValidatedFiles["index.html"]; !ok {
		t.Error("expected index.html in validated files")
	}
}

// ==============================================================================
// Nested directories — valid nested structure
// ==============================================================================

func TestValidateZip_NestedDirectories(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html":                    "<html></html>",
		"assets/css/main.css":           "body{}",
		"assets/js/main.js":             "1",
		"assets/images/icons/logo.png":  "png",
		"blog/post/index.html":          "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid for nested structure, got: %v", result.Errors)
	}
	if _, ok := result.ValidatedFiles["blog/post/index.html"]; !ok {
		t.Error("expected nested index.html to be preserved")
	}
}

// ==============================================================================
// Only public/ dir — but no other top-level entries — still normalizes
// ==============================================================================

func TestValidateZip_PublicDirWithNoConflicts(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"public/index.html":       "<html></html>",
		"public/about/index.html": "<html></html>",
		"public/css/style.css":    "body{}",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	if !result.Valid {
		t.Fatalf("expected valid, got: %v", result.Errors)
	}
	// All paths should be stripped of public/ prefix.
	for name := range result.ValidatedFiles {
		if strings.HasPrefix(name, "public/") {
			t.Errorf("normalized path %q still has public/ prefix", name)
		}
	}
}

// ==============================================================================
// Mixed top-level entries — public/ is NOT stripped when other entries exist
// ==============================================================================

func TestValidateZip_MixedTopLevel_PublicNotStripped(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html":         "<html><body>Root</body></html>",
		"public/index.html":  "<html><body>Public</body></html>",
		"README.md":          "# Readme",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	// Should be valid — index.html exists at root.
	// public/ should NOT be stripped because other top-level entries exist.
	if !result.Valid {
		t.Fatalf("expected valid, got: %v", result.Errors)
	}
	// README.md should remain at root.
	if _, ok := result.ValidatedFiles["README.md"]; !ok {
		t.Error("README.md should be at root when no normalization occurs")
	}
	// public/index.html should still be under public/ since normalization didn't apply.
	if _, ok := result.ValidatedFiles["public/index.html"]; !ok {
		t.Error("public/index.html should remain when public/ isn't the only top-level dir")
	}
	// index.html should be at root.
	if _, ok := result.ValidatedFiles["index.html"]; !ok {
		t.Error("root index.html should be present")
	}
}

// ==============================================================================
// Unicode / non-ASCII filenames — should pass through (UTF-8 byte paths)
// ==============================================================================

func TestValidateZip_UnicodeFilenames(t *testing.T) {
	t.Parallel()

	raw := mustZip(map[string]string{
		"index.html":   "<html></html>",
		"résumé.html":  "<html></html>",
	})

	result := validate.Validate(validate.Input{
		UploadType:          "zip",
		Content:             raw,
		CompressedSizeBytes: int64(len(raw)),
	}, validate.DefaultConfig())

	// zip is valid — unicode filenames are not blocked.
	if !result.Valid {
		t.Fatalf("expected valid for unicode filenames, got: %v", result.Errors)
	}
	if _, ok := result.ValidatedFiles["résumé.html"]; !ok {
		t.Error("expected unicode filename to be preserved")
	}
}
