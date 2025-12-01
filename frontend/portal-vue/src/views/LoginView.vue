<!--
// ==============================================================================
// LoginView.vue — Authentication entry point
//
// Two modes:
//
//   Production (Cognito):
//     The ALB intercepts unauthenticated requests and redirects to Cognito.
//     The browser follows the redirect, authenticates, and returns with an
//     ALB auth cookie.  The page reload is enough — the user is now
//     authenticated and the router guard passes.
//
//     This page shows a "Redirecting to login..." message and provides a
//     manual link to trigger the Cognito flow.
//
//   Local development:
//     No Cognito is available.  The user enters a dev user ID (which maps
//     to a Cognito sub in the backend's dev-header path).  The value is
//     stored in localStorage and sent as x-dev-user-id on every API call.
//
// VISION.md §5.1 — Portal entry; VISION.md §8.8 — authenticated sessions.
// ==============================================================================
-->
<template>
  <v-app>
    <v-container class="fill-height d-flex align-center justify-center">
      <v-card max-width="480" class="pa-4" elevation="3">
        <v-card-title class="text-h4 mb-2">Site Manager</v-card-title>
        <v-card-subtitle class="mb-4">
          {{ isDevMode ? "Configure a dev identity to bypass Cognito" : "Sign in to manage your sites" }}
        </v-card-subtitle>

        <v-card-text>
          <!-- Production: Cognito redirect -->
          <v-btn
            v-if="!isDevMode"
            color="primary"
            size="large"
            block
            class="mb-4"
            @click="triggerCognitoLogin"
          >
            Sign in with Cognito
          </v-btn>

          <!-- Dev mode: user ID entry -->
          <v-form v-if="isDevMode" @submit.prevent="setDevIdentity">
            <v-text-field
              v-model="userId"
              label="Dev User ID (Cognito sub)"
              hint="e.g. 'sub-alice' or 'my-user-id'"
              variant="outlined"
              required
              autofocus
              class="mb-2"
            />
            <v-btn
              color="primary"
              size="large"
              block
              type="submit"
              :disabled="!userId.trim()"
            >
              Continue as Dev User
            </v-btn>
          </v-form>

          <div class="text-center mt-4">
            <v-btn
              variant="text"
              size="small"
              @click="isDevMode = !isDevMode"
            >
              {{ isDevMode ? "Use Cognito login" : "Developer mode" }}
            </v-btn>
          </div>
        </v-card-text>
      </v-card>
    </v-container>
  </v-app>
</template>

<script setup>
import { ref } from "vue";
import { useRouter } from "vue-router";
import { setDevIdentity as storeDevIdentity } from "../api/client";

const router = useRouter();

const isDevMode = ref(false);
const userId = ref("");

/**
 * Trigger Cognito authentication by navigating to a path protected by
 * the ALB.  When the ALB sees no auth cookie, it redirects to Cognito.
 *
 * In local development this has no effect — the ALB is not running.
 * The dev header flow is the local alternative.
 */
function triggerCognitoLogin() {
  // Navigating to the home page triggers the ALB's Cognito authentication
  // if the user has no session.  The ALB redirects to Cognito, and after
  // login the user returns to this application.
  window.location.href = "/";
}

/**
 * Store the dev identity and navigate to the home page.
 * The router guard will retry the API call and pass this time.
 */
function setDevIdentity() {
  storeDevIdentity(userId.value.trim());
  router.push({ name: "home" });
}
</script>
