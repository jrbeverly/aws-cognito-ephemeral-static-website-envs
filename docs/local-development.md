# Local Development

This document explains how to run and test the entire platform
locally without any AWS deployment. The devcontainer provides all
required tooling (Go, Node.js, npm, Hugo, Docker, LocalStack,
AWS CLI, `http-server`).

## Prerequisites

- The devcontainer is running (see `.devcontainer/README.md`).
- Docker is available (`docker info`).
- Go 1.22+ is on `PATH` (`go version`).
- Node.js 20+ and npm are on `PATH` (`node --version`).
- Hugo extended is on `PATH` (`hugo version`).
- AWS CLI is on `PATH` (`aws --version`).
- `http-server` is on `PATH` (`http-server --version`).

All of the above are pre-installed in the devcontainer.

## Quick start

```sh
# 1. Start LocalStack DynamoDB
make local-env

# 2. Seed test data (tables + fixtures)
make seed-local

# 3. Start the Go backend (terminal 1)
make run-backend

# 4. Start the Vue portal (terminal 2)
make run-frontend
```

The portal is at **http://localhost:5173**.  Use the Login page in
"Developer mode" — enter a dev user ID (e.g., `sub-alice`) to
authenticate against the local backend.

## Services

### LocalStack DynamoDB

The `make local-env` target starts a LocalStack container with
DynamoDB available on port 4566.  No other AWS services are used.

```sh
# Verify DynamoDB is reachable
aws --endpoint-url http://localhost:4566 dynamodb list-tables --region us-east-1
```

### Go backend (`make run-backend`)

Starts the Go API server on `:8080` in local mode (not Lambda).
The server connects to LocalStack for DynamoDB and S3 when
`DYNAMODB_ENDPOINT` and `S3_ENDPOINT` are set.

Environment variables injected by `make run-backend`:

| Variable | Value | Purpose |
|---|---|---|
| `DYNAMODB_ENDPOINT` | `http://localhost:4566` | Connect DynamoDB to LocalStack |
| `S3_ENDPOINT` | `http://localhost:4566` | Connect S3 to LocalStack |
| `SITES_BUCKET` | `local-sites` | Local bucket name |
| `LOG_LEVEL` | `debug` | Verbose logging |

For a custom LocalStack endpoint:

```sh
DYNAMODB_ENDPOINT=http://localhost:4566 \
S3_ENDPOINT=http://localhost:4566 \
SITES_BUCKET=local-sites \
LOG_LEVEL=debug \
  cd backend/go-api && go run ./cmd/api/
```

### Vue portal (`make run-frontend`)

Starts the Vite dev server on `:5173`.  API calls to `/api` and
`/health` are proxied to `localhost:8080` (the Go backend).

The portal supports **developer mode** for local identity — enter
any user ID (e.g., `sub-alice`) on the login page to set the
`x-dev-user-id` header.  The backend accepts this header as the
authenticated identity in local mode.

### NGINX S3 gateway (`make build-gateway`)

The gateway is built on the upstream
[nginx-s3-gateway](https://github.com/nginx/nginx-s3-gateway) image
(F5, Inc., Apache 2.0) with platform additions: host-to-prefix
resolution via DynamoDB-backed njs module, JSON access logs,
security headers, and per-extension Cache-Control.

**Build the image:**

```sh
make build-gateway
# or
cd gateway/nginx-s3-gateway && make build
```

The image is tagged `nginx-s3-gateway:latest` locally.
See the [gateway README](../gateway/nginx-s3-gateway/README.md) for
full architecture and boundaries.

**Lint the Dockerfile:**

```sh
make lint-gateway
# or
cd gateway/nginx-s3-gateway && make lint
```

The lint target runs [hadolint](https://github.com/hadolint/hadolint)
with `--failure-threshold error`.

**Run locally against LocalStack S3** (advanced):

The gateway needs an S3 bucket, AWS credentials, and a DynamoDB
table for host-to-prefix resolution.  For basic local testing,
configure the container with LocalStack endpoints:

```sh
docker run --rm -p 8080:8080 \
  -e S3_BUCKET=local-sites \
  -e AWS_REGION=us-east-1 \
  -e S3_STYLE=path \
  -e AWS_ACCESS_KEY_ID=test \
  -e AWS_SECRET_ACCESS_KEY=test \
  -e S3_SERVER=localhost:4566 \
  -e S3_SERVER_PROTO=http \
  -e S3_SERVER_PORT=4566 \
  -e SITE_METADATA_TABLE_NAME=site_metadata \
  -e DYNAMODB_ENDPOINT=http://localhost:4566 \
  nginx-s3-gateway:latest
```

For most local development the Go backend and `http-server` are
sufficient — the gateway is only needed when testing the full
serving path with hostname resolution.

### Serving built examples

Build and package Hugo example sites for the portal's upload
flows:

```sh
make build-examples
make package-examples
```

This produces `dist/examples/hugo-basic.zip`,
`dist/examples/hugo-docs.zip`, and
`dist/examples/single-index.html`.

Serve the built portal (`dist/`) with `http-server`:

```sh
cd frontend/portal-vue/dist && http-server -p 8081
```

## Running tests

### Unit tests (`make test`)

Unit tests use an in-memory mock DynamoDB and do not require
LocalStack.  They run fast and are suitable for TDD loops.

```sh
make test           # all unit tests
make test-backend   # Go unit tests only
```

The three required test areas per `VISION.md` §16:

| Area | Test file | Lines |
|---|---|---|
| Hostname parsing | `internal/resolver/hostname_test.go` | ~277 |
| Upload authorization | `internal/handler/uploads_test.go` | ~1,031 |
| Validation logic (slug) | `internal/handler/slug_test.go` | ~93 |

### Integration tests (`make test-integration`)

Integration tests run against a real LocalStack DynamoDB instance.
They require LocalStack to be running and seeded.

```sh
make local-env         # ensure LocalStack is running
make seed-local        # create tables and seed fixtures
make test-integration  # run tests tagged with "Integration"
```

Integration tests are tagged with `//go:build integration` or use
`TestIntegration` naming.  They validate that the DynamoDB
repositories work correctly against a real DynamoDB-compatible
endpoint.

## Seed data

`make seed-local` calls `scripts/seed-local-dynamodb.sh` which:

1. Creates the `site_metadata` and `upload_records` DynamoDB tables.
2. Inserts a test user (`sub-alice` / slug `alice`).
3. Inserts a test site (`my-demo-site` under `sub-alice`).
4. Inserts a host mapping.
5. Inserts a completed upload record.

To inspect or modify the seed data directly:

```sh
# Scan a table
aws --endpoint-url http://localhost:4566 dynamodb scan \
  --table-name site_metadata --region us-east-1

# Put a custom item
aws --endpoint-url http://localhost:4566 dynamodb put-item \
  --table-name site_metadata --region us-east-1 \
  --item '{...}'
```

## Manual API testing

With the backend running locally:

```sh
# Health check (unauthenticated)
curl http://localhost:8080/health

# List sites (dev identity via header)
curl -H "x-dev-user-id: sub-alice" http://localhost:8080/api/sites

# Create a site
curl -X POST -H "x-dev-user-id: sub-alice" \
  -H "Content-Type: application/json" \
  -d '{"site_slug":"my-new-site"}' \
  http://localhost:8080/api/sites
```

## Architecture notes

- **No AWS credentials needed locally.**  LocalStack accepts any
  credentials.  The Go SDK sends `dummy/dummy` by default when
  the endpoint is overridden.
- **S3 presigned URLs do not work reliably with all LocalStack
  versions.**  Paste upload (which writes content directly to S3
  via the backend) is the recommended local upload path.
- **Cognito is not available locally.**  The Go backend accepts
  `x-dev-user-id` and `x-dev-user-slug` headers for local
  identity.  The Vue portal sets these via the Login page's
  developer mode.
- **The gateway (NGINX) is built on the upstream
  [nginx-s3-gateway](https://github.com/nginx/nginx-s3-gateway) image.**
  Upload and publish flows are tested through the Go API.  For
  serving content locally, use `http-server` against the Hugo
  example `public/` directories.  To test the gateway locally,
  see the [NGINX S3 gateway](#nginx-s3-gateway-make-build-gateway)
  section above.
