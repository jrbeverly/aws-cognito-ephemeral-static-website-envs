#!/bin/sh
# ==============================================================================
# build-example-sites.sh — Build Hugo example sites into static output
#
# Runs `hugo` for each Hugo-based example so the output is ready for
# packaging.  The single-index example needs no build step — it is already a
# standalone HTML file.
#
# Output (under each example directory):
#   examples/hugo-basic/public/   — built Hugo basic site
#   examples/hugo-docs/public/    — built Hugo docs-style site
#
# Requirements: Hugo CLI (already in the devcontainer).
#
# See VISION.md §10.4.
# ==============================================================================

set -eu

# ------------------------------------------------------------------------------
# Resolve repository root (script may be called from any directory)
# ------------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "[build-example-sites] repository root: ${REPO_ROOT}"

# ------------------------------------------------------------------------------
# Shared Hugo flags
#
#   --baseURL /        Serve with root-relative paths — the platform serves
#                      each site at the root of its namespace.
#   --minify           Compress HTML/CSS/JS output for production-like builds.
#   --cleanDestinationDir  Remove stale files from the output directory.
# ------------------------------------------------------------------------------

HUGO_FLAGS="--baseURL / --minify --cleanDestinationDir"

# ------------------------------------------------------------------------------
# build_hugo <name> <source-dir>
# ------------------------------------------------------------------------------

build_hugo() {
	_name="$1"
	_src_dir="$2"

	echo ""
	echo "[build-example-sites] === Building ${_name} ==="

	if [ ! -d "${_src_dir}" ]; then
		echo "[build-example-sites] ERROR: source directory not found: ${_src_dir}" >&2
		exit 1
	fi

	cd "${_src_dir}"
	hugo ${HUGO_FLAGS}
	cd "${REPO_ROOT}"

	echo "[build-example-sites] ${_name} built successfully: ${_src_dir}/public/"
}

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------

build_hugo "hugo-basic"  "${REPO_ROOT}/examples/hugo-basic"
build_hugo "hugo-docs"   "${REPO_ROOT}/examples/hugo-docs"

echo ""
echo "[build-example-sites] All example sites built successfully."
