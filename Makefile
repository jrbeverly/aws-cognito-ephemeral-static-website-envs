# ==============================================================================
# Makefile — Top-level build, test, lint, and validation targets
#
# All CI workflows delegate to these targets so that local development, the
# devcontainer, and Gitea Actions share one entry point.
#
# Each target that requires an external tool checks for it first and emits a
# clear skip message when the tool is unavailable.  This keeps the Makefile
# portable across the devcontainer (full toolchain) and bare CI runners.
#
# Usage:
#   make help                   Show this help message
#   make build                  Build all components
#   make test                   Run all unit tests
#   make lint                   Run all linters
#   make validate               Aggregate PR checks (test + lint + Terraform validate)
#   make clean                  Remove all build and packaging output
#
# Local development (see docs/local-development.md):
#   make local-env              Start local services (LocalStack DynamoDB)
#   make seed-local             Seed LocalStack DynamoDB with test fixtures
#   make run-backend            Start the Go backend locally
#   make run-frontend           Start the Vue portal dev server
#   make test-integration       Run Go tests against LocalStack DynamoDB
# ==============================================================================

# ------------------------------------------------------------------------------
# Help — self-documenting targets
# ------------------------------------------------------------------------------

.PHONY: help
help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build targets:"
	@echo "  build              Build all components"
	@echo "  build-backend      Build Go backend API Lambda package"
	@echo "  build-frontend     Build the Vue portal frontend"
	@echo "  build-gateway      Build the NGINX S3 gateway Docker image"
	@echo "  build-workers      Build worker Lambda packages (not yet implemented)"
	@echo "  build-examples     Build Hugo example sites"
	@echo "  package-examples   Package built examples into uploadable artifacts"
	@echo ""
	@echo "Test targets:"
	@echo "  test               Run all unit tests"
	@echo "  test-backend       Run Go unit tests"
	@echo "  test-frontend      Run frontend tests (not yet implemented)"
	@echo "  test-workers       Run worker unit tests"
	@echo ""
	@echo "Lint targets:"
	@echo "  lint               Run all linters"
	@echo "  lint-terraform     Run terraform fmt check"
	@echo "  lint-gateway       Lint the gateway Dockerfile"
	@echo ""
	@echo "Validation:"
	@echo "  validate           Aggregate PR checks (test + lint + Terraform validate)"
	@echo ""
	@echo "Local development:"
	@echo "  local-env          Start local services (LocalStack DynamoDB)"
	@echo "  seed-local         Seed LocalStack DynamoDB with test fixtures"
	@echo "  run-backend        Start the Go backend locally"
	@echo "  run-frontend       Start the Vue portal dev server"
	@echo "  test-integration   Run Go tests against LocalStack DynamoDB"
	@echo ""
	@echo "Deploy:"
	@echo "  deploy             Build + terraform apply + push gateway (AUTO_APPROVE=1)"
	@echo "  destroy            Tear down the deployment (AUTO_APPROVE=1)"
	@echo ""
	@echo "Utility:"
	@echo "  clean              Remove all build and packaging output"

# ==============================================================================
# Build — all components
# ==============================================================================

.PHONY: build
build: build-backend build-frontend build-examples build-workers build-gateway ## Build all components

# ------------------------------------------------------------------------------
# Build backend API Lambda package
# ------------------------------------------------------------------------------

.PHONY: build-backend
build-backend: ## Build Go backend API Lambda package
	@if ! command -v go >/dev/null 2>&1; then \
		echo "[build-backend] go not found — skipping (run in devcontainer for full build)"; \
	else \
		scripts/build-backend.sh; \
	fi

# ------------------------------------------------------------------------------
# Build the Vue portal frontend
# ------------------------------------------------------------------------------

.PHONY: build-frontend
build-frontend: ## Build the Vue portal frontend
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "[build-frontend] npm not found — skipping (run in devcontainer for full build)"; \
	else \
		cd frontend/portal-vue && npm install && npm run build; \
	fi

# ------------------------------------------------------------------------------
# Build worker Lambda packages
# ------------------------------------------------------------------------------

.PHONY: build-workers
build-workers: ## Build worker Lambda packages
	@if ! command -v go >/dev/null 2>&1; then \
		echo "[build-workers] go not found — skipping (run in devcontainer for full build)"; \
	else \
		cd workers/site-publisher && go build ./cmd/worker/; \
	fi

# ------------------------------------------------------------------------------
# Build Hugo example sites
# ------------------------------------------------------------------------------

.PHONY: build-examples
build-examples: ## Build Hugo example sites
	@if ! command -v hugo >/dev/null 2>&1; then \
		echo "[build-examples] hugo not found — skipping (run in devcontainer for full build)"; \
	else \
		scripts/build-example-sites.sh; \
	fi

# ------------------------------------------------------------------------------
# Package examples into uploadable artifacts
# ------------------------------------------------------------------------------

.PHONY: package-examples
package-examples: build-examples ## Package built examples into uploadable artifacts
	scripts/package-example-sites.sh

# ------------------------------------------------------------------------------
# Build NGINX S3 gateway Docker image
# ------------------------------------------------------------------------------

.PHONY: build-gateway
build-gateway: ## Build the NGINX S3 gateway Docker image
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[build-gateway] docker not found — skipping (run in devcontainer for full build)"; \
	else \
		cd gateway/nginx-s3-gateway && $(MAKE) build; \
	fi

# ==============================================================================
# Test — all unit tests
# ==============================================================================

.PHONY: test
test: test-backend test-frontend test-workers ## Run all unit tests

# ------------------------------------------------------------------------------
# Test — Go backend
# ------------------------------------------------------------------------------

.PHONY: test-backend
test-backend: ## Run Go unit tests
	@if ! command -v go >/dev/null 2>&1; then \
		echo "[test-backend] go not found — skipping (run in devcontainer for full test suite)"; \
	else \
		cd backend/go-api && go test ./...; \
	fi

# ------------------------------------------------------------------------------
# Test — frontend (not yet implemented)
# ------------------------------------------------------------------------------

.PHONY: test-frontend
test-frontend: ## Run frontend tests (not yet implemented)
	@echo "[test-frontend] No frontend tests configured — skipping"

# ------------------------------------------------------------------------------
# Test — workers
# ------------------------------------------------------------------------------

.PHONY: test-workers
test-workers: ## Run worker unit tests
	@if ! command -v go >/dev/null 2>&1; then \
		echo "[test-workers] go not found — skipping (run in devcontainer for full test suite)"; \
	else \
		cd workers/site-publisher && go test ./...; \
	fi

# ==============================================================================
# Lint — all linters
# ==============================================================================

.PHONY: lint
lint: lint-terraform lint-gateway ## Run all linters

# ------------------------------------------------------------------------------
# Lint — Terraform
# ------------------------------------------------------------------------------

.PHONY: lint-terraform
lint-terraform: ## Run terraform fmt check
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "[lint-terraform] terraform not found — skipping (run in devcontainer for full lint)"; \
	else \
		cd infrastructure/aws && terraform fmt -check -diff -recursive; \
	fi

# ------------------------------------------------------------------------------
# Lint — gateway Dockerfile
# ------------------------------------------------------------------------------

.PHONY: lint-gateway
lint-gateway: ## Lint the gateway Dockerfile with hadolint
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[lint-gateway] docker not found — skipping (run in devcontainer for full lint)"; \
	else \
		cd gateway/nginx-s3-gateway && $(MAKE) lint; \
	fi

# ==============================================================================
# Validate — aggregate PR checks
# ==============================================================================

.PHONY: validate
validate: test lint ## Aggregate PR checks (test + lint + Terraform validate)
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "[validate] terraform not found — skipping terraform validate (run in devcontainer for full validation)"; \
	else \
		cd infrastructure/aws && terraform init -backend=false && terraform validate; \
	fi

# ==============================================================================
# Local development (see docs/local-development.md)
# ==============================================================================

LOCALSTACK_ENDPOINT ?= http://localhost:4566
LOCALSTACK_S3_ENDPOINT ?= http://localhost:4566

.PHONY: local-env
local-env: ## Start local services (LocalStack DynamoDB)
	@echo "[local-env] Checking Docker availability..."
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "[local-env] docker not found — cannot start LocalStack"; \
		exit 1; \
	fi
	@if docker ps --format '{{.Names}}' | grep -q 'localstack'; then \
		echo "[local-env] LocalStack is already running"; \
	else \
		echo "[local-env] Starting LocalStack container..."; \
		docker run -d --rm --name localstack-main \
			-p 4566:4566 \
			-p 4510-4559:4510-4559 \
			localstack/localstack:4.12; \
		echo "[local-env] Waiting for LocalStack to be ready..."; \
		until curl -s "$(LOCALSTACK_ENDPOINT)/_localstack/health" | grep -Eq '"dynamodb": ?"available"'; do \
			sleep 2; \
		done; \
		echo "[local-env] LocalStack is ready (DynamoDB available)"; \
	fi

.PHONY: seed-local
seed-local: ## Seed LocalStack DynamoDB with test fixtures
	@scripts/seed-local-dynamodb.sh "$(LOCALSTACK_ENDPOINT)"

.PHONY: run-backend
run-backend: ## Start the Go backend locally
	@echo "[run-backend] Starting Go backend on :8080..."
	@if go version 2>/dev/null | grep -qE 'go1\.(2[2-9]|[3-9][0-9])'; then \
		cd backend/go-api && \
		AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
		DYNAMODB_ENDPOINT="$(LOCALSTACK_ENDPOINT)" \
		S3_ENDPOINT="$(LOCALSTACK_S3_ENDPOINT)" \
		SITES_BUCKET="local-sites" \
		LOG_LEVEL="debug" \
		go run ./cmd/api/; \
	else \
		echo "[run-backend] go 1.22+ not found — running in golang:1.23 container"; \
		docker run --rm -it --network host \
			-v "$(CURDIR)/backend/go-api":/src -w /src \
			-e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test \
			-e DYNAMODB_ENDPOINT="$(LOCALSTACK_ENDPOINT)" \
			-e S3_ENDPOINT="$(LOCALSTACK_S3_ENDPOINT)" \
			-e SITES_BUCKET="local-sites" \
			-e LOG_LEVEL="debug" \
			golang:1.23 go run ./cmd/api/; \
	fi

.PHONY: run-frontend
run-frontend: ## Start the Vue portal dev server
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "[run-frontend] npm not found — run in the devcontainer"; \
		exit 1; \
	fi
	@echo "[run-frontend] Starting Vue dev server on :5173 (proxies /api to :8080)..."
	@cd frontend/portal-vue && npm install && npm run dev

.PHONY: test-integration
test-integration: ## Run Go tests against LocalStack DynamoDB
	@if ! command -v go >/dev/null 2>&1; then \
		echo "[test-integration] go not found — run in the devcontainer"; \
		exit 1; \
	fi
	@echo "[test-integration] Running integration tests against $(LOCALSTACK_ENDPOINT)..."
	@cd backend/go-api && \
		AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
		DYNAMODB_ENDPOINT="$(LOCALSTACK_ENDPOINT)" \
		GOFLAGS="-count=1" \
		go test ./... -run Integration

# ==============================================================================
# Deploy — portal + backend Lambda + gateway to AWS (see scripts/deploy.sh)
# ==============================================================================

.PHONY: deploy
deploy: ## Build everything, terraform apply, push gateway image (AUTO_APPROVE=1 to skip prompt)
	@scripts/deploy.sh

.PHONY: destroy
destroy: ## Tear down everything deploy created (AUTO_APPROVE=1 to skip prompt)
	@scripts/destroy.sh

# ==============================================================================
# Clean — remove build output and packaged artifacts
# ==============================================================================

.PHONY: clean
clean: ## Remove all build and packaging output
	rm -rf dist/
	rm -rf examples/hugo-basic/public/
	rm -rf examples/hugo-docs/public/
	rm -rf frontend/portal-vue/dist/
