// ==============================================================================
// sites.go — Site management handlers
//
// These endpoints form the site lifecycle CRUD surface.  All endpoints require
// authentication — the user identity is derived from Cognito claims.
//
// The backend never accepts client-supplied owner IDs or S3 prefixes.  The
// owner is always derived from the authenticated identity, and S3 prefixes
// are always constructed server-side from metadata (VISION.md §8.2, §13.2).
//
// Ownership is enforced at the data layer: the DynamoDB primary key includes
// the user ID, so a user cannot read or mutate another user's site even if
// they guess the site ID.  No additional authorization check is needed
// beyond passing the authenticated user's ID to the repository.
// ==============================================================================

package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"backend/go-api/internal/auth"
	"backend/go-api/internal/metadata"
	"backend/go-api/internal/response"
)

// ==============================================================================
// SitesHandler — Site lifecycle operations with server-side authorization
// ==============================================================================

// SitesHandler implements the site CRUD endpoints backed by a
// metadata.SiteMetadataRepository.  Every method derives the owner identity
// from the authenticated request context — clients never provide owner IDs.
type SitesHandler struct {
	repo        metadata.SiteMetadataRepository
	callbacks   CallbackRegistrar
	sitesDomain string
}

// NewSitesHandler creates a SitesHandler with its repository, callback
// registrar, and sites-domain configuration.
func NewSitesHandler(repo metadata.SiteMetadataRepository, callbacks CallbackRegistrar, sitesDomain string) *SitesHandler {
	return &SitesHandler{repo: repo, callbacks: callbacks, sitesDomain: sitesDomain}
}

// ==============================================================================
// Request / response types
// ==============================================================================

// createSiteRequest is the JSON body expected by CreateSite.
type createSiteRequest struct {
	SiteSlug string `json:"site_slug"`
}

// siteResponse is the JSON shape returned for a single site.
type siteResponse struct {
	SiteID    string   `json:"site_id"`
	SiteSlug  string   `json:"site_slug"`
	UserID    string   `json:"user_id"`
	Hostnames []string `json:"hostnames"`
	CreatedAt string   `json:"created_at"`
	Disabled  bool     `json:"disabled"`
}

// deleteSiteResponse is the JSON shape returned after a successful soft-delete.
type deleteSiteResponse struct {
	SiteID  string `json:"site_id"`
	Message string `json:"message"`
}

// ==============================================================================
// GET /api/sites — List the authenticated user's sites
// ==============================================================================

func (h *SitesHandler) ListSites(w http.ResponseWriter, r *http.Request) {
	id := auth.RequestIdentity(r)
	if id == nil {
		response.Unauthorized(w, "")
		return
	}

	sites, err := h.repo.ListSites(r.Context(), id.UserID)
	if err != nil {
		slog.Error("list sites failed", "user_id", id.UserID, "error", err)
		response.InternalError(w, err)
		return
	}

	// Build the response slice — always return [] not null.
	out := make([]siteResponse, 0, len(sites))
	for _, s := range sites {
		out = append(out, siteResponseFromMetadata(s, id.UserSlug, h.sitesDomain))
	}

	slog.Info("listed sites", "user_id", id.UserID, "count", len(out))
	response.JSON(w, http.StatusOK, out)
}

// ==============================================================================
// POST /api/sites — Create a new site
//
// The site is created within the authenticated user's namespace.
// The site slug must be a valid DNS-safe segment and must not conflict with
// an existing hostname owned by any user.
// ==============================================================================

func (h *SitesHandler) CreateSite(w http.ResponseWriter, r *http.Request) {
	id := auth.RequestIdentity(r)
	if id == nil {
		response.Unauthorized(w, "")
		return
	}

	// Parse and validate the request body.
	var req createSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Invalid request body: must be JSON with a site_slug field")
		return
	}

	slug := sanitizeSlug(req.SiteSlug)
	if err := ValidateSiteSlug(slug); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	ctx := r.Context()

	// Check hostname availability before creating the site.
	// Both patterns must be free; if either is taken the creation is rejected.
	preferredHost := fmt.Sprintf("%s.%s.%s", slug, id.UserSlug, h.sitesDomain)
	aliasHost := fmt.Sprintf("%s--%s.%s", slug, id.UserSlug, h.sitesDomain)

	for _, host := range []string{preferredHost, aliasHost} {
		existing, err := h.repo.GetHostMapping(ctx, host)
		if err != nil {
			slog.Error("check host mapping failed", "hostname", host, "error", err)
			response.InternalError(w, err)
			return
		}
		if existing != nil {
			response.Conflict(w, fmt.Sprintf("Hostname %q is already in use", host))
			return
		}
	}

	if err := h.callbacks.AddCallbackURL(ctx, "https://"+aliasHost+"/oauth2/idpresponse"); err != nil {
		slog.Error("register site callback failed", "hostname", aliasHost, "error", err)
		response.InternalError(w, err)
		return
	}

	// Generate a unique site ID.
	siteID := newSiteID()

	// Create the site record.
	site := metadata.Site{
		SiteID:   siteID,
		UserID:   id.UserID,
		SiteSlug: slug,
	}
	if err := h.repo.CreateSite(ctx, site); err != nil {
		slog.Error("create site failed", "user_id", id.UserID, "site_id", siteID, "error", err)
		response.InternalError(w, err)
		return
	}

	// Create host mappings for both patterns.
	for _, host := range []string{preferredHost, aliasHost} {
		mapping := metadata.HostMapping{
			Hostname: host,
			UserID:   id.UserID,
			UserSlug: id.UserSlug,
			SiteID:   siteID,
			SiteSlug: slug,
			// VersionID and S3PublishedPrefix are empty — no active version yet.
		}
		if err := h.repo.CreateHostMapping(ctx, mapping); err != nil {
			slog.Error("create host mapping failed", "hostname", host, "error", err)
			response.InternalError(w, err)
			return
		}
	}

	// Read back the created site to get the canonical timestamp.
	created, err := h.repo.GetSite(ctx, id.UserID, siteID)
	if err != nil || created == nil {
		slog.Error("read back created site failed", "site_id", siteID, "error", err)
		response.InternalError(w, fmt.Errorf("site was created but could not be read back"))
		return
	}

	slog.Info("site created",
		"user_id", id.UserID,
		"site_id", siteID,
		"site_slug", slug,
		"preferred_host", preferredHost,
	)

	response.JSON(w, http.StatusCreated, siteResponseFromMetadata(*created, id.UserSlug, h.sitesDomain))
}

// ==============================================================================
// GET /api/sites/{id} — Get a single site by ID
//
// Ownership is enforced at the data layer: GetSite queries by userID + siteID,
// so a request for another user's site returns nil → 404.
// ==============================================================================

func (h *SitesHandler) GetSite(w http.ResponseWriter, r *http.Request) {
	id := auth.RequestIdentity(r)
	if id == nil {
		response.Unauthorized(w, "")
		return
	}

	siteID := r.PathValue("id")

	site, err := h.repo.GetSite(r.Context(), id.UserID, siteID)
	if err != nil {
		slog.Error("get site failed", "user_id", id.UserID, "site_id", siteID, "error", err)
		response.InternalError(w, err)
		return
	}
	if site == nil {
		response.NotFound(w, "Site not found")
		return
	}

	response.JSON(w, http.StatusOK, siteResponseFromMetadata(*site, id.UserSlug, h.sitesDomain))
}

// ==============================================================================
// DELETE /api/sites/{id} — Soft-delete a site
//
// Sets the disabled flag on the site metadata.  The gateway checks this flag
// and stops serving the site (VISION.md §12).  Physical S3 deletion is not
// performed by this endpoint.
// ==============================================================================

func (h *SitesHandler) DeleteSite(w http.ResponseWriter, r *http.Request) {
	id := auth.RequestIdentity(r)
	if id == nil {
		response.Unauthorized(w, "")
		return
	}

	siteID := r.PathValue("id")

	// Verify the site exists and belongs to the authenticated user.
	// DeleteSite uses userID as part of the PK, so a wrong-user request
	// will fail with "not found" — no explicit ownership check needed.
	site, err := h.repo.GetSite(r.Context(), id.UserID, siteID)
	if err != nil {
		slog.Error("get site before delete failed", "user_id", id.UserID, "site_id", siteID, "error", err)
		response.InternalError(w, err)
		return
	}
	if site == nil {
		response.NotFound(w, "Site not found")
		return
	}

	if err := h.repo.DeleteSite(r.Context(), id.UserID, siteID); err != nil {
		slog.Error("delete site failed", "user_id", id.UserID, "site_id", siteID, "error", err)
		response.InternalError(w, err)
		return
	}

	// Propagate the site-disabled flag to host mappings so the gateway can
	// reject requests with a single DynamoDB GetItem (see
	// docs/design-gateway-host-to-prefix-resolution.md §4.1, §7.2).
	//
	// Each site has two hostname patterns.  The host mapping may not exist
	// for both patterns (some deployments only create one), so a missing
	// mapping is logged but not treated as an error.
	h.syncSiteDisabledToHostMappings(r.Context(), *site, id.UserSlug, true)

	slog.Info("site deleted (soft)", "user_id", id.UserID, "site_id", siteID)

	response.JSON(w, http.StatusOK, deleteSiteResponse{
		SiteID:  siteID,
		Message: "Site deleted",
	})
}

// ==============================================================================
// Internal helpers
// ==============================================================================

// syncSiteDisabledToHostMappings updates both hostname patterns for a site
// to reflect the current site-disabled state.  Used after soft-delete (sets
// siteDisabled=true) so the gateway sees the change with a single GetItem.
//
// Missing host mappings are logged and skipped — they may not exist if only
// one pattern was created in a given deployment.
func (h *SitesHandler) syncSiteDisabledToHostMappings(ctx context.Context, site metadata.Site, userSlug string, disabled bool) {
	preferredHost := fmt.Sprintf("%s.%s.%s", site.SiteSlug, userSlug, h.sitesDomain)
	aliasHost := fmt.Sprintf("%s--%s.%s", site.SiteSlug, userSlug, h.sitesDomain)

	for _, host := range []string{preferredHost, aliasHost} {
		mapping, err := h.repo.GetHostMapping(ctx, host)
		if err != nil {
			slog.Warn("failed to read host mapping for disabled-flag sync",
				"hostname", host, "site_id", site.SiteID, "error", err)
			continue
		}
		if mapping == nil {
			slog.Debug("host mapping not found — skipping disabled-flag sync",
				"hostname", host, "site_id", site.SiteID)
			continue
		}
		mapping.SiteDisabled = disabled
		if err := h.repo.UpdateHostMapping(ctx, host, *mapping); err != nil {
			slog.Warn("failed to update host mapping for disabled-flag sync",
				"hostname", host, "site_id", site.SiteID, "error", err)
			continue
		}
		slog.Debug("synced site-disabled flag to host mapping",
			"hostname", host, "site_disabled", disabled)
	}
}

// ==============================================================================

// siteResponseFromMetadata converts a metadata.Site into the API response shape.
func siteResponseFromMetadata(s metadata.Site, userSlug, sitesDomain string) siteResponse {
	// Only the one-label alias is offered: the ALB cert is *.<sitesDomain>, and
	// a wildcard cannot cover <site>.<user>.<sitesDomain> (ACM has no *.*).
	hostnames := []string{
		fmt.Sprintf("%s--%s.%s", s.SiteSlug, userSlug, sitesDomain),
	}
	return siteResponse{
		SiteID:    s.SiteID,
		SiteSlug:  s.SiteSlug,
		UserID:    s.UserID,
		Hostnames: hostnames,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		Disabled:  s.Disabled,
	}
}

// newSiteID generates a unique site identifier.
// Uses crypto/rand to produce a 16-byte hex string prefixed with "site_".
func newSiteID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read can fail only on a broken system; fall back to a
		// timestamp-based suffix so callers don't have to handle the error.
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return fmt.Sprintf("site_%x", b)
}
