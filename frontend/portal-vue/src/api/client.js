// ==============================================================================
// api/client.js — Backend API client
//
// Thin wrapper around fetch() for calling the Go backend API.  Every call
// includes credentials (cookies) so the ALB Cognito session is forwarded.
//
// In local development the backend accepts x-dev-user-id as an alternative
// to Cognito claims.  The dev identity is set in localStorage by LoginView
// and sent as a header on every request.
//
// No AWS credentials are embedded — the frontend calls the backend, and the
// backend calls AWS with server-side credentials (VISION.md §8.8).
// ==============================================================================

const DEV_USER_ID_STORAGE_KEY = "portal.dev.userId";
const BASE_URL = "";

/**
 * Returns the configured dev identity or an empty object.
 * In production (no dev identity set), the ALB Cognito session handles auth.
 */
function devHeaders() {
  const userId = localStorage.getItem(DEV_USER_ID_STORAGE_KEY);
  if (!userId) return {};
  return { "x-dev-user-id": userId };
}

/**
 * Make an authenticated request to the backend API.
 *
 * @param {string} path - URL path (e.g. "/api/sites")
 * @param {object} [options] - fetch options merged into the request
 * @returns {Promise<Response>} raw fetch Response
 *
 * Every request is sent with credentials: "include" so the ALB auth cookie
 * is forwarded in production.  In dev, the x-dev-user-id header is added.
 */
export async function apiClient(path, options = {}) {
  const url = `${BASE_URL}${path}`;

  const headers = {
    "Content-Type": "application/json",
    ...devHeaders(),
    ...options.headers,
  };

  return fetch(url, {
    ...options,
    headers,
    credentials: "include",
  });
}

/**
 * Convenience: JSON decode a successful response.
 * Throws ApiError on non-OK status codes.
 */
export async function apiJson(path, options = {}) {
  const response = await apiClient(path, options);
  const body = await response.json();

  if (!response.ok) {
    const err = new Error(body?.error?.message || response.statusText);
    err.code = body?.error?.code || "UNKNOWN";
    err.status = response.status;
    throw err;
  }

  return body.data;
}

/** Set the dev identity in localStorage.  Used by LoginView in dev mode. */
export function setDevIdentity(userId) {
  localStorage.setItem(DEV_USER_ID_STORAGE_KEY, userId);
}

/** Clear the dev identity. */
export function clearDevIdentity() {
  localStorage.removeItem(DEV_USER_ID_STORAGE_KEY);
}

/** Check whether a dev identity is configured. */
export function hasDevIdentity() {
  return !!localStorage.getItem(DEV_USER_ID_STORAGE_KEY);
}
