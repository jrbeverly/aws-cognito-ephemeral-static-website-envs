<!--
// ==============================================================================
// UploadView.vue — Upload content to a site
//
// Three upload modes, all using backend-issued presigned grants:
//   1. Zip archive   — select a .zip, get a presigned URL, PUT to S3 staging
//   2. Single file   — select or drag-and-drop an index.html, same flow
//   3. Paste HTML    — type or paste HTML, backend stores it directly
//
// All destination keys are backend-derived.  The client never provides S3
// keys, owner IDs, or any part of the staging prefix (VISION.md §8.4, §13.2).
//
// After upload, the view polls upload status and shows the final result.
//
// VISION.md §5.2 — Upload flows; §8.1 — Presigned grants; §11 — Paste HTML.
// ==============================================================================
-->
<template>
  <v-container class="py-6">
    <!-- Header -->
    <v-row>
      <v-col>
        <v-btn
          variant="text"
          prepend-icon="mdi-arrow-left"
          :to="{ name: 'home' }"
          class="mb-2"
        >
          Back to Sites
        </v-btn>
        <h1 class="text-h3 mb-1">
          Upload to
          <span v-if="site" class="text-primary">{{ site.site_slug }}</span>
          <v-skeleton-loader v-else type="text" width="120" class="d-inline-block" />
        </h1>
        <p class="text-medium-emphasis">
          Upload a zip archive, a single HTML file, or paste HTML content.
        </p>
      </v-col>
    </v-row>

    <!-- Error banner -->
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

    <!-- Already in a flow — show progress / status -->
    <template v-if="flowState !== 'idle'">
      <v-card variant="outlined" class="pa-6">
        <!-- Progress bar during S3 upload -->
        <template v-if="flowState === 'uploading'">
          <h3 class="text-h6 mb-2">Uploading&hellip;</h3>
          <v-progress-linear
            :model-value="uploadProgress"
            color="primary"
            height="20"
            rounded
            striped
            class="mb-3"
          />
          <p class="text-caption text-medium-emphasis">
            {{ uploadProgress }}% &mdash; uploading
            {{ activeMode === 'zip' ? 'archive' : 'file' }} to staging
          </p>
        </template>

        <!-- Processing / polling status -->
        <template v-if="flowState === 'processing'">
          <h3 class="text-h6 mb-2">Processing&hellip;</h3>
          <v-progress-linear
            indeterminate
            color="primary"
            class="mb-3"
          />
          <p class="text-caption text-medium-emphasis">
            Status: <code>{{ uploadStatus }}</code> &mdash; waiting for validation and publishing
          </p>
        </template>

        <!-- Published -->
        <template v-if="flowState === 'published'">
          <v-icon size="48" color="success" class="mb-2">mdi-check-circle</v-icon>
          <h3 class="text-h6 mb-2">Published</h3>
          <p class="text-medium-emphasis mb-2">
            Your site is now live.
          </p>
          <div
            v-if="publishedHosts.length > 0"
            class="mb-4"
          >
            <div
              v-for="host in publishedHosts"
              :key="host"
              class="mb-2"
            >
              <v-icon size="small" class="mr-1">mdi-link-variant</v-icon>
              <a
                :href="`https://${host}`"
                target="_blank"
                rel="noopener noreferrer"
                class="text-primary"
              >
                https://{{ host }}
              </a>
            </div>
          </div>
          <p v-else class="text-medium-emphasis mb-4">
            No hostnames are configured for this site yet.
          </p>
          <v-btn
            color="primary"
            :to="{ name: 'home' }"
          >
            Back to Dashboard
          </v-btn>
        </template>

        <!-- Failed -->
        <template v-if="flowState === 'failed'">
          <v-icon size="48" color="error" class="mb-2">mdi-alert-circle</v-icon>
          <h3 class="text-h6 mb-2">Upload Failed</h3>
          <template v-if="failureErrors.length > 0">
            <p class="text-medium-emphasis mb-2">
              The following issues were found:
            </p>
            <ul class="text-left mb-4">
              <li
                v-for="(msg, i) in failureErrors"
                :key="i"
                class="mb-1 text-medium-emphasis"
              >
                {{ msg }}
              </li>
            </ul>
          </template>
          <p v-else class="text-medium-emphasis mb-4">
            The upload could not be published. Check your content and try again.
          </p>
          <v-btn
            variant="tonal"
            prepend-icon="mdi-refresh"
            @click="resetFlow"
          >
            Try Again
          </v-btn>
        </template>
      </v-card>
    </template>

    <!-- Mode selection (only when idle) -->
    <template v-if="flowState === 'idle'">
      <v-tabs v-model="activeMode" class="mb-6">
        <v-tab value="zip">Zip Archive</v-tab>
        <v-tab value="file">Single File</v-tab>
        <v-tab value="paste">Paste HTML</v-tab>
      </v-tabs>

      <!-- ==================================================================
           Zip archive upload
           ================================================================== -->
      <v-card v-if="activeMode === 'zip'" variant="outlined">
        <v-card-item>
          <v-card-title>Upload a Zip Archive</v-card-title>
          <v-card-subtitle>
            Select a <code>.zip</code> file containing your static site
            (max 100&nbsp;MiB).  The archive must include an
            <code>index.html</code> at the root.
          </v-card-subtitle>
        </v-card-item>
        <v-card-text>
          <v-file-input
            v-model="zipFile"
            label="Select .zip file"
            accept=".zip,application/zip"
            variant="outlined"
            :rules="[validateZipFile]"
            prepend-icon="mdi-folder-zip"
            show-size
            @update:model-value="zipFileError = null"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            color="primary"
            :disabled="!zipFile || uploading"
            :loading="uploading"
            prepend-icon="mdi-upload"
            @click="uploadZip"
          >
            Upload
          </v-btn>
        </v-card-actions>
      </v-card>

      <!-- ==================================================================
           Single file upload (with drag-and-drop)
           ================================================================== -->
      <v-card v-if="activeMode === 'file'" variant="outlined">
        <v-card-item>
          <v-card-title>Upload an index.html File</v-card-title>
          <v-card-subtitle>
            Select or drag-and-drop a single HTML file (max 5&nbsp;MiB).
          </v-card-subtitle>
        </v-card-item>
        <v-card-text>
          <!-- Drag-and-drop zone -->
          <div
            class="drop-zone pa-6 mb-4 text-center rounded-lg"
            :class="{ 'drop-zone--active': dragOver }"
            @dragenter.prevent="onDragEnter"
            @dragover.prevent="onDragOver"
            @dragleave.prevent="onDragLeave"
            @drop.prevent="onDrop"
          >
            <v-icon size="48" color="grey-lighten-1" class="mb-2">
              mdi-cloud-upload
            </v-icon>
            <p v-if="!singleFile" class="text-medium-emphasis mb-2">
              Drag and drop an HTML file here, or use the file picker below.
            </p>
            <p v-else class="text-primary mb-0">
              {{ singleFile.name }}
              <span class="text-medium-emphasis">
                ({{ formatFileSize(singleFile.size) }})
              </span>
            </p>
          </div>

          <v-file-input
            v-model="singleFile"
            label="Select HTML file"
            accept=".html,.htm,text/html"
            variant="outlined"
            :rules="[validateSingleFile]"
            prepend-icon="mdi-language-html5"
            show-size
            @update:model-value="singleFileError = null"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            color="primary"
            :disabled="!singleFile || uploading"
            :loading="uploading"
            prepend-icon="mdi-upload"
            @click="uploadSingleFile"
          >
            Upload
          </v-btn>
        </v-card-actions>
      </v-card>

      <!-- ==================================================================
           Paste HTML
           ================================================================== -->
      <v-card v-if="activeMode === 'paste'" variant="outlined">
        <v-card-item>
          <v-card-title>Paste HTML Content</v-card-title>
          <v-card-subtitle>
            Paste or type your HTML content below (max 5&nbsp;MiB).
            The content is uploaded as <code>index.html</code>.
          </v-card-subtitle>
        </v-card-item>
        <v-card-text>
          <v-textarea
            v-model="pastedHtml"
            label="HTML Content"
            variant="outlined"
            rows="16"
            auto-grow
            :rules="[validatePastedHtml]"
            hint="Paste your complete HTML document here."
            persistent-hint
          />
          <p class="text-caption text-medium-emphasis text-right mt-1">
            {{ pastedHtml.length.toLocaleString() }} / 5,242,880 characters
          </p>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            color="primary"
            :disabled="!pastedHtml.trim() || uploading"
            :loading="uploading"
            prepend-icon="mdi-content-paste"
            @click="uploadPaste"
          >
            Upload HTML
          </v-btn>
        </v-card-actions>
      </v-card>
    </template>
  </v-container>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from "vue";
import { useRoute } from "vue-router";
import { getSite } from "../api/sites";
import {
  requestUploadGrant,
  completeUpload,
  getUploadStatus,
  pasteUpload,
} from "../api/uploads";

// ---------------------------------------------------------------------------
// Route & site
// ---------------------------------------------------------------------------

const route = useRoute();
const siteId = computed(() => route.params.siteId);
const site = ref(null);
const error = ref(null);

// Published hostnames derived from the site record.
const publishedHosts = computed(() => site.value?.hostnames ?? []);

// ---------------------------------------------------------------------------
// Upload mode
// ---------------------------------------------------------------------------
const activeMode = ref("zip"); // "zip" | "file" | "paste"

// ---------------------------------------------------------------------------
// Flow state machine: idle → uploading → processing → published | failed
// ---------------------------------------------------------------------------
const flowState = ref("idle"); // "idle" | "uploading" | "processing" | "published" | "failed"
const uploadProgress = ref(0);
const uploadStatus = ref(null);
const failureErrors = ref([]);
const uploading = ref(false);

// ---------------------------------------------------------------------------
// Zip upload state
// ---------------------------------------------------------------------------
const zipFile = ref(null);
const zipFileError = ref(null);

// ---------------------------------------------------------------------------
// Single file upload state
// ---------------------------------------------------------------------------
const singleFile = ref(null);
const singleFileError = ref(null);
const dragOver = ref(false);

// ---------------------------------------------------------------------------
// Paste HTML state
// ---------------------------------------------------------------------------
const pastedHtml = ref("");

// ---------------------------------------------------------------------------
// Size limits (server-enforced; client validates for better UX)
// ---------------------------------------------------------------------------
const MAX_ZIP_BYTES = 100 * 1024 * 1024;   // 100 MiB
const MAX_SINGLE_BYTES = 5 * 1024 * 1024;  // 5 MiB
const MAX_PASTE_BYTES = 5 * 1024 * 1024;   // 5 MiB

// ---------------------------------------------------------------------------
// Polling control
// ---------------------------------------------------------------------------
let pollTimer = null;

onMounted(async () => {
  try {
    site.value = await getSite(siteId.value);
  } catch (err) {
    error.value = `Failed to load site: ${err.message || "Unknown error"}`;
  }
});

onUnmounted(() => {
  stopPolling();
});

// ---------------------------------------------------------------------------
// File validators
// ---------------------------------------------------------------------------

function validateZipFile(file) {
  if (!file) return true;
  if (zipFileError.value) return zipFileError.value;
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return "Only .zip files are accepted";
  }
  if (file.size > MAX_ZIP_BYTES) {
    return `File is too large (max ${formatFileSize(MAX_ZIP_BYTES)})`;
  }
  return true;
}

function validateSingleFile(file) {
  if (!file) return true;
  if (singleFileError.value) return singleFileError.value;
  if (file.size > MAX_SINGLE_BYTES) {
    return `File is too large (max ${formatFileSize(MAX_SINGLE_BYTES)})`;
  }
  return true;
}

function validatePastedHtml(value) {
  if (!value) return true;
  if (new Blob([value]).size > MAX_PASTE_BYTES) {
    return `Content is too large (max ${formatFileSize(MAX_PASTE_BYTES)})`;
  }
  return true;
}

// ---------------------------------------------------------------------------
// Drag-and-drop handlers
// ---------------------------------------------------------------------------

function onDragEnter() {
  dragOver.value = true;
}

function onDragOver() {
  dragOver.value = true;
}

function onDragLeave() {
  dragOver.value = false;
}

function onDrop(event) {
  dragOver.value = false;
  const files = event.dataTransfer?.files;
  if (!files || files.length === 0) return;
  const file = files[0];
  if (file.size > MAX_SINGLE_BYTES) {
    singleFileError.value = `File is too large (max ${formatFileSize(MAX_SINGLE_BYTES)})`;
    return;
  }
  singleFile.value = file;
  singleFileError.value = null;
}

// ---------------------------------------------------------------------------
// Upload flows
// ---------------------------------------------------------------------------

/**
 * Upload a zip file via presigned grant.
 */
async function uploadZip() {
  if (!zipFile.value) return;
  uploading.value = true;
  error.value = null;

  try {
    const grant = await requestUploadGrant(siteId.value, "zip");
    await uploadToS3(grant.presigned_url, zipFile.value, "application/zip");
    await completeUpload(grant.upload_id);
    startPolling(grant.upload_id);
  } catch (err) {
    error.value = err.message || "Upload failed";
    flowState.value = "failed";
    failureErrors.value = [err.message || "Upload failed"];
  } finally {
    uploading.value = false;
  }
}

/**
 * Upload a single HTML file via presigned grant.
 */
async function uploadSingleFile() {
  if (!singleFile.value) return;
  uploading.value = true;
  error.value = null;

  try {
    const grant = await requestUploadGrant(siteId.value, "index");
    await uploadToS3(grant.presigned_url, singleFile.value, "text/html");
    await completeUpload(grant.upload_id);
    startPolling(grant.upload_id);
  } catch (err) {
    error.value = err.message || "Upload failed";
    flowState.value = "failed";
    failureErrors.value = [err.message || "Upload failed"];
  } finally {
    uploading.value = false;
  }
}

/**
 * Paste HTML — the backend stores it directly in S3 staging.  No presigned
 * grant or separate S3 PUT is needed.
 */
async function uploadPaste() {
  if (!pastedHtml.value.trim()) return;
  uploading.value = true;
  error.value = null;

  try {
    const result = await pasteUpload(siteId.value, pastedHtml.value.trim());
    // Paste uploads are created in "uploaded" state (content already staged).
    startPolling(result.upload_id);
  } catch (err) {
    error.value = err.message || "Upload failed";
    flowState.value = "failed";
    failureErrors.value = [err.message || "Upload failed"];
  } finally {
    uploading.value = false;
  }
}

// ---------------------------------------------------------------------------
// S3 presigned-URL upload with progress via XMLHttpRequest
//
// fetch() does not expose upload progress events, so we use XMLHttpRequest
// specifically for the S3 PUT.  The presigned URL points directly to S3 —
// the browser makes a cross-origin PUT with the file content.
// ---------------------------------------------------------------------------

function uploadToS3(presignedUrl, file, contentType) {
  flowState.value = "uploading";
  uploadProgress.value = 0;

  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();

    xhr.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) {
        uploadProgress.value = Math.round((event.loaded / event.total) * 100);
      }
    });

    xhr.addEventListener("load", () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve();
      } else {
        let message = `S3 upload failed with status ${xhr.status}`;
        try {
          const body = JSON.parse(xhr.responseText);
          if (body?.message) message = body.message;
        } catch {
          // response wasn't JSON — use the status text
        }
        reject(new Error(message));
      }
    });

    xhr.addEventListener("error", () => {
      reject(new Error("Network error during upload. Check your connection and try again."));
    });

    xhr.addEventListener("abort", () => {
      reject(new Error("Upload was aborted"));
    });

    xhr.open("PUT", presignedUrl);
    xhr.setRequestHeader("Content-Type", contentType);
    xhr.send(file);
  });
}

// ---------------------------------------------------------------------------
// Status polling
// ---------------------------------------------------------------------------

function startPolling(uploadId) {
  flowState.value = "processing";
  uploadStatus.value = "uploaded";
  pollUpload(uploadId);
}

function pollUpload(uploadId) {
  pollTimer = setTimeout(async () => {
    try {
      const status = await getUploadStatus(uploadId);
      uploadStatus.value = status.status;

      if (status.status === "published") {
        flowState.value = "published";
        return; // stop polling
      }

      if (status.status === "failed") {
        flowState.value = "failed";
        const vr = status.validation_result;
        if (vr?.errors && vr.errors.length > 0) {
          failureErrors.value = vr.errors;
        } else {
          failureErrors.value = ["Validation failed — check your content and try again."];
        }
        return; // stop polling
      }

      // Still processing — poll again.
      pollUpload(uploadId);
    } catch (err) {
      // Transient error — keep polling unless we've been unmounted.
      if (pollTimer !== null) {
        pollUpload(uploadId);
      }
    }
  }, 2000); // poll every 2 seconds
}

function stopPolling() {
  if (pollTimer !== null) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function resetFlow() {
  stopPolling();
  flowState.value = "idle";
  uploadProgress.value = 0;
  uploadStatus.value = null;
  failureErrors.value = [];
  uploading.value = false;
  error.value = null;
  zipFile.value = null;
  zipFileError.value = null;
  singleFile.value = null;
  singleFileError.value = null;
  pastedHtml.value = "";
}

function formatFileSize(bytes) {
  if (!bytes) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"];
  let i = 0;
  let size = bytes;
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024;
    i++;
  }
  return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
</script>

<style scoped>
.drop-zone {
  border: 2px dashed #bdbdbd;
  border-radius: 8px;
  transition: border-color 0.2s, background-color 0.2s;
  cursor: pointer;
}

.drop-zone--active {
  border-color: #1976d2;
  background-color: rgba(25, 118, 210, 0.05);
}
</style>
