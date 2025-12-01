// ==============================================================================
// cognito.go — Cognito identity claim parsing
//
// Derives the effective platform user identity from Cognito claims injected
// by the ALB authenticate-cognito action or API Gateway JWT authorizer.
//
// The ALB injects claims in headers after successful authentication:
//
//	x-amzn-oidc-accesstoken  — OAuth 2.0 access token
//	x-amzn-oidc-identity     — Base64-encoded identity token (JWT)
//	x-amzn-oidc-data         — Base64-encoded user claims (JWT payload)
//
// Claims are already verified by the ALB/API Gateway infrastructure.
// The backend parses them to extract the user identity; it does not trust
// client-supplied owner IDs or S3 keys (VISION.md §8.2, §8.8).
// ==============================================================================

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

// ==============================================================================
// Identity — Platform user identity derived from Cognito claims
// ==============================================================================

// Identity represents the authenticated platform user, derived exclusively
// from Cognito claims.  UserID is the Cognito sub claim — a stable, globally
// unique identifier.  UserSlug is a URL-safe namespace segment derived from
// the user's email or username.
type Identity struct {
	UserID   string `json:"user_id"`   // Cognito sub claim — stable platform identifier
	UserSlug string `json:"user_slug"` // URL-safe namespace segment
	Email    string `json:"email"`     // Email claim (may be empty for non-email users)
	Username string `json:"username"`  // cognito:username claim
}

// ==============================================================================
// Sentinel errors
// ==============================================================================

var (
	ErrNoIdentity   = errors.New("no identity found in request")
	ErrInvalidToken = errors.New("invalid token format")
	ErrMissingSub   = errors.New("cognito sub claim is required")
)

// ==============================================================================
// Claim source constants
// ==============================================================================

const (
	// Headers injected by ALB authenticate-cognito action.
	HeaderALBAccessToken = "x-amzn-oidc-accesstoken"
	HeaderALBIdentity    = "x-amzn-oidc-identity"
	HeaderALBData        = "x-amzn-oidc-data"

	// Headers for local development and testing.
	// These bypass the ALB and allow direct identity injection.
	HeaderDevUserID   = "x-dev-user-id"
	HeaderDevUserSlug = "x-dev-user-slug"
	HeaderDevEmail    = "x-dev-email"
	HeaderDevUsername = "x-dev-username"
)

// cognitoClaims represents the JWT payload injected by the ALB.
type cognitoClaims struct {
	Sub      string `json:"sub"`
	Email    string `json:"email"`
	Username string `json:"cognito:username"`
}

// ==============================================================================
// ParseHeaders — Extract identity from request headers
//
// Priority:
//  1. ALB authenticate-cognito headers (production)
//  2. Dev headers (local development / testing)
//
// Returns ErrNoIdentity when no identity can be derived.
// ==============================================================================

func ParseHeaders(headers map[string]string) (*Identity, error) {
	// Production path — ALB authenticate-cognito header
	if encoded := headers[HeaderALBData]; encoded != "" {
		return parseJWTClaims(encoded)
	}

	// Dev / local testing path
	if userID := headers[HeaderDevUserID]; userID != "" {
		return &Identity{
			UserID:   userID,
			UserSlug: coalesce(headers[HeaderDevUserSlug], deriveSlug(headers[HeaderDevEmail], headers[HeaderDevUsername])),
			Email:    headers[HeaderDevEmail],
			Username: headers[HeaderDevUsername],
		}, nil
	}

	return nil, ErrNoIdentity
}

// ==============================================================================
// ParseEventAuthorizer — Extract identity from an API Gateway JWT authorizer
//
// When API Gateway is configured with a Cognito JWT authorizer, the validated
// claims are placed in event.RequestContext.Authorizer.JWT.Claims.
// ==============================================================================

func ParseEventAuthorizer(claims map[string]string) (*Identity, error) {
	sub := claims["sub"]
	if sub == "" {
		return nil, ErrMissingSub
	}

	email := claims["email"]
	username := claims["cognito:username"]

	return &Identity{
		UserID:   sub,
		UserSlug: deriveSlug(email, username),
		Email:    email,
		Username: username,
	}, nil
}

// ==============================================================================
// Internal helpers
// ==============================================================================

// parseJWTClaims decodes the payload from a base64-encoded JWT string.
// It does NOT verify the signature — the ALB or API Gateway has already
// validated the token before the request reaches this handler.
//
// In production, the JWT signature should be independently verified against
// the Cognito JWKS endpoint (cognito-idp.<region>.amazonaws.com/<pool>/.well-known/jwks.json)
// for defense-in-depth.  This is noted for a follow-up hardening pass.
func parseJWTClaims(encoded string) (*Identity, error) {
	// JWT format: header.payload.signature
	parts := strings.Split(encoded, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrInvalidToken, len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	var claims cognitoClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if claims.Sub == "" {
		return nil, ErrMissingSub
	}

	return &Identity{
		UserID:   claims.Sub,
		UserSlug: deriveSlug(claims.Email, claims.Username),
		Email:    claims.Email,
		Username: claims.Username,
	}, nil
}

// ==============================================================================
// Slug derivation
//
// The user slug is a URL-safe, human-readable namespace segment derived from
// the user's email (local part) or their cognito:username.  It is used in
// hostname construction: {siteSlug}.{userSlug}.sites.{domain}.
//
// Derivation rules:
//  1. Prefer the email local part (before '@'), fall back to username
//  2. Lowercase all characters
//  3. Replace runs of non-alphanumeric characters with a single hyphen
//  4. Strip leading and trailing hyphens
//  5. Clamp to max 39 characters (RFC 1035 label length limit)
//  6. If the result is empty, use "user"
// ==============================================================================

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

const maxSlugLength = 39

func deriveSlug(email, username string) string {
	source := username
	if email != "" {
		// Extract local part (before '@')
		if idx := strings.Index(email, "@"); idx > 0 {
			source = email[:idx]
		} else {
			source = email
		}
	} else if source == "" {
		return "user"
	}

	// Lowercase and normalize
	slug := strings.ToLower(source)

	// Replace runs of non-alphanumeric characters with a single hyphen
	slug = nonSlugChars.ReplaceAllString(slug, "-")

	// Strip leading and trailing hyphens
	slug = strings.Trim(slug, "-")

	// Clamp length
	if len(slug) > maxSlugLength {
		slug = slug[:maxSlugLength]
		slug = strings.TrimRight(slug, "-")
	}

	// If normalization produced an empty string, use a safe fallback
	if slug == "" {
		// Derive from runes of the original source
		var sb strings.Builder
		for _, r := range source {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				sb.WriteRune(unicode.ToLower(r))
			}
		}
		if slug = sb.String(); slug == "" {
			return "user"
		}
		if len(slug) > maxSlugLength {
			slug = slug[:maxSlugLength]
		}
	}

	return slug
}

// coalesce returns the first non-empty string.
func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ==============================================================================
// Context identity accessors
//
// These are defined in the auth package (rather than api) to avoid an import
// cycle between api and handler.  Both api/middleware.go and handler/*.go
// import auth and use these accessors.
// ==============================================================================

type contextKey string

const identityContextKey contextKey = "platform.identity"

// SetIdentity stores the authenticated identity in the context.
// Used by the authentication middleware in api/middleware.go.
func SetIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, id)
}

// GetIdentity retrieves the authenticated identity from the context.
// Returns nil if no identity has been set.
func GetIdentity(ctx context.Context) *Identity {
	if id, ok := ctx.Value(identityContextKey).(*Identity); ok {
		return id
	}
	return nil
}

// RequestIdentity is a convenience accessor that reads from the request context.
func RequestIdentity(r *http.Request) *Identity {
	return GetIdentity(r.Context())
}
