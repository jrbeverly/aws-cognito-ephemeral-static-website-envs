# Validation & Publishing Worker

Serverless worker that validates uploaded content and publishes approved versions.

## Responsibility

- Verify upload ownership against the authenticated user
- Extract and validate zip files (path traversal, file count, size limits, allowed types)
- Require `index.html` after normalization (with `public/` directory promotion for Hugo output)
- Publish normalized content into immutable versioned S3 prefixes
- Update active site version atomically through metadata

## Boundaries

- Reads from staging, writes to published prefixes — never the reverse
- Must not serve traffic directly
- Failed validation must not update the active published site

## Technology

Written in Go, deployed as AWS Lambda functions.
See `VISION.md` §8.5 for full validation and publishing requirements.

## Structure

```
workers/site-publisher/
├── cmd/worker/main.go              # Lambda entry point (orchestrates S3 + DynamoDB)
└── internal/validate/
    ├── validate.go                 # Core validation logic (zip, single-file, paste)
    ├── zip.go                      # Safe zip extraction with path-traversal protection
    └── validate_test.go            # Comprehensive unit tests
```

## Validation

The `internal/validate` package implements all validation behaviours from
`VISION.md` §8.5:

- Safe zip extraction with path-traversal, absolute-path, and symlink rejection
- Compressed and uncompressed size limits
- File count limit
- Blocked extension list (executables, server-side scripts, shell scripts)
- `index.html` requirement after `public/` directory normalization
- Single-file and pasted-HTML candidates flow through the same validator

### Failure messages (VISION.md §5.4)

Each rejected upload produces a clear, actionable error:
- missing `index.html`
- zip could not be extracted
- file count exceeds allowed limit
- total uncompressed size exceeds allowed limit
- unsupported file type
- path traversal detected

### Configuration

`validate.DefaultConfig()` returns production-standard thresholds:
- Max zip compressed: 100 MiB
- Max single HTML: 5 MiB
- Max uncompressed: 150 MiB
- Max files: 1000

Callers may supply a custom `validate.Config` for testing or alternate policies.

## Build

```sh
cd workers/site-publisher && go build ./cmd/worker/
```

Or from the repo root:

```sh
make build-workers
```

## Test

```sh
cd workers/site-publisher && go test ./...
```
