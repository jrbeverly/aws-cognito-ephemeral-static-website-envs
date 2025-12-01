// ==============================================================================
// vite.config.js — Vite build configuration for the Vue portal
//
// Development:
//   - Proxies /api requests to the Go backend (localhost:8080) so the frontend
//     can call the backend with the same origin during local development.
//   - Proxies /health for ALB health-check simulation.
//
// Production:
//   - The portal is served from a separate origin from hosted content
//     (VISION.md §6.2).  API calls go to the same origin — the ALB routes
//     /api/* to the backend.
// ==============================================================================

import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import vuetify from "vite-plugin-vuetify";

export default defineConfig({
  plugins: [
    vue(),
    vuetify({ autoImport: true }),
  ],

  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/health": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },

  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
