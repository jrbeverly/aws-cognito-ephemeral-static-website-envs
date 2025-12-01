// ==============================================================================
// router/index.js — Vue Router configuration
//
// Routes:
//   /login     — Unauthenticated login page (Cognito redirect + dev mode)
//   /          — Authenticated landing page (site list, upload options)
//
// The navigation guard calls the backend to verify the session.  If the
// user is not authenticated, they are redirected to /login.
//
// In production the ALB handles Cognito authentication — the browser
// session carries the ALB auth cookie.  In local development the backend
// accepts x-dev-user-id headers set via the login page.
// ==============================================================================

import { createRouter, createWebHistory } from "vue-router";
import { apiClient } from "../api/client";

import HomeView from "../views/HomeView.vue";
import LoginView from "../views/LoginView.vue";
import UploadView from "../views/UploadView.vue";

const routes = [
  {
    path: "/login",
    name: "login",
    component: LoginView,
    meta: { requiresAuth: false },
  },
  {
    path: "/",
    name: "home",
    component: HomeView,
    meta: { requiresAuth: true },
  },
  {
    path: "/sites/:siteId/upload",
    name: "upload",
    component: UploadView,
    meta: { requiresAuth: true },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

// ---------------------------------------------------------------------------
// Navigation guard — verify the session before entering authenticated routes
//
// Calls GET /api/sites (an authenticated endpoint).  A 401 response means
// the session is invalid or expired — redirect to /login.
//
// Other errors (network, 500) are treated as a valid session since the
// request reached the backend and got a non-401 response — the error is
// something else, not an auth problem.
// ---------------------------------------------------------------------------
router.beforeEach(async (to, _from, next) => {
  if (to.meta.requiresAuth !== true) {
    return next();
  }

  try {
    const response = await apiClient("/api/sites");
    if (response.status === 401) {
      return next({ name: "login" });
    }
    // Any other response (200, 500, network error) — proceed; auth is not the issue.
    return next();
  } catch {
    // Network error — the backend may not be running.  In dev this is
    // expected; let the user through so they can configure dev identity.
    return next();
  }
});

export default router;
