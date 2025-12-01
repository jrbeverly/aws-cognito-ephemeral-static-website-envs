# ECS NGINX S3 Gateway

The serving gateway for hosted static sites. Receives authenticated requests
from the ALB and serves content from private S3 buckets.

## Responsibility

- Receive traffic only from the ALB security group
- Resolve request `Host` header to site metadata (owner → site → active version → S3 prefix)
- Fetch and serve files from S3 using platform-controlled AWS IAM permissions
- Serve `index.html` for directory requests
- Return uniform 404 responses for missing sites, disabled sites, or missing files
- Preserve static asset content types and set per-extension cache headers
- Apply baseline security response headers on all content and error responses

## Boundaries

- Must not serve from a local writable filesystem — S3 is the source of truth
- Must not require public S3 access — works with private S3 buckets
- Must be deployable in both public and private modes per `VISION.md` §2.7
- Must not expose NGINX version, S3 bucket names, or object keys to clients

## Architecture

Built on the upstream [`nginx-s3-gateway`](https://github.com/nginx/nginx-s3-gateway)
image (F5, Inc., Apache 2.0) at `ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway:latest-njs-oss-20241001`.

The upstream provides:
- AWS SigV4 request signing (`awssig4.js`)
- IAM credential management from the ECS task metadata endpoint (`awscredentials.js`)
- S3 request routing and content loading (`s3gateway.js`)
- Directory listing with XSLT transformation
- Proxy caching with configurable TTLs
- Error sanitisation (all S3 errors → uniform 404)

Our additions on top of the upstream image:

| Addition | File | Purpose |
|---|---|---|
| Entrypoint bridge | `docker-entrypoint.sh` | Maps ECS env var names (`S3_BUCKET`, `AWS_REGION`) to upstream equivalents (`S3_BUCKET_NAME`, `S3_REGION`, `S3_SERVER`); enables `PROVIDE_INDEX_PAGE` by default |
| JSON access logs | `etc/nginx/nginx.conf` | Structured JSON log format with `escape=json` for CloudWatch metric extraction; `worker_processes auto` for Fargate autoscaling; `js_shared_dict_zone` for host resolution cache |
| Host-to-prefix resolver | `etc/nginx/njs/host-resolver.js` | njs module that resolves `Host` headers to S3 published prefixes via DynamoDB with in-memory caching |
| Extended server config | `etc/nginx/templates/default.conf.template` | Constructs `$s3_key` from resolved prefix + S3 URI; per-extension Cache-Control map; security response headers; enhanced `/health` endpoint; `@gateway_error` location |
| S3 proxy template | `etc/nginx/templates/gateway/s3_location_common.conf.template` | Proxy configuration using `$s3_key` instead of raw `$s3uri`; security headers on all content responses |

The serving path for a hosted-site request:

```
Client → ALB (HTTPS, Cognito auth)
     → ECS Fargate NGINX
       ├── host-resolver.js resolves Host → S3 prefix via DynamoDB GetItem
       ├── $s3_key = <resolved_prefix> + <S3 URI path>
       ├── upstream awssig4.js signs the proxy request
       └── proxy_pass https://<bucket>.s3.<region>.amazonaws.com/$s3_key
     → S3 (private, VPC gateway endpoint)
```

## Configuration Surface

### Required environment variables

Set by the ECS task definition (`infrastructure/aws/modules/ecs-gateway/main.tf`):

| Variable | Purpose |
|---|---|
| `S3_BUCKET_NAME` | S3 bucket for hosted site content (bridged from `S3_BUCKET` by the entrypoint) |
| `S3_REGION` | AWS region for S3 endpoint and DynamoDB host resolution (bridged from `AWS_REGION`) |
| `SITE_METADATA_TABLE_NAME` | DynamoDB table queried by `host-resolver.js` for Host → S3 prefix resolution |

### Optional environment variables

| Variable | Default | Purpose |
|---|---|---|
| `RESOLVE_CACHE_TTL_SECONDS` | `5` | Shared-dict cache TTL for resolved prefixes (positive + disabled/no-version entries). Unknown-hostname negatives use 6× this TTL. |

### Upstream defaults (set in Dockerfile, overridable at runtime)

| Variable | Value | Purpose |
|---|---|---|
| `S3_SERVICE` | `s3` | Standard S3 (not S3 Express) |
| `S3_SERVER_PORT` | `443` | HTTPS |
| `S3_SERVER_PROTO` | `https` | Encrypted transport |
| `S3_STYLE` | `virtual-v2` | Virtual-hosted-style S3 requests |
| `AWS_SIGS_VERSION` | `4` | Signature version |
| `ALLOW_DIRECTORY_LIST` | `0` | Directory listing disabled |
| `CORS_ENABLED` | `0` | CORS handled by ALB |

## Host-to-Prefix Resolution

The gateway resolves incoming `Host` headers to S3 published prefixes at request
time. The resolution chain:

1. `location /` invokes `hostResolver.resolveAndRedirect` (replaces upstream's
   `s3gateway.redirectToS3`)
2. The `Host` header is normalised (lowercased, port stripped)
3. A `js_shared_dict` cache lookup is performed first (zone: `resolve_cache`,
   4 MiB, ~16,000 entries)
4. On cache miss: a SigV4-signed DynamoDB `GetItem` is sent for
   `pk=HOST#<hostname>`, `sk=MAPPING`
5. The resolved item is validated: `userDisabled`, `siteDisabled`,
   `s3PublishedPrefix`, `versionId` are checked
6. The prefix is validated to start with `published/` (defence-in-depth)
7. On success: `$s3_prefix` is set and the request delegates to the upstream
   `s3gateway.redirectToS3`
8. On failure: the request is redirected to `@gateway_error` → 404

All resolution failures — host not found, user/site disabled, no active
version — return identical 404 responses to avoid leaking information about
whether a hostname was ever registered.

Cache TTLs:
- Positive entries (resolved prefix): `RESOLVE_CACHE_TTL_SECONDS` (default 5 s)
- Negative entries (disabled / no version): `RESOLVE_CACHE_TTL_SECONDS` (default 5 s)
- Negative entries (host not found): 6 × `RESOLVE_CACHE_TTL_SECONDS` (default 30 s)

See `docs/design-gateway-host-to-prefix-resolution.md` for the full design.

## Security Headers

Applied on every response path:

| Location | `X-Content-Type-Options` | `X-Frame-Options` | `Referrer-Policy` |
|---|---|---|---|
| Content (`@s3`, `@s3Directory`, `~ /index.html$`) | `nosniff` | `SAMEORIGIN` | `strict-origin-when-cross-origin` |
| Error (`@error404`, `@gateway_error`) | `nosniff` | `DENY` | `no-referrer` |
| Health (`/health`) | None (internal endpoint) | None | None |

## Cache-Control

Per-extension `Cache-Control` headers set via an nginx `map` block:

| File Extension | `Cache-Control` |
|---|---|
| `.css`, `.js`, `.mjs` | `max-age=31536000, immutable` |
| `.woff`, `.woff2`, `.ttf`, `.eot`, `.otf` | `max-age=31536000, immutable` |
| `.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.ico`, `.webp`, `.avif` | `max-age=31536000, immutable` |
| All other (HTML, etc.) | `no-cache` |

## Decommissioned Custom Artifacts

The following artifacts from the original custom gateway have been removed and
replaced by upstream equivalents:

| Removed Artifact | Replaced By | Reason |
|---|---|---|
| `etc/nginx/njs/s3-auth.js` (custom pure-JS SHA-256 + SigV4, ~448 lines) | Upstream `awssig4.js` + njs crypto module | Upstream uses njs built with `--with-crypto` for correct, audited SHA-256 and HMAC |
| `docker-entrypoint.d/40-refresh-aws-credentials.sh` (filesystem credential caching via `aws-credentials.inc`) | Upstream `awscredentials.js` (ECS task metadata endpoint via `ngx.fetch`) | Upstream mechanism is standard, uses the ECS credential endpoint directly |
| `aws-credentials.inc` mechanism (nginx `include`-based credential injection) | Removed entirely — credentials flow through njs variables | Simpler, no filesystem dependency |

**Decommissioning verified**: None of the removed files exist in the repository.
The only custom njs module is `host-resolver.js`. The `docker-entrypoint.d/`
directory has been removed.

## Build and Lint

```sh
# Build the Docker image
make build-gateway          # from repository root
# or
cd gateway/nginx-s3-gateway && make build

# Lint the Dockerfile with hadolint
make lint-gateway           # from repository root
# or
cd gateway/nginx-s3-gateway && make lint

# Push to ECR
cd gateway/nginx-s3-gateway && REGISTRY=<account>.dkr.ecr.<region>.amazonaws.com make push
```

The build target tags the image as both `<registry>/nginx-s3-gateway:latest` and
`nginx-s3-gateway:latest` (for local testing).

## Cutover Plan

The cutover deploys the upstream-based gateway image to the ECS service. The
ECS rolling-update mechanism and ALB target-group health checks provide a safe,
automated deployment with zero-downtime and automatic rollback on failure.

### Prerequisites

- [ ] The upstream-based gateway image has been built and pushed to ECR:
  ```sh
  cd gateway/nginx-s3-gateway
  make build
  REGISTRY=<account>.dkr.ecr.<region>.amazonaws.com make push
  ```
- [ ] The image digest (SHA256) has been recorded for pinning:
  ```sh
  docker inspect --format='{{index .RepoDigests 0}}' <account>.dkr.ecr.<region>.amazonaws.com/nginx-s3-gateway:latest
  ```
- [ ] The `container_image` Terraform variable is set to the new image URI
  (or left empty to use the module-managed ECR repository with `:latest` tag)
- [ ] The `RESOLVE_CACHE_TTL_SECONDS` variable (default `"5"`) is acceptable
  for the target environment
- [ ] The `SITE_METADATA_TABLE_NAME` variable references the correct DynamoDB
  table

### Procedure

1. **Verify infrastructure**: Run `terraform plan` from `infrastructure/aws/`
   to confirm only the ECS task definition revision changes. No other resources
   should be modified.

2. **Apply**: Run `terraform apply`. Terraform creates a new ECS task definition
   revision with the upstream-based image and updates the ECS service to
   reference it.

3. **Monitor deployment**: The ECS service uses a rolling update
   (`deployment_maximum_percent = 200`, `deployment_minimum_healthy_percent = 100`).
   ECS launches new tasks, registers them with the ALB target group, and drains
   old tasks.

4. **Health check validation**: The ALB target group performs health checks
   (`GET /health`, expects 200) every 15 seconds with a 5-second timeout.
   The ECS container health check (`CMD-SHELL curl -sf http://localhost:80/health || exit 1`)
   runs every 15 seconds. Both must pass for a task to be considered healthy.

   **Expected behaviour during cutover**:
   - New tasks pass the container health check after NGINX starts (~2 s)
   - New tasks pass the ALB health check once registered (~15-30 s)
   - Old tasks continue serving traffic until new tasks are healthy
   - Old tasks are drained (`deregistration_delay`) and stopped

5. **Smoke-test the migrated gateway**:
   ```sh
   # Verify a known hostname resolves and serves content
   curl -sI https://<known-hostname>/ | head -20
   # Confirm: HTTP 200, Cache-Control header present, security headers present

   # Verify an unknown hostname returns clean 404
   curl -sI https://nonexistent.sites.<domain>/ | head -10
   # Confirm: HTTP 404, X-Frame-Options: DENY, Referrer-Policy: no-referrer

   # Verify health endpoint
   curl -s https://<alb-dns>/health
   # Confirm: HTTP 200, body "OK"
   ```

6. **Verify observability**: Check CloudWatch Logs for the gateway log group
   (`/ecs/<prefix>-gateway`). Confirm JSON-format access logs are flowing and
   contain the expected fields (`timestamp`, `host`, `status`, `request_time`,
   `upstream_response_time`).

### Success Criteria

- [ ] All ECS tasks are running the new task definition revision
- [ ] ALB target group shows all targets healthy
- [ ] Known hostnames resolve and serve content with correct headers
- [ ] Unknown hostnames return clean 404
- [ ] JSON access logs are visible in CloudWatch
- [ ] No 5xx errors in the gateway log group during the deployment window

## Rollback Plan

If the migrated gateway exhibits regressions after cutover, roll back to the
prior ECS task definition revision. The rollback is a Terraform operation
(revert `container_image` to the prior value, or use a prior task definition
revision) followed by an ECS service update.

### Rollback Triggers

Initiate rollback if **any** of the following conditions are observed after
cutover:

| Trigger | Detection | Severity |
|---|---|---|
| ALB target group health checks failing | CloudWatch metric `HealthyHostCount < 1` for the gateway target group | **Critical** — immediate rollback |
| Elevated 5xx responses | CloudWatch metric filter on gateway access logs shows `status >= 500` rate exceeding baseline | **Critical** |
| Host resolution failing for known-good hostnames | Manual smoke test: `curl -sI https://<known-hostname>/` returns 404 or 5xx | **High** |
| Security headers missing from content responses | Manual verification: `curl -sI` shows no `X-Content-Type-Options` header | **High** |
| Cache-Control headers missing or incorrect | Manual verification: CSS/JS assets missing `Cache-Control: max-age=31536000, immutable` | **Medium** |
| JSON log format malformed | CloudWatch Logs Insights queries fail to parse log entries as JSON | **Medium** |
| DynamoDB resolution errors in logs | Gateway log group shows `dynamodb_error` or `invalid_prefix` log entries above baseline | **High** |

### Procedure

1. **Identify the prior task definition revision**:
   ```sh
   aws ecs describe-task-definition \
     --task-definition <prefix>-gateway \
     --region <region>
   ```
   The previous (active, pre-cutover) revision will be listed in the output.
   Alternatively, known-good revision ARNs can be found in the ECS console
   under Task Definitions → `<prefix>-gateway` → Revisions.

2. **Revert the ECS service** (choose one method):

   **Method A — Terraform (preferred)**:
   Update `container_image` to the prior image URI and run `terraform apply`.
   This creates a new task definition revision pointing to the old image.

   **Method B — AWS CLI (faster, emergency)**:
   ```sh
   aws ecs update-service \
     --cluster <prefix>-gateway \
     --service <prefix>-gateway \
     --task-definition <prefix>-gateway:<prior-revision> \
     --region <region>
   ```

3. **Monitor deployment**: ECS performs a rolling update back to the prior
   revision. The same health check mechanisms apply — tasks are only put in
   service after passing both container and ALB health checks.

4. **Verify recovery**:
   - ALB target group returns to all-healthy state
   - Known hostnames resolve and serve content
   - 5xx error rate returns to baseline

5. **Post-rollback**: Investigate the root cause using CloudWatch Logs from the
   migrated gateway's task logs before the logs expire. Key areas to examine:
   - `host-resolver.js` debug logs (set `DEBUG=true` env var and redeploy to a
     test ECS service to capture detailed resolution traces)
   - DynamoDB `GetItem` call failures (credentials, network path via VPC
     endpoint)
   - NGINX error log (`/dev/stderr`) for template processing or upstream
     connection errors

### Rollback Time Estimate

- Terraform apply: 2–5 minutes (new task definition + service update)
- ECS rolling update to replace tasks: 2–5 minutes (depends on task count)
- Total recovery time: **5–10 minutes**

## References

- `VISION.md` §8.6 — Gateway requirements
- `VISION.md` §13.3 — Security headers
- `docs/upstream-nginx-s3-gateway-gap-analysis-78.md` — Upstream evaluation and extend decision
- `docs/design-gateway-host-to-prefix-resolution.md` — Host resolution design
- `docs/gateway-migration-validation-85.md` — Post-migration validation report
- `infrastructure/aws/modules/ecs-gateway/` — Terraform module for ECS deployment
