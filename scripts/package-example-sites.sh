#!/bin/sh
# ==============================================================================
# package-example-sites.sh — Package built examples into uploadable artifacts
#
# Builds the three artifact types used as test inputs for the portal:
#   dist/examples/hugo-basic.zip      — Hugo basic site (zip upload)
#   dist/examples/hugo-docs.zip       — Hugo docs-style site (zip upload)
#   dist/examples/single-index.html   — Single-file index (file / paste upload)
#
# Prerequisite: run build-example-sites.sh first (or `make build-examples`).
#
# See VISION.md §10.4.
# ==============================================================================

set -eu

# ------------------------------------------------------------------------------
# Resolve repository root (script may be called from any directory)
# ------------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "[package-example-sites] repository root: ${REPO_ROOT}"

DIST_DIR="${REPO_ROOT}/dist/examples"

# ------------------------------------------------------------------------------
# Create output directory
# ------------------------------------------------------------------------------

mkdir -p "${DIST_DIR}"

# ------------------------------------------------------------------------------
# package_hugo <name>
#
# Zips the contents of an example's public/ directory so that index.html
# sits at the root of the archive.  This matches what the portal expects
# when a user uploads a Hugo-generated zip.
# ------------------------------------------------------------------------------

package_hugo() {
	_name="$1"
	_src_dir="${REPO_ROOT}/examples/${_name}"

	echo ""
	echo "[package-example-sites] === Packaging ${_name} ==="

	if [ ! -d "${_src_dir}/public" ]; then
		echo "[package-example-sites] ERROR: ${_name} has not been built —" >&2
		echo "[package-example-sites]        run build-example-sites.sh first." >&2
		exit 1
	fi

	if [ ! -f "${_src_dir}/public/index.html" ]; then
		echo "[package-example-sites] ERROR: ${_name}/public/index.html not found —" >&2
		echo "[package-example-sites]        the Hugo build may have failed." >&2
		exit 1
	fi

	# Zip the contents of public/ so archive members are root-relative.
	cd "${_src_dir}/public"
	zip -qr "${DIST_DIR}/${_name}.zip" .
	cd "${REPO_ROOT}"

	echo "[package-example-sites] ${_name} packaged: ${DIST_DIR}/${_name}.zip"
}

# ------------------------------------------------------------------------------
# package_single_index
#
# Copies the standalone index.html into the dist directory.  No build step
# is required — this file is already a complete single-page site.
# ------------------------------------------------------------------------------

package_single_index() {
	_name="single-index"
	_src="${REPO_ROOT}/examples/single-index/index.html"

	echo ""
	echo "[package-example-sites] === Packaging ${_name} ==="

	if [ ! -f "${_src}" ]; then
		echo "[package-example-sites] ERROR: source file not found: ${_src}" >&2
		exit 1
	fi

	cp "${_src}" "${DIST_DIR}/${_name}.html"

	echo "[package-example-sites] ${_name} packaged: ${DIST_DIR}/${_name}.html"
}

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------

package_hugo "hugo-basic"
package_hugo "hugo-docs"
package_single_index

echo ""
echo "[package-example-sites] All artifacts packaged:"
echo "  ${DIST_DIR}/hugo-basic.zip"
echo "  ${DIST_DIR}/hugo-docs.zip"
echo "  ${DIST_DIR}/single-index.html"
