# Portal Frontend (Vue.js)

The management portal for the self-service static site hosting platform.

## Responsibility

- Authenticated landing page and site management UI
- Upload flows: zip archive, single `index.html` file, and pasted HTML
- Publish status and validation error display
- Site deletion and replacement actions

## Boundaries

- Must not embed AWS credentials
- Uploads use backend-issued presigned URLs — never direct S3 access
- Served from a separate origin from hosted user sites per `VISION.md` §6.2

## Technology

Built with Vue 3, Vuetify, and Vite. See `VISION.md` §8.1 for full portal
requirements.

## Development

```sh
# Install dependencies and start the dev server (with HMR)
npm install
npm run dev

# The dev server proxies /api requests to the Go backend on localhost:8080.
# Start the backend first:
#   cd ../../backend/go-api && go run ./cmd/api/
```

### Authentication (local development)

In production the ALB handles Cognito authentication — the browser session
carries an ALB auth cookie.  For local development without Cognito:

1. Open `http://localhost:5173` — the auth guard redirects to `/login`
2. Click **Developer mode**
3. Enter a dev user ID (e.g. `sub-alice`) — this is sent as the
   `x-dev-user-id` header on every API call
4. The backend accepts this header as the authenticated identity

### Build

```sh
# Production build (output in dist/)
npm run build

# Or from the repository root:
make build
```
