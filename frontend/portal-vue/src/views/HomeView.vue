<!--
// ==============================================================================
// HomeView.vue — Authenticated landing page
//
// Shows the user's sites with status, hosted URLs, and site lifecycle actions
// (create, delete).  All authorization is server-side — the frontend never
// sends owner IDs or S3 prefixes (VISION.md §8.1, §13.2).
//
// VISION.md §5.1 — Portal entry; §5.3 — Publish result; §12 — Deletion.
// ==============================================================================
-->
<template>
  <v-container class="py-6">
    <v-row>
      <v-col>
        <h1 class="text-h3 mb-2">Your Sites</h1>
        <p class="text-medium-emphasis">
          Manage your static sites — create, upload, publish, and delete.
        </p>
      </v-col>
      <v-col cols="auto" class="d-flex align-center">
        <v-btn
          color="primary"
          prepend-icon="mdi-plus"
          @click="openCreateDialog"
        >
          Create Site
        </v-btn>
      </v-col>
    </v-row>

    <!-- Loading state -->
    <v-row v-if="loading">
      <v-col v-for="n in 3" :key="n" cols="12" sm="6" md="4">
        <v-skeleton-loader type="card" />
      </v-col>
    </v-row>

    <!-- Error state -->
    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      class="mb-4"
      closable
      @click:close="error = null"
    >
      {{ error }}
    </v-alert>

    <!-- Empty state -->
    <v-row v-if="!loading && !error && sites.length === 0">
      <v-col>
        <v-card variant="outlined" class="pa-8 text-center">
          <v-icon size="64" color="grey-lighten-1" class="mb-4">
            mdi-web
          </v-icon>
          <h3 class="text-h5 mb-2">No sites yet</h3>
          <p class="text-medium-emphasis mb-4">
            Create your first site to start publishing static content.
          </p>
          <v-btn
            color="primary"
            prepend-icon="mdi-plus"
            @click="openCreateDialog"
          >
            Create Your First Site
          </v-btn>
        </v-card>
      </v-col>
    </v-row>

    <!-- Site cards -->
    <v-row v-if="!loading && sites.length > 0">
      <v-col
        v-for="site in sites"
        :key="site.site_id"
        cols="12"
        sm="6"
        md="4"
      >
        <v-card :disabled="site.disabled">
          <v-card-item>
            <template #prepend>
              <v-icon :color="site.disabled ? 'grey-lighten-1' : 'primary'">
                mdi-web
              </v-icon>
            </template>
            <v-card-title>
              {{ site.site_slug }}
              <v-chip
                v-if="site.disabled"
                size="x-small"
                color="grey-darken-1"
                class="ml-2"
              >
                Disabled
              </v-chip>
              <v-chip
                v-else
                size="x-small"
                color="success"
                class="ml-2"
              >
                Active
              </v-chip>
            </v-card-title>
            <v-card-subtitle>
              Created {{ formatDate(site.created_at) }}
            </v-card-subtitle>
          </v-card-item>

          <v-card-text>
            <div
              v-for="host in site.hostnames"
              :key="host"
              class="text-caption font-monospace mb-1 d-flex align-center"
            >
              <v-icon size="small" class="mr-1">mdi-link-variant</v-icon>
              <a
                :href="`https://${host}`"
                target="_blank"
                rel="noopener noreferrer"
                class="text-decoration-none"
              >
                {{ host }}
              </a>
            </div>
          </v-card-text>

          <v-card-actions>
            <v-btn
              v-if="!site.disabled"
              variant="text"
              color="primary"
              size="small"
              prepend-icon="mdi-upload"
              :to="{ name: 'upload', params: { siteId: site.site_id } }"
            >
              Upload
            </v-btn>
            <v-spacer />
            <v-btn
              v-if="!site.disabled"
              variant="text"
              color="error"
              size="small"
              :loading="deleting === site.site_id"
              @click="onDelete(site)"
            >
              Delete
            </v-btn>
            <span
              v-else
              class="text-caption text-medium-emphasis mr-3"
            >
              Deleted
            </span>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>

    <!-- Create site dialog -->
    <v-dialog v-model="showCreateDialog" max-width="480">
      <v-card>
        <v-card-title>Create Site</v-card-title>
        <v-card-text>
          <v-alert
            v-if="createError"
            type="error"
            variant="tonal"
            class="mb-4"
            density="compact"
            closable
            @click:close="createError = null"
          >
            {{ createError }}
          </v-alert>

          <v-form @submit.prevent="onCreate">
            <v-text-field
              v-model="newSiteSlug"
              label="Site Slug"
              hint="Lowercase letters, digits, and hyphens.  Max 39 characters."
              variant="outlined"
              required
              autofocus
              :rules="slugRules"
              :error-messages="slugServerErrors"
              class="mb-2"
              @input="slugServerErrors = []"
            />
          </v-form>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showCreateDialog = false">
            Cancel
          </v-btn>
          <v-btn
            color="primary"
            :disabled="!newSiteSlug.trim() || creating"
            :loading="creating"
            @click="onCreate"
          >
            Create
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<script setup>
import { ref, onMounted } from "vue";
import { listSites, createSite, deleteSite } from "../api/sites";

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

const sites = ref([]);
const loading = ref(true);
const error = ref(null);

const showCreateDialog = ref(false);
const newSiteSlug = ref("");
const creating = ref(false);
const createError = ref(null);
const slugServerErrors = ref([]);

const deleting = ref(null); // site_id currently being deleted, or null

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

onMounted(async () => {
  try {
    sites.value = await listSites();
  } catch (err) {
    error.value = err.message || "Failed to load sites";
  } finally {
    loading.value = false;
  }
});

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

function openCreateDialog() {
  createError.value = null;
  slugServerErrors.value = [];
  newSiteSlug.value = "";
  showCreateDialog.value = true;
}

async function onCreate() {
  if (!newSiteSlug.value.trim()) return;
  creating.value = true;
  createError.value = null;
  slugServerErrors.value = [];
  try {
    const site = await createSite(newSiteSlug.value.trim());
    sites.value.push(site);
    newSiteSlug.value = "";
    showCreateDialog.value = false;
  } catch (err) {
    // Distinguish server-side slug validation errors from generic failures.
    const msg = err.message || "Failed to create site";
    if (
      err.status === 400 ||
      (err.code === "VALIDATION_FAILED" && msg.toLowerCase().includes("slug"))
    ) {
      slugServerErrors.value = [msg];
    } else {
      createError.value = msg;
    }
  } finally {
    creating.value = false;
  }
}

async function onDelete(site) {
  deleting.value = site.site_id;
  try {
    await deleteSite(site.site_id);
    // Reflect the soft-delete in the local list — update the site's disabled
    // flag rather than removing it, so the UI matches the backend's soft-delete
    // semantics (VISION.md §12).
    const idx = sites.value.findIndex((s) => s.site_id === site.site_id);
    if (idx !== -1) {
      sites.value[idx] = { ...sites.value[idx], disabled: true };
    }
  } catch (err) {
    error.value = err.message || "Failed to delete site";
  } finally {
    deleting.value = null;
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDate(iso) {
  if (!iso) return "";
  return new Date(iso).toLocaleDateString();
}

const slugRules = [
  (value) => {
    if (!value) return true; // required handled separately
    if (value.length > 39) return "Max 39 characters";
    const pattern = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;
    if (!pattern.test(value))
      return "Only lowercase letters, digits, and hyphens; cannot start or end with a hyphen";
    return true;
  },
];
</script>
