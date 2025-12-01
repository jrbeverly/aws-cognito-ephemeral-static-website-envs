#!/bin/sh
# ==============================================================================
# docker-entrypoint.sh — Bridge env vars then delegate to upstream entrypoint
#
# The upstream nginx-s3-gateway image has ENTRYPOINT /docker-entrypoint.sh.
# We rename the original to /docker-entrypoint-upstream.sh and replace it
# with this wrapper.  This wrapper bridges ECS-supplied environment variable
# names (S3_BUCKET, AWS_REGION) to the upstream-named equivalents
# (S3_BUCKET_NAME, S3_REGION, S3_SERVER) in the CURRENT process environment
# so that both the upstream entrypoint logic and the template-processing
# step (20-envsubst-on-templates.sh) see the correct values.
#
# Reference: VISION.md §2.4, §8.6
# ==============================================================================

set -e

# ------------------------------------------------------------------------------
# Map legacy ECS variable names to upstream names.
# If the upstream name is already set directly, it takes precedence.
# ------------------------------------------------------------------------------

# S3_BUCKET → S3_BUCKET_NAME
if [ -z "${S3_BUCKET_NAME:-}" ] && [ -n "${S3_BUCKET:-}" ]; then
    export S3_BUCKET_NAME="${S3_BUCKET}"
fi

# AWS_REGION → S3_REGION
if [ -z "${S3_REGION:-}" ] && [ -n "${AWS_REGION:-}" ]; then
    export S3_REGION="${AWS_REGION}"
fi

# ------------------------------------------------------------------------------
# Derive S3_SERVER from S3_REGION.
# ------------------------------------------------------------------------------
if [ -z "${S3_SERVER:-}" ]; then
    export S3_SERVER="s3.${S3_REGION}.amazonaws.com"
fi

# ------------------------------------------------------------------------------
# Ensure index-page serving is enabled (VISION.md §8.6).
# ------------------------------------------------------------------------------
if [ -z "${PROVIDE_INDEX_PAGE:-}" ]; then
    export PROVIDE_INDEX_PAGE="1"
fi

# ------------------------------------------------------------------------------
# Recomputation of derived upstream variables.
#
# The upstream docker-entrypoint.sh computes S3_UPSTREAM and S3_HOST_HEADER
# inline (before running /docker-entrypoint.d/ scripts).  Since we may have
# changed S3_BUCKET_NAME or S3_SERVER above, we recompute these now so the
# upstream's computed values are correct for template envsubst.
#
# This logic mirrors the upstream docker-entrypoint.sh exactly.
# ------------------------------------------------------------------------------
if [ "${S3_STYLE}" = "virtual-v2" ]; then
    export S3_UPSTREAM="${S3_BUCKET_NAME}.${S3_SERVER}:${S3_SERVER_PORT}"
    export S3_HOST_HEADER="${S3_BUCKET_NAME}.${S3_SERVER}:${S3_SERVER_PORT}"
elif [ "${S3_STYLE}" = "path" ]; then
    export S3_UPSTREAM="${S3_SERVER}:${S3_SERVER_PORT}"
    export S3_HOST_HEADER="${S3_SERVER}:${S3_SERVER_PORT}"
else
    export S3_UPSTREAM="${S3_SERVER}:${S3_SERVER_PORT}"
    export S3_HOST_HEADER="${S3_BUCKET_NAME}.${S3_SERVER}"
fi

# Delegate to the upstream entrypoint.
exec /docker-entrypoint-upstream.sh "$@"
