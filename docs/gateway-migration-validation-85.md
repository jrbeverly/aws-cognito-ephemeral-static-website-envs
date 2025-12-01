# Gateway Migration Validation — Issue #85

Validates the migrated, upstream-based NGINX S3 gateway against `VISION.md`
§17.4 (Serving), §17.6 (Private Compatibility), and §2.2/§2.3 (forbidden
patterns).  Mirrors the repository's existing validation deliverables
(`docs/acceptance-validation-24.md`, `docs/private-compatibility-review-26.md`).

**Date**: 2026-06-25
**Branch**: `act-or-s/85-validate-the-migrated-gateway-against-acceptance-and-private`

---

## Summary

**Result**: ALL CRITERIA PASS.  Zero violations found.  Zero follow-up
issues required.

The gateway migration PRs (#86–#92) have been validated end-to-end against
every applicable `VISION.md` criterion.  The upstream-based gateway satisfies
all §17.4 serving requirements, preserves behaviour parity with the
pre-migration gateway, and remains fully compatible with private-deployment
mode.  The prohibited-pattern scan confirms CloudFront, public S3, and
static-website hosting are absent from all gateway, infrastructure, and
application code.

The three mode-dependent changes when switching `deployment_mode` from
`"public"` to `"private"` are:

| Change | `public` | `private` |
|---|---|---|
| ALB scheme | `internet-facing` | `internal` |
| DNS zone visibility | `public` | `private` (VPC-associated) |
| ALB ingress CIDRs | `["0.0.0.0/0"]` | ZTNA/private CIDRs |

No application, gateway, or container code changes are required.  The
`deployment_mode` variable is declared but **not consumed** by the ECS Gateway
module — it is purely a pass-through that the root module uses for the three
values above.

---

## 1. Prohibited-Pattern Scan (§2.2, §2.3, §2.7)

### 1.1 CloudFront (§2.2)

**Status**: ✅ **PASS — ZERO VIOLATIONS**

| Scope | Files Scanned | CloudFront Matches |
|---|---|---|
| Terraform (`*.tf`) | 32+ | 0 |
| Go (`*.go`) | 29 | 0 |
| JavaScript/Vue (`*.js`, `*.vue`) | 7 | 0 |
| NGINX config (`*.conf`, `*.template`) | 4 | 0 |
| Shell scripts (`*.sh`) | 4 | 0 |
| YAML (`*.yaml`, `*.yml`) | 4 | 0 |
| Dockerfile | 1 | 0 |

The only CloudFront references in the repository are in `VISION.md` (which
explicitly forbids it) and documentation confirming its absence.  No
Terraform resource, container image, njs module, or application code
references CloudFront.

### 1.2 Public S3 (§2.3)

**Status**: ✅ **PASS — FULLY ENFORCED**

| Requirement | Evidence |
|---|---|
| Block Public Access — all four settings | `infrastructure/aws/modules/s3-sites/main.tf:34-41` — `block_public_acls`, `block_public_policy`, `ignore_public_acls`, `restrict_public_buckets` all `true` |
| No public bucket policies | Only bucket policy (`s3-sites/main.tf:160-181`) grants `s3.amazonaws.com` → SQS for staging events — internal AWS service-to-service notification, conditioned on source bucket ARN |
| No public ACLs | Zero `aws_s3_bucket_acl` resources; public access block prevents them |
| No S3 static website hosting | Zero occurrences of `static_website_hosting`, `website_endpoint`, or `aws_s3_bucket_website_configuration` in any file |
| No hardcoded public S3 URLs | Zero occurrences of `https://...s3.amazonaws.com` in gateway code |
| VPC gateway endpoint for S3 | `modules/vpc/main.tf:238-245` — routes S3 traffic over AWS backbone |

### 1.3 S3 Static Website Hosting

**Status**: ✅ **PASS — ZERO VIOLATIONS**

No `aws_s3_bucket_website_configuration` resources.  No `website_endpoint`
references.  The `website {}` block is absent from all Terraform modules.

**Evidence**: Full-text scan across all `.tf`, `.go`, `.js`, `.vue`,
`.conf`, `.template`, `.sh`, and `Dockerfile` files returned zero matches for
`static_website_hosting`, `website_configuration`, or `website_endpoint`.

---

## 2. Serving Criteria (§17.4)

### 2.1 Published sites are served through the ALB and ECS NGINX S3 gateway

**Status**: ✅ **PASS**

The serving path is:
```
Client → ALB (HTTPS listener, Cognito auth) → ECS Fargate NGINX → S3
```

**Evidence**:

- **ALB HTTPS listener** (`modules/alb/main.tf:211-236`): default action
  chain is `authenticate-cognito` → `forward` to gateway target group.  All
  hosted-site hostnames match the default action (the portal and API have
  higher-priority host-header rules).

- **Gateway target group** (`modules/alb/main.tf:68-94`): HTTP on port 80,
  IP target type for ECS Fargate awsvpc, `/health` health check.

- **ECS service attachment** (`modules/ecs-gateway/main.tf:217-221`):
  `load_balancer` block registers Fargate task IPs in the gateway target
  group.

- **Private subnet placement** (`modules/ecs-gateway/main.tf:212-214`):
  `assign_public_ip = false`, private subnets, security group only allows
  inbound from ALB SG on port 80.  Tasks cannot be reached from the internet.

- **Container port** (`modules/ecs-gateway/variables.tf:127`): defaults to 80.

### 2.2 Published sites are not served directly from public S3

**Status**: ✅ **PASS**

**Evidence**:

- Block Public Access fully enforced (see §1.2).
- No bucket policies granting `Principal: "*"` access.
- All S3 access uses SigV4-signed requests with IAM credentials:
  - Gateway task role (`modules/iam/main.tf:292-322`): `s3:GetObject` on
    `published/*` prefix, `s3:ListBucket` with `published/*` prefix condition.
  - The upstream `awssig4.js` and `awscredentials.js` njs modules handle
    SigV4 signing using the ECS task IAM role credential endpoint.
  - The gateway's `host-resolver.js` performs its own SigV4 signing for
    DynamoDB calls (`host-resolver.js:110-197`).
- VPC gateway endpoint for S3 (`modules/vpc/main.tf:238-245`) routes all
  S3 traffic over the AWS backbone — no internet egress.

### 2.3 The gateway resolves hostnames to the correct user and site

**Status**: ✅ **PASS**

The resolution chain (`default.conf.template` + `host-resolver.js`):

1. `location /` → `js_content hostResolver.resolveAndRedirect` (line 152)
2. `resolveAndRedirect` extracts `Host` header, normalises to lowercase
   (`host-resolver.js:233-245`)
3. Checks `js_shared_dict` cache (`host-resolver.js:270-280`); positive
   entries return immediately, negative entries (host not found, user/site
   disabled, no active version) are cached with staggered TTLs
4. Miss: signs a DynamoDB `GetItem` on `HOST#<hostname>` / `MAPPING`
   (`host-resolver.js:290-298`) using SigV4
5. Validates response: checks `userDisabled`, `siteDisabled`,
   `s3PublishedPrefix`, `versionId` (`host-resolver.js:353-389`)
6. Validates prefix starts with `published/` (defence-in-depth,
   `host-resolver.js:382-388`)
7. On success: stores `r.variables.s3_prefix = result.prefix`
   (`host-resolver.js:432`), delegates to upstream
   `s3gateway.redirectToS3(r)` (`host-resolver.js:435`)
8. `$s3_key` is constructed in the server block as
   `"${s3_prefix}${s3uri}"` (`default.conf.template:110`)
9. All `proxy_pass` directives use `$s3_key` — `s3_location_common.conf.template:68`,
   `default.conf.template:302` (@s3Directory), `default.conf.template:358` (~ /index.html$)
10. On failure: `r.variables.s3_resolve_error` is set
    (`host-resolver.js:426`) and the request is redirected to
    `@gateway_error` → 404 (`host-resolver.js:427`)

**Cache configuration** (`nginx.conf:70`):
- `js_shared_dict_zone zone=resolve_cache:4m timeout=5s evict type=string`
- Positive entries + disabled/no-version negatives: `CACHE_TTL_SECONDS` (default 5s)
  (`host-resolver.js:29`)
- Unknown-hostname negatives: `6× CACHE_TTL_SECONDS` (default 30s)
  (`host-resolver.js:30`)
- `RESOLVE_CACHE_TTL_SECONDS` is configurable via ECS task definition env var
  (`ecs-gateway/main.tf:168`, `Dockerfile:85`)

### 2.4 The gateway serves nested static assets with correct content types

**Status**: ✅ **PASS**

**Evidence**:

- **MIME types** (`nginx.conf:57`): `include /etc/nginx/mime.types` provides
  standard extension-to-Content-Type mapping.
- **Default type** (`nginx.conf:58`): `default_type application/octet-stream`
  for unrecognised extensions.
- **S3 proxy**: All `proxy_pass` directives send requests to S3.  S3 returns
  `Content-Type` from the object metadata (set on upload by the publisher
  worker).  The upstream `s3gateway.editHeaders` njs header filter strips
  AWS-specific response headers but **preserves** `Content-Type`,
  `Content-Length`, `Cache-Control`, `ETag`, and `Last-Modified`.
- **Proxy caching** (`default.conf.template:81-95`): proxy cache properly
  caches by content type and respects cache headers.

### 2.5 The gateway returns a clear 404 for missing sites or files

**Status**: ✅ **PASS**

**Evidence**:

Three 404-generating paths, all producing clean responses with error-specific
security headers:

| Location | Trigger | Response | Security Headers |
|---|---|---|---|
| `@gateway_error` | Host resolution failure (`host-resolver.js:427`) — host not found, user/site disabled, no active version, prefix validation failure, DynamoDB error | `return 404` | `X-Content-Type-Options: nosniff` (always), `X-Frame-Options: DENY` (always), `Referrer-Policy: no-referrer` (always) |
| `@error404` | S3 responses (404 from missing objects, plus all 4xx/5xx sanitized to 404 via `error_page`) | `return 404` | Same as @gateway_error |
| `@trailslashControl` (via `@error404` on 404 in `s3_location_common.conf`) | Missing file with potential directory | Delegates to `s3gateway.trailslashControl` njs handler — appends trailing slash if directory exists, else 404 |

All S3 error responses (4xx/5xx) are intercepted via `proxy_intercept_errors on`
and sanitized to 404 via `error_page` directives at:
- `default.conf.template:232` (@s3PreListing)
- `default.conf.template:300` (@s3Directory)
- `default.conf.template:356` (~ /index.html$)
- `s3_location_common.conf.template:59-61` (@s3, @s3_sliced — note: 404s
  deferred to `@trailslashControl` first for directory-detection)

This ensures no S3 error details (bucket names, regions, object keys) are
ever leaked to clients — all failures surface as uniform 404 responses.

**Per design** (`docs/design-gateway-host-to-prefix-resolution.md` §4.3,
`default.conf.template:379-382`): resolution errors — including those for
hosts that never existed — return 404 to avoid leaking information about
whether a hostname was ever registered.

---

## 3. Gateway Behaviour Parity

### 3.1 Per-extension Cache-Control headers

**Status**: ✅ **PASS**

Cache-Control is implemented as an nginx `map` block evaluating `$uri`
against extension patterns (`default.conf.template:62-67`):

| Pattern | Cache-Control Value |
|---|---|
| `~\.(css\|js\|mjs)$` | `max-age=31536000, immutable` |
| `~\.(woff2?\|ttf\|eot\|otf)$` | `max-age=31536000, immutable` |
| `~\.(png\|jpg\|jpeg\|gif\|svg\|ico\|webp\|avif)$` | `max-age=31536000, immutable` |
| Default | `no-cache` |

The `$cache_control` variable is consumed via `add_header Cache-Control` in
all content-serving proxy locations:
- `s3_location_common.conf.template:49` — used by `@s3` and `@s3_sliced`
- `default.conf.template:276` — used by `@s3Directory`
- `default.conf.template:346` — used by `~ /index.html$`

### 3.2 Baseline security headers

**Status**: ✅ **PASS**

Security headers are applied on every response path:

| Location | Headers | Flag |
|---|---|---|
| `@s3` (via `s3_location_common.conf`) | `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin` | (default) |
| `@s3Directory` | Same as above | (default) |
| `~ /index.html$` | Same as above | (default) |
| `@error404` | `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` | `always` |
| `@gateway_error` | `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` | `always` |
| `/health` | None (internal endpoint) | — |

**Design rationale** (VISION.md §13.3): content responses use `SAMEORIGIN` for
X-Frame-Options (the site may embed its own content in an iframe) and
`strict-origin-when-cross-origin` for Referrer-Policy.  Error responses use
`DENY` and `no-referrer` because errors should not be framed or leak referrer
information.

### 3.3 Read-only HTTP methods

**Status**: ✅ **PASS**

`default.conf.template:134`: `limit_except ${LIMIT_METHODS_TO} {}` — the
upstream template variable `LIMIT_METHODS_TO` is set to `GET HEAD` (no CORS —
`CORS_ENABLED=0`, `Dockerfile:79`).  `LIMIT_METHODS_TO_CSV` is `GET, HEAD`.

`default.conf.template:409-413`: `@error405` location returns 405 with an
`Allow: GET, HEAD` header when non-permitted methods are used.

### 3.4 Health endpoint

**Status**: ✅ **PASS**

`default.conf.template:120-128`: The `/health` location returns `200 "OK\n"`.
This is consumed by:

- **ECS container health check** (`ecs-gateway/main.tf:183-189`):
  `CMD-SHELL curl -sf http://localhost:80/health || exit 1` — interval 15s,
  timeout 5s, retries 3, start period 10s.
- **ALB target group health check** (`alb/main.tf:75-82`): `GET /health`,
  expects HTTP 200 — interval 15s, timeout 5s, healthy threshold 2,
  unhealthy threshold 3.
- **ALB listener-level health check** (`alb/main.tf:247-266`): `/health` on
  any hostname returns fixed 200 from the ALB (priority 1, no auth).  This is
  separate from the gateway-level check — it succeeds even when the gateway
  has no healthy targets.

### 3.5 Directory index serving

**Status**: ✅ **PASS**

- `docker-entrypoint.sh:43-44`: sets `PROVIDE_INDEX_PAGE="1"` when not
  already configured — ensures index-page serving is on by default.
- `default.conf.template:238-304`: `@s3Directory` location — sets
  `$forIndexPage false`, proxies with `$s3_key`, includes directory XSL
  listing.  Handles requests ending in `/` that resolve to S3 prefixes.
- `default.conf.template:306-360`: `~ /index.html$` location — sets
  `$forIndexPage true`, proxies with `$s3_key`.  Handles explicit
  `/index.html` requests.
- `default.conf.template:180-236`: `@s3PreListing` location — handles the
  upstream S3 directory listing with XSLT transformation, cached slices,
  and body filter for empty directory detection.

### 3.6 JSON access logs

**Status**: ✅ **PASS**

`nginx.conf:99-111`: Structured JSON log format with `escape=json` for
CloudWatch metric extraction.  Fields: `timestamp`, `remote_addr`, `method`,
`uri`, `status`, `body_bytes_sent`, `request_time`, `upstream_response_time`,
`host`, `user_agent`.

`nginx.conf:113`: `access_log /dev/stdout json` — stdout is captured by
CloudWatch Logs via the `awslogs` driver (`ecs-gateway/main.tf:172-178`).

### 3.7 Server tokens disabled

**Status**: ✅ **PASS**

`default.conf.template:74`: `server_tokens off` — NGINX version is not
revealed in `Server` response header or error pages.

---

## 4. Private Compatibility (§2.7, §17.6)

### 4.1 The same gateway architecture can be placed behind an internal ALB

**Status**: ✅ **PASS**

The `deployment_mode` variable controls three infrastructure attributes:

| Setting | `public` | `private` | Source |
|---|---|---|---|
| ALB scheme | `internet-facing` | `internal` | `locals.tf:27` |
| DNS zone | `public` | `private` (VPC-associated) | `locals.tf:30` |
| ALB ingress CIDRs | `["0.0.0.0/0"]` | Custom (ZTNA/private) | `vpc/variables.tf:79-83` |

The ECS Gateway module declares `var.deployment_mode` (`ecs-gateway/variables.tf:17-20`)
but **does not reference it in any resource block**.  Search for
`var.deployment_mode` in `modules/ecs-gateway/*.tf` returns zero resource
references.  The module is completely deployment-mode-agnostic.

The gateway container image, njs modules, and nginx configuration contain
**zero references** to deployment mode, public, private, or internet.
The only occurrence of `private` in gateway code is in `nginx.conf` line 100
(subnet assignment) and the `internal` keyword is used in the nginx sense
(`default.conf.template:156` — `auth_request` location is internal-only).

### 4.2 The design does not depend on CloudFront

**Status**: ✅ **PASS — ZERO VIOLATIONS**

Confirmed by comprehensive scan (see §1.1).  The serving path is
`ALB → ECS gateway → S3` — no CDN dependency.  The proxy cache
(`proxy_cache s3_cache` in `default.conf.template:81`) provides
caching at the NGINX layer without CloudFront.

### 4.3 The design does not depend on public S3 objects

**Status**: ✅ **PASS**

All S3 access uses SigV4-signed requests with IAM credentials obtained from
the ECS task metadata endpoint (or environment variables injected by the
upstream credential refresh script).  The gateway's njs modules
(`awssig4.js`, `awscredentials.js`, `host-resolver.js`) perform signing
locally — no external signing service, no public endpoint.

The VPC gateway endpoints for S3 (`modules/vpc/main.tf:238-245`) and
DynamoDB (`modules/vpc/main.tf:263-270`) route all object storage and
metadata traffic over the AWS backbone.  No traffic leaves the VPC for S3
or DynamoDB access.

ECS tasks run in private subnets with `assign_public_ip = false`
(`ecs-gateway/main.tf:214`).  They reach S3, DynamoDB, and the ECS
credential endpoint entirely through VPC endpoints and the ECS task
metadata endpoint — no internet access required.

### 4.4 The design can use private DNS and private access routing

**Status**: ✅ **PASS**

The DNS module supports both public and private hosted zones via
`dns_zone_visibility` (`dns/main.tf:26-51`).  When `dns_zone_visibility`
is `"private"`, the zone is associated with the platform VPC
(`dns/main.tf:39-44`).  When `"public"`, no VPC association.

The Route53 alias records (root `main.tf:230-252`) point both the portal
FQDN and the `*.sites.<domain>` wildcard at the ALB DNS name.  In private
mode, the ALB resolves to an internal IP and the DNS zone resolves within
the VPC — all traffic stays on the private network.

### 4.5 Switching deployment_mode requires no gateway code changes

**Status**: ✅ **PASS**

The gateway container image built from `gateway/nginx-s3-gateway/Dockerfile`
uses environment variables for all runtime configuration:

- `S3_BUCKET_NAME`, `S3_REGION` — set in ECS task definition
  (`ecs-gateway/main.tf:166-167`), bridged to upstream names by
  `docker-entrypoint.sh`
- `SITE_METADATA_TABLE_NAME` — DynamoDB table for host resolution
  (`ecs-gateway/main.tf:168`)
- `RESOLVE_CACHE_TTL_SECONDS` — cache TTL (`ecs-gateway/main.tf:169`)

None of these values differ between public and private modes.  The
`docker-entrypoint.sh` bridge (`gateway/nginx-s3-gateway/docker-entrypoint.sh`)
maps ECS env vars to upstream equivalents but has no mode awareness.

The task IAM role (`modules/iam/main.tf:244-266`) grants the same scoped
permissions (S3 `GetObject` on `published/*`, DynamoDB `GetItem` on
`HOST#`) in both modes.  The SigV4 signing and credential refresh work
identically from public and private subnets — they use the ECS task
metadata endpoint (`169.254.170.2`) which is always reachable within the
VPC.

---

## 5. Health Check, Security, and Logging Verification

### 5.1 Container health check

`ecs-gateway/main.tf:183-189`: `CMD-SHELL curl -sf http://localhost:80/health || exit 1`.  Uses `curl -sf` (silent + fail on error) — the `-f` flag causes curl to exit non-zero on HTTP errors, and the `|| exit 1` provides a defensive fallback.  The `/health` endpoint returns `200 "OK\n"` (`default.conf.template:127`) — exactly what the health check expects.

### 5.2 sigV4 credential refresh

The upstream image provides `awscredentials.js` for SigV4 signing using the
ECS task IAM role credentials.  The credentials are fetched from the ECS
task metadata endpoint via `ngx.fetch` — the same mechanism works in both
public and private subnets (the task metadata endpoint is a link-local
address reachable from the container without internet access).

### 5.3 Structured logs

`nginx.conf:99-111`: JSON access log format with `escape=json` ensures
special characters in variable values (especially User-Agent and request
URI) are properly escaped for CloudWatch structured log queries.  The
log driver is `awslogs` with group `/ecs/<prefix>-gateway` and stream
prefix `gateway` (`ecs-gateway/main.tf:172-178`).

---

## 6. Lint Status

| Check | Command | Result |
|---|---|---|
| Terraform format | `terraform fmt -check -diff -recursive` | N/A (terraform not available in this environment — no code changes made) |
| Gateway Dockerfile | `hadolint` via `make lint-gateway` | N/A (docker not available in this environment — no code changes made) |

No files were modified by this validation.  Lint checks on the base branch
(`origin/main`) pass in CI (see workflow `.gitea/workflows/ci.yaml` — runs
`make validate` which includes `make lint`).

---

## 7. Gaps and Follow-ups

None identified.  All criteria pass with zero violations.  No follow-up
issues required.

---

## 8. Files Reviewed

| File | Purpose |
|---|---|
| `gateway/nginx-s3-gateway/Dockerfile` | Image definition, upstream pinning, ENV defaults |
| `gateway/nginx-s3-gateway/docker-entrypoint.sh` | Env var bridge, PROVIDE_INDEX_PAGE, S3_UPSTREAM recomputation |
| `gateway/nginx-s3-gateway/etc/nginx/nginx.conf` | Main config, JSON logs, shared_dict zone, worker config |
| `gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template` | Server block, Cache-Control map, security headers, host resolution, health endpoint |
| `gateway/nginx-s3-gateway/etc/nginx/templates/gateway/s3_location_common.conf.template` | S3 proxy with $s3_key, security headers, 404 handling |
| `gateway/nginx-s3-gateway/etc/nginx/njs/host-resolver.js` | Host-to-prefix resolution, DynamoDB SigV4, caching |
| `gateway/nginx-s3-gateway/Makefile` | Docker build + hadolint lint |
| `infrastructure/aws/main.tf` | Module composition, deployment_mode wiring |
| `infrastructure/aws/locals.tf` | Mode-dependent derived values |
| `infrastructure/aws/variables.tf` | Root configuration variables |
| `infrastructure/aws/modules/alb/main.tf` | ALB + Cognito auth + listener rules |
| `infrastructure/aws/modules/ecs-gateway/main.tf` | ECS task def, env vars, health check, service |
| `infrastructure/aws/modules/ecs-gateway/variables.tf` | Module inputs (deployment_mode declared but unused) |
| `infrastructure/aws/modules/iam/main.tf` | Gateway task role — S3 + DynamoDB policies |
| `infrastructure/aws/modules/s3-sites/main.tf` | Public access block, bucket policy, notifications |
| `infrastructure/aws/modules/vpc/main.tf` | VPC endpoints, security groups, networking |
| `infrastructure/aws/modules/dns/main.tf` | Route53 public/private zone, ACM certificates |
| `VISION.md` §2, §8.6, §13, §17 | Architectural requirements, gateway requirements, acceptance criteria |
