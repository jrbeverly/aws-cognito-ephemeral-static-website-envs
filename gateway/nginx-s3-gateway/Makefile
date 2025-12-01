# ==============================================================================
# Makefile — NGINX S3 gateway container image
#
# Builds the gateway image on top of the upstream nginx-s3-gateway (F5, Inc.,
# Apache 2.0).  The gateway serves objects from private S3 using AWS SigV4
# signing against the ECS task IAM role.  No static credentials, no public S3.
#
# Usage:
#   make build                          Build the Docker image
#   make push                           Push to the configured registry
#   make lint                           Validate image (hadolint)
# ==============================================================================

REGISTRY   ?= 000000000000.dkr.ecr.us-east-1.amazonaws.com
IMAGE_NAME ?= nginx-s3-gateway
TAG        ?= latest

IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(TAG)

# ------------------------------------------------------------------------------
# Build
# ------------------------------------------------------------------------------

.PHONY: build
build:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[build] docker not found — skipping (run in devcontainer for full build)"; \
	else \
		docker build \
			--tag $(IMAGE) \
			--tag $(IMAGE_NAME):$(TAG) \
			.; \
	fi

# ------------------------------------------------------------------------------
# Push
# ------------------------------------------------------------------------------

.PHONY: push
push: build
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[push] docker not found — skipping"; \
	else \
		docker push $(IMAGE); \
	fi

# ------------------------------------------------------------------------------
# Lint — validate the Dockerfile with hadolint
# ------------------------------------------------------------------------------

.PHONY: lint
lint:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[lint] docker not found — skipping (run in devcontainer for full lint)"; \
	else \
		docker run --rm -i hadolint/hadolint /bin/hadolint --failure-threshold error - < Dockerfile; \
	fi
