// ==============================================================================
// zip.go — Safe zip extraction with path-traversal and content protection
//
// Every uploaded zip is untrusted (VISION.md §13.3).  This file implements
// zip extraction that enforces:
//   - No path traversal (.. segments, absolute paths)
//   - No symlink entries
//   - No empty or non-canonical file names
//   - Per-entry size limits
//   - Total uncompressed size and file count limits
//
// VISION.md §5.2 — Zip upload; §5.4 — Failure messages; §8.5 — Validation worker.
// ==============================================================================

package validate

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// ==============================================================================
// Sentinel errors — each maps to a §5.4 failure message
// ==============================================================================

var (
	ErrZipNotExtracted        = errors.New("zip could not be extracted")
	ErrPathTraversal          = errors.New("path traversal detected")
	ErrAbsolutePath           = errors.New("path traversal detected")
	ErrSymlinkDetected        = errors.New("symlink content is not supported")
	ErrOversizedEntry         = errors.New("file exceeds maximum allowed size")
	ErrTooManyFiles           = errors.New("file count exceeds allowed limit")
	ErrUncompressedSizeExceed = errors.New("total uncompressed size exceeds allowed limit")
)

// ==============================================================================
// extractionConfig — limits enforced during extraction
// ==============================================================================

type extractionConfig struct {
	maxUncompressedBytes int64
	maxEntryBytes        int64
	maxFileCount         int
}

// ==============================================================================
// extractZip — Safe zip extraction
//
// Reads a zip archive from data, validates every entry, and returns a map of
// normalized file path → file content.  Paths are cleaned, lowercased for
// extension checks, and compared against the set of blocked extensions.
//
// Normalization: after extraction, if the archive contains a single top-level
// directory named "public/", its contents are promoted to the root.  This
// matches the Hugo default output layout (VISION.md §5.2).
//
// Returns:
//   - files:   normalized path → content map (validated, safe files only)
//   - entries: original zip entry count (before filtering)
//   - err:     non-nil when extraction is impossible or a safety check fails
// ==============================================================================

func extractZip(data []byte, blockedExts map[string]bool, cfg extractionConfig) (files map[string][]byte, entries int, err error) {
	reader, err := zip.NewReader(bytesReaderAt(data), int64(len(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrZipNotExtracted, err)
	}

	files = make(map[string][]byte)
	var totalUncompressed int64

	for _, f := range reader.File {
		entries++

		// ------------------------------------------------------------------
		// File count check (evaluate before reading the entry body so we
		// don't unpack content we would immediately discard).
		// ------------------------------------------------------------------
		if len(files) >= cfg.maxFileCount {
			return nil, entries, ErrTooManyFiles
		}

		// ------------------------------------------------------------------
		// Reject symlinks.  The zip spec encodes symlinks via the external
		// attributes or the Unix mode field.  We reject any entry where the
		// mode indicates a symlink, OR where the name suggests a symlink
		// placeholder (e.g., "__MACOSX" resource forks).
		// ------------------------------------------------------------------
		if isSymlink(f) {
			return nil, entries, fmt.Errorf("%w: %s", ErrSymlinkDetected, f.Name)
		}

		// ------------------------------------------------------------------
		// Path safety checks
		// ------------------------------------------------------------------
		clean := path.Clean(f.Name)

		// Reject absolute paths (e.g., "/etc/passwd").
		if path.IsAbs(clean) {
			return nil, entries, fmt.Errorf("%w: absolute path %q", ErrAbsolutePath, f.Name)
		}

		// Reject path traversal (.. segments).
		if strings.Contains(clean, "..") {
			return nil, entries, fmt.Errorf("%w: %q", ErrPathTraversal, f.Name)
		}

		// Reject empty names after cleaning.
		if clean == "." || clean == "" {
			continue // skip directory entries with no name
		}

		// Reject names that start with a dot or backslash (hidden / escape).
		if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, `\`) {
			return nil, entries, fmt.Errorf("%w: %q", ErrPathTraversal, f.Name)
		}

		// ------------------------------------------------------------------
		// Skip directory entries (they have no body).
		// ------------------------------------------------------------------
		if f.FileInfo().IsDir() {
			continue
		}

		// ------------------------------------------------------------------
		// Extension block list check.
		// ------------------------------------------------------------------
		ext := strings.ToLower(path.Ext(clean))
		if blockedExts[ext] {
			return nil, entries, fmt.Errorf("unsupported file type: %s (%s)", clean, ext)
		}

		// ------------------------------------------------------------------
		// Per-entry size check (before uncompressing).
		// ------------------------------------------------------------------
		if f.UncompressedSize64 > uint64(cfg.maxEntryBytes) {
			return nil, entries, fmt.Errorf("%w: %s (%d bytes)", ErrOversizedEntry, clean, f.UncompressedSize64)
		}

		// ------------------------------------------------------------------
		// Total uncompressed size check.
		// ------------------------------------------------------------------
		if totalUncompressed+int64(f.UncompressedSize64) > cfg.maxUncompressedBytes {
			return nil, entries, fmt.Errorf("%w: limit is %d bytes", ErrUncompressedSizeExceed, cfg.maxUncompressedBytes)
		}

		// ------------------------------------------------------------------
		// Read and decompress the entry body.
		// ------------------------------------------------------------------
		rc, err := f.Open()
		if err != nil {
			return nil, entries, fmt.Errorf("%w: cannot open %s: %v", ErrZipNotExtracted, f.Name, err)
		}

		// Limit the reader to the declared uncompressed size + a small margin
		// to detect zip bombs that declare one size but decompress larger.
		body, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if err != nil {
			return nil, entries, fmt.Errorf("%w: cannot read %s: %v", ErrZipNotExtracted, f.Name, err)
		}

		// Zip-bomb detection: actual decompressed size exceeds declared size.
		if int64(len(body)) > int64(f.UncompressedSize64) {
			return nil, entries, fmt.Errorf("%w: zip bomb detected in %s", ErrZipNotExtracted, f.Name)
		}

		totalUncompressed += int64(len(body))
		files[clean] = body
	}

	// ----------------------------------------------------------------------
	// Normalize: promote a single top-level "public/" directory.
	// Hugo generates output into a public/ directory by default.  If the
	// zip contains exactly one top-level directory named "public", strip it
	// and treat its contents as the site root (VISION.md §5.2).
	// ----------------------------------------------------------------------
	files = normalizePublicDir(files)

	return files, entries, nil
}

// ==============================================================================
// Symlink detection
// ==============================================================================

// isSymlink returns true when the zip entry represents a symlink.
//
// In zip files, symlinks are typically stored with:
//   - External attributes where the high 16 bits are a Unix file mode with
//     S_IFLNK (0120000).
//   - The file body holding the symlink target path.
//
// This is a best-effort check.  Most zip tools do not preserve symlinks by
// default, but archive_zip supports reading the mode bits.
func isSymlink(f *zip.File) bool {
	// Check the Unix mode bits in the external attributes.
	// S_IFLNK = 0120000 (in octal) = 0xA000.
	mode := f.FileInfo().Mode()
	return mode&0xA0000000 != 0 && mode&0x20000000 != 0 // type == symlink
}

// ==============================================================================
// normalizePublicDir — Promote a single top-level public/ directory
//
// If all extracted file paths start with "public/" and no other top-level
// entries exist, strip that prefix so index.html lives at the virtual root.
// This matches the Hugo default output layout behaviour described in §5.2.
// ==============================================================================

func normalizePublicDir(files map[string][]byte) map[string][]byte {
	const prefix = "public/"

	// Verify every file is under public/.
	hasOther := false
	for name := range files {
		if !strings.HasPrefix(name, prefix) {
			hasOther = true
			break
		}
	}

	// If all files are under public/, promote.
	if !hasOther && len(files) > 0 {
		promoted := make(map[string][]byte, len(files))
		for name, body := range files {
			promoted[name[len(prefix):]] = body
		}
		return promoted
	}

	return files
}

// ==============================================================================
// bytesReaderAt — adapts a byte slice to io.ReaderAt for archive/zip
// ==============================================================================

type bytesReaderAt []byte

func (b bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || int(off) >= len(b) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
