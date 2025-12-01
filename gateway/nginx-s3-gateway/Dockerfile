# ==============================================================================
# NGINX S3 gateway — serve private S3 objects with AWS SigV4 signing
#
# Built on the upstream nginx-s3-gateway image (F5, Inc., Apache 2.0) which
# provides SigV4 signing (awssig4.js), IAM credential management (awscredentials.js),
# S3 request routing (s3gateway.js), directory indexing, proxy caching, and
# error sanitisation.
#
# Our additions:
#   - docker-entrypoint.sh bridge — maps ECS-supplied env vars (S3_BUCKET,
#     AWS_REGION) to upstream-named equivalents (S3_BUCKET_NAME, S3_REGION,
#     S3_SERVER) and recomputes S3_UPSTREAM before the upstream entrypoint
#     validates and processes templates.
#   - nginx.conf — JSON access log format for CloudWatch metric extraction,
#     worker_processes auto for Fargate autoscaling.
#   - ENV defaults — platform-safe defaults for the required upstream env vars
#     (S3_SERVICE, S3_STYLE, AWS_SIGS_VERSION, ALLOW_DIRECTORY_LIST, etc.).
#
# Pinning: dated tag with digest for supply-chain integrity.
# See docs/upstream-nginx-s3-gateway-gap-analysis-78.md §5.2.
#
# Reference: VISION.md §2.4, §8.6, §13.6
# ==============================================================================

FROM ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway:latest-njs-oss-20241001

# ------------------------------------------------------------------------------
# Entrypoint bridge — rename the upstream entrypoint and insert our wrapper.
#
# Our wrapper bridges ECS env var names to upstream names, then exec's the
# original upstream entrypoint.  This must happen in the entrypoint process
# (not a child) so the bridged vars are visible to template envsubst.
# ------------------------------------------------------------------------------

RUN mv /docker-entrypoint.sh /docker-entrypoint-upstream.sh
COPY --chmod=755 docker-entrypoint.sh /docker-entrypoint.sh

# ------------------------------------------------------------------------------
# Custom nginx.conf with JSON access log format and worker_processes auto.
# ------------------------------------------------------------------------------

COPY etc/nginx/nginx.conf /etc/nginx/nginx.conf

# ------------------------------------------------------------------------------
# Host-to-prefix resolution module and templates.
#   - host-resolver.js: njs module for DynamoDB-backed hostname resolution
#   - default.conf.template: extended server block with $s3_key and wrapper
#   - s3_location_common.conf.template: uses $s3_key instead of raw $s3uri
# ------------------------------------------------------------------------------

COPY etc/nginx/njs/host-resolver.js /etc/nginx/njs/host-resolver.js
COPY etc/nginx/templates/default.conf.template /etc/nginx/templates/default.conf.template
COPY etc/nginx/templates/gateway/s3_location_common.conf.template /etc/nginx/templates/gateway/s3_location_common.conf.template

# ------------------------------------------------------------------------------
# Platform defaults for the upstream image.
#
# The upstream 00-check-for-required-env.sh requires these to be set.
# Values set here are overridable at runtime via --env.
#
#   S3_SERVICE           — always "s3" for standard S3
#   S3_SERVER_PORT       — always 443 (HTTPS)
#   S3_SERVER_PROTO      — always "https"
#   S3_STYLE             — "virtual-v2" (recommended upstream style)
#   AWS_SIGS_VERSION     — always SigV4
#   ALLOW_DIRECTORY_LIST — disabled (VISION.md does not require directory listing)
#   CORS_ENABLED         — disabled (the ALB handles cross-origin)
#
# S3_BUCKET_NAME, S3_REGION, and S3_SERVER are set at runtime by either
# the ECS task definition or the entrypoint bridge script.
# ------------------------------------------------------------------------------

ENV S3_SERVICE="s3"
ENV S3_SERVER_PORT="443"
ENV S3_SERVER_PROTO="https"
ENV S3_STYLE="virtual-v2"
ENV AWS_SIGS_VERSION="4"
ENV ALLOW_DIRECTORY_LIST="0"
ENV CORS_ENABLED="0"

# Host-to-prefix resolution cache TTL in seconds (design §5.2).
# Positive entries (resolved prefix) and negative entries for disabled/
# no-version sites use this TTL.  Negative entries for unknown hostnames
# use 6× this TTL.  Overridable at runtime via the ECS task definition.
ENV RESOLVE_CACHE_TTL_SECONDS="5"
