#!/usr/bin/env bash
# ==============================================================================
# build-backend.sh — Build and package the Go serverless backend API
#
# Compiles the Go backend for Lambda (arm64, provided.al2023 runtime) and
# packages it as a zip ready for deployment.
#
# Usage:
#   ./scripts/build-backend.sh              Build and package
#   ./scripts/build-backend.sh --arch amd64  Build for x86_64
#   ./scripts/build-backend.sh --output ./dist  Custom output directory
#
# Output:
#   dist/backend-api.zip   Lambda deployment package
# ==============================================================================

set -euo pipefail

# Resolve repository root from script location
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND_DIR="$REPO_ROOT/backend/go-api"

# Defaults
ARCH="${GOARCH:-arm64}"
OUTPUT_DIR="${REPO_ROOT}/dist"

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    --arch)
      ARCH="$2"; shift 2 ;;
    --output)
      OUTPUT_DIR="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: $0 [--arch arm64|amd64] [--output DIR]"
      exit 0 ;;
    *)
      echo "Unknown option: $1"
      exit 1 ;;
  esac
done

echo "=== Building backend API for AWS Lambda ==="
echo "  Architecture: $ARCH"
echo "  Source:       $BACKEND_DIR"
echo "  Output:       $OUTPUT_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build the Go binary
echo ""
echo "--- Compiling ---"

cd "$BACKEND_DIR"

# go.mod needs Go 1.22+; an older local toolchain (e.g. distro go-1.19) can't build it.
if go version 2>/dev/null | grep -qE 'go1\.(2[2-9]|[3-9][0-9])'; then
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build \
    -ldflags="-s -w" \
    -o "$OUTPUT_DIR/bootstrap" \
    ./cmd/api/
else
  echo "  go 1.22+ not found — compiling in golang:1.23 container"
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH="$ARCH" \
    -v "$BACKEND_DIR":/src -v "$OUTPUT_DIR":/out -w /src \
    golang:1.23 go build -ldflags="-s -w" -o /out/bootstrap ./cmd/api/
fi

echo "  Binary: $OUTPUT_DIR/bootstrap ($(du -h "$OUTPUT_DIR/bootstrap" | cut -f1))"

# Package as Lambda deployment zip
echo ""
echo "--- Packaging ---"

ZIP_FILE="$OUTPUT_DIR/backend-api.zip"

# Remove old zip if present
rm -f "$ZIP_FILE"

# Lambda requires the binary to be named 'bootstrap' at the zip root.  The
# built portal (if present) goes in portal/, which the Lambda serves.
cd "$OUTPUT_DIR"
rm -rf portal
if [ -d "$REPO_ROOT/frontend/portal-vue/dist" ]; then
  cp -r "$REPO_ROOT/frontend/portal-vue/dist" portal
fi
zip -qr "$ZIP_FILE" bootstrap portal 2>/dev/null || zip -q "$ZIP_FILE" bootstrap
rm -rf bootstrap portal

echo "  Package: $ZIP_FILE ($(du -h "$ZIP_FILE" | cut -f1))"
echo ""
echo "=== Build complete ==="
echo "Lambda deployment package: $ZIP_FILE"
