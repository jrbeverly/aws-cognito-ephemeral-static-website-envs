// ==============================================================================
// api/uploads.js — Upload management API methods
//
// Calls the backend's /api/uploads endpoints (backend/go-api/internal/handler/uploads.go).
// The frontend never provides S3 keys, prefixes, or owner IDs — all are
// derived server-side from authenticated identity.
//
// Upload flows:
//   1. CreateUpload   → get a presigned S3 PUT URL + upload ID
//   2. Upload to S3   → PUT the content to the presigned URL (not this client)
//   3. CompleteUpload → signal the backend that the S3 PUT is done
//   4. Poll status    → GET /api/uploads/{id} until status is published/failed
//
//   Paste uploads skip S3 — the backend stores the content directly.
// ==============================================================================

import { apiJson } from "./client";

/**
 * Request an upload grant for a site.
 * @param {string} siteId
 * @param {"zip"|"index"} type - upload type
 * @returns {Promise<object>} { upload_id, presigned_url, staging_key, expires_at }
 */
export async function requestUploadGrant(siteId, type) {
  return apiJson("/api/uploads", {
    method: "POST",
    body: JSON.stringify({ site_id: siteId, type }),
  });
}

/**
 * Signal that the presigned S3 upload is complete.
 * @param {string} uploadId
 * @returns {Promise<object>} { upload_id, status }
 */
export async function completeUpload(uploadId) {
  return apiJson(`/api/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST",
  });
}

/**
 * Get the current status of an upload.
 * @param {string} uploadId
 * @returns {Promise<object>} upload status including validation_result
 */
export async function getUploadStatus(uploadId) {
  return apiJson(`/api/uploads/${encodeURIComponent(uploadId)}`);
}

/**
 * Paste HTML directly as a new upload.
 * @param {string} siteId
 * @param {string} html - raw HTML content
 * @returns {Promise<object>} { upload_id, site_id, status, staging_key, created_at }
 */
export async function pasteUpload(siteId, html) {
  return apiJson("/api/uploads/paste", {
    method: "POST",
    body: JSON.stringify({ site_id: siteId, html }),
  });
}
