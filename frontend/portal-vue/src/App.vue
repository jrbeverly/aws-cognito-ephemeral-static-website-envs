<!--
// ==============================================================================
// App.vue — Root component
//
// Wraps every view in the Vuetify app shell.  The <router-view> renders
// the current route component.  LoginView is rendered without the
// application layout; authenticated views receive the full navigation shell.
// ==============================================================================
-->
<template>
  <v-app>
    <app-layout v-if="isAuthenticated">
      <router-view />
    </app-layout>
    <router-view v-else />
  </v-app>
</template>

<script setup>
import { computed } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "./components/AppLayout.vue";

const route = useRoute();
const isAuthenticated = computed(() => route.meta?.requiresAuth !== false);
</script>
