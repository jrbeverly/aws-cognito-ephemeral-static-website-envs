// ==============================================================================
// api/sites.js — Site management API methods
//
// Calls the backend's /api/sites endpoints (backend/go-api/internal/handler/sites.go).
// All authorization is server-side — the frontend never sends owner IDs.
// ==============================================================================

import { apiJson } from "./client";

/**
 * List all sites belonging to the authenticated user.
 * @returns {Promise<Array>} list of site objects
 */
export async function listSites() {
  return apiJson("/api/sites");
}

/**
 * Create a new site with the given slug.
 * @param {string} siteSlug - DNS-safe site namespace segment
 * @returns {Promise<object>} created site
 */
export async function createSite(siteSlug) {
  return apiJson("/api/sites", {
    method: "POST",
    body: JSON.stringify({ site_slug: siteSlug }),
  });
}

/**
 * Get a single site by ID.
 * @param {string} siteId
 * @returns {Promise<object>} site details
 */
export async function getSite(siteId) {
  return apiJson(`/api/sites/${encodeURIComponent(siteId)}`);
}

/**
 * Soft-delete a site.
 * @param {string} siteId
 * @returns {Promise<object>} deletion confirmation
 */
export async function deleteSite(siteId) {
  return apiJson(`/api/sites/${encodeURIComponent(siteId)}`, {
    method: "DELETE",
  });
}
