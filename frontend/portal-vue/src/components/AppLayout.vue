<!--
// ==============================================================================
// AppLayout.vue — Application shell with navigation
//
// Wraps authenticated views with a navigation bar and a slot for content.
// Shows the current user identity when available.
//
// VISION.md §6.2 — The portal is served from a separate origin from hosted
// content, so all navigation stays within the portal.
// ==============================================================================
-->
<template>
  <v-layout>
    <!-- Navigation bar -->
    <v-app-bar color="primary" density="compact">
      <v-app-bar-title>
        <v-icon class="mr-2">mdi-cloud-upload</v-icon>
        Site Manager
      </v-app-bar-title>

      <v-spacer />

      <v-btn
        variant="text"
        prepend-icon="mdi-home"
        :to="{ name: 'home' }"
      >
        Dashboard
      </v-btn>

      <v-btn
        variant="text"
        prepend-icon="mdi-logout"
        @click="onLogout"
      >
        Sign Out
      </v-btn>
    </v-app-bar>

    <!-- Page content -->
    <v-main>
      <slot />
    </v-main>
  </v-layout>
</template>

<script setup>
import { useRouter } from "vue-router";
import { clearDevIdentity } from "../api/client";

const router = useRouter();

function onLogout() {
  clearDevIdentity();
  router.push({ name: "login" });
}
</script>
