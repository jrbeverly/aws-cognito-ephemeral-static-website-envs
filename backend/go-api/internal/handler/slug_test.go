// ==============================================================================
// slug_test.go — Unit tests for site slug validation
// ==============================================================================

package handler_test

import (
	"errors"
	"testing"

	"backend/go-api/internal/handler"
)

func TestValidateSiteSlug_Valid(t *testing.T) {
	slugs := []string{
		"my-blog",
		"docs-site",
		"landing-page",
		"a",
		"abc123",
		"site-1",
		"x-y-z",
		"a123456789b123456789c123456789d12345678", // exactly 39 chars
	}
	for _, s := range slugs {
		if err := handler.ValidateSiteSlug(s); err != nil {
			t.Errorf("ValidateSiteSlug(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateSiteSlug_Empty(t *testing.T) {
	err := handler.ValidateSiteSlug("")
	if err == nil {
		t.Fatal("expected error for empty slug")
	}
	if !errors.Is(err, handler.ErrSlugEmpty) {
		t.Errorf("error = %v, want %v", err, handler.ErrSlugEmpty)
	}
}

func TestValidateSiteSlug_TooLong(t *testing.T) {
	// 40 characters, exceeds the 39-char limit.
	long := "a123456789b123456789c123456789d1234567890"
	err := handler.ValidateSiteSlug(long)
	if err == nil {
		t.Fatal("expected error for oversize slug")
	}
	if !errors.Is(err, handler.ErrSlugTooLong) {
		t.Errorf("error = %v, want %v", err, handler.ErrSlugTooLong)
	}
}

func TestValidateSiteSlug_LeadingDash(t *testing.T) {
	err := handler.ValidateSiteSlug("-bad")
	if err == nil {
		t.Fatal("expected error for leading dash")
	}
	if !errors.Is(err, handler.ErrSlugLeadingDash) {
		t.Errorf("error = %v, want %v", err, handler.ErrSlugLeadingDash)
	}
}

func TestValidateSiteSlug_TrailingDash(t *testing.T) {
	err := handler.ValidateSiteSlug("bad-")
	if err == nil {
		t.Fatal("expected error for trailing dash")
	}
	if !errors.Is(err, handler.ErrSlugTrailingDash) {
		t.Errorf("error = %v, want %v", err, handler.ErrSlugTrailingDash)
	}
}

func TestValidateSiteSlug_InvalidChars(t *testing.T) {
	bad := []string{
		"My-Blog",       // uppercase
		"my_blog",        // underscore
		"my blog",        // space
		"my.blog",        // dot
		"blog!",          // exclamation
		"über",           // non-ASCII
	}
	for _, s := range bad {
		err := handler.ValidateSiteSlug(s)
		if err == nil {
			t.Errorf("ValidateSiteSlug(%q) = nil, want error", s)
			continue
		}
		if !errors.Is(err, handler.ErrSlugInvalidChars) {
			t.Errorf("ValidateSiteSlug(%q) error = %v, want %v", s, err, handler.ErrSlugInvalidChars)
		}
	}
}
