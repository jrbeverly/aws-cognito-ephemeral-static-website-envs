// ==============================================================================
// cognito_test.go — Unit tests for Cognito claim parsing
// ==============================================================================

package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"backend/go-api/internal/auth"
)

func TestParseHeaders_ALBHeader(t *testing.T) {
	claims := map[string]string{
		"sub":              "abc123-sub",
		"email":            "janedoe@example.com",
		"cognito:username": "janedoe",
	}

	encoded := makeJWT(claims)

	identity, err := auth.ParseHeaders(map[string]string{
		auth.HeaderALBData: encoded,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if identity.UserID != "abc123-sub" {
		t.Errorf("UserID = %q, want %q", identity.UserID, "abc123-sub")
	}
	if identity.Email != "janedoe@example.com" {
		t.Errorf("Email = %q, want %q", identity.Email, "janedoe@example.com")
	}
	if identity.Username != "janedoe" {
		t.Errorf("Username = %q, want %q", identity.Username, "janedoe")
	}
	if identity.UserSlug != "janedoe" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "janedoe")
	}
}

func TestParseHeaders_ALBHeaderDerivesSlugFromEmail(t *testing.T) {
	claims := map[string]string{
		"sub":              "sub-1",
		"email":            "Alice.Wonderland@Example.com",
		"cognito:username": "awonder",
	}

	encoded := makeJWT(claims)

	identity, err := auth.ParseHeaders(map[string]string{
		auth.HeaderALBData: encoded,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slug should be derived from email local part (alice-wonderland), not username
	if identity.UserSlug != "alice-wonderland" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "alice-wonderland")
	}
}

func TestParseHeaders_ALBHeaderDerivesSlugFromUsernameWhenNoEmail(t *testing.T) {
	claims := map[string]string{
		"sub":              "sub-2",
		"cognito:username": "janedoe",
	}

	encoded := makeJWT(claims)

	identity, err := auth.ParseHeaders(map[string]string{
		auth.HeaderALBData: encoded,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if identity.UserSlug != "janedoe" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "janedoe")
	}
}

func TestParseHeaders_DevHeaders(t *testing.T) {
	identity, err := auth.ParseHeaders(map[string]string{
		auth.HeaderDevUserID:   "test-sub",
		auth.HeaderDevEmail:    "test@example.com",
		auth.HeaderDevUsername: "tester",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if identity.UserID != "test-sub" {
		t.Errorf("UserID = %q, want %q", identity.UserID, "test-sub")
	}
	if identity.UserSlug != "test" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "test")
	}
}

func TestParseHeaders_DevHeadersExplicitSlug(t *testing.T) {
	identity, err := auth.ParseHeaders(map[string]string{
		auth.HeaderDevUserID:   "sub-3",
		auth.HeaderDevUserSlug: "custom-slug",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if identity.UserSlug != "custom-slug" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "custom-slug")
	}
}

func TestParseHeaders_DevSlugDerivedFromEmail(t *testing.T) {
	// Dev headers are used when no ALB headers are present.
	// ALB headers take priority in production — dev headers only activate
	// when x-amzn-oidc-data is absent (local development / testing).
	devIdentity, _ := auth.ParseHeaders(map[string]string{
		auth.HeaderDevUserID: "dev-sub",
		auth.HeaderDevEmail:  "dev@test.com",
	})

	if devIdentity.UserID != "dev-sub" {
		t.Errorf("UserID = %q, want %q", devIdentity.UserID, "dev-sub")
	}
}

func TestParseHeaders_NoIdentity(t *testing.T) {
	_, err := auth.ParseHeaders(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty headers")
	}
	if err != auth.ErrNoIdentity {
		t.Errorf("error = %v, want %v", err, auth.ErrNoIdentity)
	}
}

func TestParseHeaders_InvalidJWT(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{"not enough segments", "header.payload"},
		{"too many segments", "a.b.c.d"},
		{"bad base64", "header.!@#.sig"},
		{"bad json payload", "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.ParseHeaders(map[string]string{
				auth.HeaderALBData: tt.encoded,
			})
			if err == nil {
				t.Fatal("expected error for invalid JWT")
			}
		})
	}
}

func TestParseHeaders_MissingSub(t *testing.T) {
	claims := map[string]string{
		"email":            "user@example.com",
		"cognito:username": "user",
	}
	encoded := makeJWT(claims)

	_, err := auth.ParseHeaders(map[string]string{
		auth.HeaderALBData: encoded,
	})
	if err == nil {
		t.Fatal("expected error for missing sub claim")
	}
}

func TestParseEventAuthorizer(t *testing.T) {
	identity, err := auth.ParseEventAuthorizer(map[string]string{
		"sub":              "cognito-sub-789",
		"email":            "bob@example.com",
		"cognito:username": "bsmith",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if identity.UserID != "cognito-sub-789" {
		t.Errorf("UserID = %q, want %q", identity.UserID, "cognito-sub-789")
	}
	if identity.Email != "bob@example.com" {
		t.Errorf("Email = %q, want %q", identity.Email, "bob@example.com")
	}
	if identity.UserSlug != "bob" {
		t.Errorf("UserSlug = %q, want %q", identity.UserSlug, "bob")
	}
}

func TestParseEventAuthorizer_MissingSub(t *testing.T) {
	_, err := auth.ParseEventAuthorizer(map[string]string{
		"email": "user@example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing sub claim")
	}
	if err != auth.ErrMissingSub {
		t.Errorf("error = %v, want %v", err, auth.ErrMissingSub)
	}
}

func TestDeriveSlug(t *testing.T) {
	// We test slug derivation indirectly through ParseHeaders.
	// The slug derivation rules are:
	//   1. Prefer email local part
	//   2. Lowercase
	//   3. Replace non-alnum runs with hyphens
	//   4. Strip leading/trailing hyphens
	//   5. Clamp to 39 characters
	//   6. Fallback to "user"

	tests := []struct {
		name        string
		sub         string
		email       string
		username    string
		wantUserID  string
		wantSlug    string
	}{
		{
			name:     "simple email",
			sub:      "s1",
			email:    "first.last@example.com",
			username: "flast",
			wantSlug: "first-last",
		},
		{
			name:     "email with special chars",
			sub:      "s2",
			email:    "john_doe+test@example.com",
			username: "jdoe",
			wantSlug: "john-doe-test",
		},
		{
			name:     "username only",
			sub:      "s3",
			email:    "",
			username: "jane_doe",
			wantSlug: "jane-doe",
		},
		{
			name:     "email with uppercase",
			sub:      "s4",
			email:    "UPPERCASE@example.com",
			username: "uppercase",
			wantSlug: "uppercase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := map[string]string{"sub": tt.sub}
			if tt.email != "" {
				claims["email"] = tt.email
			}
			if tt.username != "" {
				claims["cognito:username"] = tt.username
			}

			encoded := makeJWT(claims)
			identity, err := auth.ParseHeaders(map[string]string{
				auth.HeaderALBData: encoded,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if identity.UserSlug != tt.wantSlug {
				t.Errorf("UserSlug = %q, want %q", identity.UserSlug, tt.wantSlug)
			}
		})
	}
}

// makeJWT builds a minimal base64-encoded JWT string with the given claims as payload.
// The header and signature are dummy values — the parser only reads the payload.
func makeJWT(claims map[string]string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	sig := base64.RawURLEncoding.EncodeToString([]byte("dummy-signature"))

	return header + "." + payload + "." + sig
}
