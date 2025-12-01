// ==============================================================================
// main.js — Vue application bootstrap
//
// Creates the Vue app with Vuetify and Vue Router, then mounts it.
// No AWS credentials are embedded — all AWS interactions go through the
// backend API (VISION.md §8.1, §8.8).
// ==============================================================================

import { createApp } from "vue";
import { createVuetify } from "vuetify";
import "vuetify/styles";

import App from "./App.vue";
import router from "./router";

const vuetify = createVuetify({
  defaults: {
    VContainer: {
      fluid: true,
    },
  },
});

const app = createApp(App);
app.use(vuetify);
app.use(router);
app.mount("#app");
