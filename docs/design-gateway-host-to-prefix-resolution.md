# Design: Dynamic host-to-prefix resolution for the gateway

**Issue**: [#77](https://github.com/your-org/aws-cognito-ephemeral-static-website-envs/issues/77)
**Date**: 2026-06-25
**Branch**: `act-or-s/77-design-dynamic-host-to-prefix-resolution-for-the-gateway`
**Status**: Design complete — ready for implementation (`gateway-resolution-impl`)

---

## 1. Summary

This design selects and specifies the mechanism by which the upstream-based
NGINX S3 gateway maps an incoming request host to the correct active published
S3 prefix (`host → owner → site → active version → S3 prefix`, per
`VISION.md` §6 and §8.6).

**Decision**: The gateway resolves host-to-prefix by querying DynamoDB
directly via njs, using the existing `SITE_METADATA_TABLE_NAME` environment
variable and gateway task IAM role.  Resolution requires exactly **one**
DynamoDB `GetItem` call per uncached request — the host mapping item is
self-contained with denormalized disabled flags.  An njs in-memory cache
(`js_shared_dict`) amortizes the DynamoDB call to zero network latency for
the vast majority of requests.

**Primary alternative considered**: An internal backend API endpoint (`GET
/api/internal/resolve`) wrapping the existing Go resolver.  Rejected because
it introduces a Lambda cold-start dependency on every uncached static-file
request and a runtime coupling between the serving path and the management
plane.

---

## 2. Background

### 2.1 Current state

The upstream `nginx-s3-gateway` image (F5, Apache 2.0) is a single-bucket,
fixed-prefix S3 proxy.  All requests proxy to `{S3_BUCKET_NAME}` with paths
derived from the incoming request URI.  There is no concept of per-hostname
routing.

Our platform requires per-user, per-site namespacing via the Host header:

```
https://foobar.myname.sites.example.com/
  → resolve "foobar.myname.sites.example.com"
  → published/users/myname/sites/foobar/versions/version_abc123/about/index.html
```

The gap analysis (`docs/upstream-nginx-s3-gateway-gap-analysis-78.md` §4)
confirmed this as the **primary architectural gap** — no upstream feature
exists or is planned to support multi-tenant, host-routed S3 gateways.

### 2.2 Existing infrastructure

The ECS gateway module (`infrastructure/aws/modules/ecs-gateway/main.tf`)
already provisions:

| What | Detail |
|---|---|
| `SITE_METADATA_TABLE_NAME` env var | Injected into the gateway container at line 162 |
| `ecs_gateway_role_arn` | Task role with `dynamodb:GetItem` + `dynamodb:Query` on `site_metadata_table_arn` |
| `gateway_s3` policy | `s3:GetObject` on `published/*`, `s3:ListBucket` on the bucket |
| VPC endpoint for DynamoDB | Gateway in private subnets, DynamoDB VPC endpoint provisioned (`modules/vpc`) |

The infrastructure was designed with direct DynamoDB access in mind — the
only missing piece is the njs module that performs the lookup.

### 2.3 Backend resolver

The Go backend (`backend/go-api/internal/resolver/`) provides two components:

- **`hostname.go`** — `ParseHost` extracts `siteSlug` / `userSlug` from the
  preferred (`{siteSlug}.{userSlug}.{sitesDomain}`) and alias
  (`{siteSlug}--{userSlug}.{sitesDomain}`) patterns.  Pure string
  manipulation, no I/O.

- **`resolver.go`** — `Resolve` performs the full serving-chain lookup:
  1. `GetHostMapping` (GetItem `PK=HOST#<hostname>`, `SK=MAPPING`)
  2. `GetUser` (GetItem `PK=USER#<userId>`, `SK=PROFILE`) → check disabled
  3. `GetSite` (GetItem `PK=USER#<userId>`, `SK=SITE#<siteId>`) → check
     disabled, verify active version exists

  Returns `*Resolution` with `S3PublishedPrefix`, or a sentinel error
  (`ErrHostNotFound`, `ErrUserDisabled`, `ErrSiteDisabled`,
  `ErrNoActiveVersion`).

The resolver currently performs **three sequential DynamoDB GetItem calls**.
One of the design goals is to reduce the hot-path gateway resolution to a
single call.

---

## 3. Candidate approaches

Three approaches were evaluated against four criteria:

1. **Latency** — resolution must not meaningfully increase request latency.
2. **Freshness** — publish events and soft-deletes must become visible within
   seconds.
3. **Operational complexity** — should not require new AWS services beyond
   those already in the architecture.
4. **Compatibility with upstream** — how much upstream njs and NGINX
   configuration is preserved.

### 3.1 Candidate A: njs subrequest to a backend resolver endpoint

The gateway's njs module calls an internal, unauthenticated backend API
endpoint (`GET /api/internal/resolve?host=...`) that wraps
`resolver.Resolve`.  The backend returns the S3 prefix as JSON.

| Criterion | Assessment |
|---|---|
| Latency | **Worst**. Lambda cold start (50–200ms+) on cache miss.  Acceptable only with aggressive caching. |
| Freshness | **Good**. Backend is the single source of truth; resolution is always consistent. |
| Operational complexity | **Medium**. Requires a new backend endpoint + making the Lambda reachable from the ECS task (extra Terraform: Lambda function URL or VPC endpoint for Lambda). |
| Upstream compatibility | **Good**. njs does a simple `ngx.fetch()` before the proxy pass; upstream signing and error handling are unchanged. |
| IAM | Gateway needs **no** DynamoDB access.  Backend Lambda already has it. |

### 3.2 Candidate B: njs reads DynamoDB directly

The gateway's njs module queries DynamoDB `GetItem` directly via
`ngx.fetch()` to the DynamoDB HTTPS API, using the same SigV4 signing
primitives as the S3 proxy (adapted for the `dynamodb` service).

| Criterion | Assessment |
|---|---|
| Latency | **Best**. DynamoDB GetItem latency is typically <10ms via the VPC endpoint.  No Lambda cold start. |
| Freshness | **Good**. DynamoDB is strongly consistent for GetItem; publishes and disables are visible immediately. |
| Operational complexity | **Medium**. Requires implementing DynamoDB SigV4 signing and response parsing in njs.  No new AWS resources. |
| Upstream compatibility | **Good**. njs module is a drop-in addition to the upstream architecture. |
| IAM | Gateway role **already has** `dynamodb:GetItem` on the metadata table (provisioned in `modules/iam`).  No IAM changes needed. |

### 3.3 Candidate C: NGINX `auth_request` / internal rewrite

The gateway uses NGINX's built-in `auth_request` directive to call an
internal location that performs resolution, then rewrites the upstream S3
key for the main proxy pass.

| Criterion | Assessment |
|---|---|
| Latency | **Same as Candidate A or B** — depends on resolution backend.  `auth_request` is an internal subrequest, not a redirect. |
| Freshness | Depends on resolution backend. |
| Operational complexity | **Highest**.  Requires coordinating two location blocks (auth + proxy).  `auth_request` returns 200/401/403, making it awkward to distinguish "host not found" from "host found, S3 object missing" — both map to 404 in the proxy location. |
| Upstream compatibility | **Worst**.  Requires restructuring the upstream's server block to use `auth_request`, which disrupts the upstream's flow and makes future upstream updates harder to merge. |
| IAM | Depends on resolution backend. |

### 3.4 Selection

**Candidate B (njs reads DynamoDB directly) is selected.**

Reasons:

1. **Lowest latency** — DynamoDB GetItem over the VPC endpoint adds ~5–10ms
   per uncached request.  With in-memory caching (see §5), >99% of requests
   are cache hits (zero added latency).  Candidate A always pays at least
   Lambda invocation overhead on cache miss, and the Lambda may be cold.

2. **Self-contained serving path** — the gateway resolves hostnames without
   depending on the backend API.  If the backend Lambda is deploying,
   throttled, or experiencing an outage, static sites continue to serve.
   Candidate A couples the serving hot path to the management plane.

3. **Infrastructure already provisioned** — the gateway task role has
   `dynamodb:GetItem` on `site_metadata_table_arn`, the table name is
   injected as `SITE_METADATA_TABLE_NAME`, and the DynamoDB VPC endpoint
   exists.  No IAM or Terraform changes are required except denormalizing
   two fields (see §4.1).

4. **No new AWS services** — Candidate B uses resources already in the
   architecture.  Candidate A requires exposing the Lambda via a function
   URL or VPC endpoint, adding one more configuration surface.

5. **Upstream compatible** — the njs module is a drop-in addition to the
   upstream architecture.  It runs before the proxy pass, sets an NGINX
   variable (`$s3_prefix`), and the upstream's SigV4 signing, directory
   indexing, and error handling proceed unchanged.

Candidate A remains a viable **fallback** if the DynamoDB response-parsing
complexity in njs proves too high.  The implementation can switch to a
backend endpoint by replacing the njs lookup function with an `ngx.fetch()`
to the backend, with no architectural changes otherwise.

---

## 4. Resolution contract

### 4.1 Host mapping item shape

To reduce resolution to a **single GetItem call**, two boolean fields are
denormalized into the host mapping item:

```
HostMapping item (DynamoDB):
  PK:                HOST#<hostname>
  SK:                MAPPING
  userId:            string   (Cognito sub)
  userSlug:          string
  siteId:            string
  siteSlug:          string
  versionId:         string   (empty before first publish)
  s3PublishedPrefix: string   (empty before first publish)
  userDisabled:      boolean  ← denormalized from User item
  siteDisabled:      boolean  ← denormalized from Site item
```

The two new fields (`userDisabled`, `siteDisabled`) MUST be kept in sync with
the `User` and `Site` items by the backend whenever those items are
created, updated, or soft-deleted.  See §7 for the required backend changes.

### 4.2 Resolution algorithm (njs)

```
function resolve(hostname):
  1.  Normalize hostname: strip port suffix (":443", ":80").
  2.  Check in-memory cache (see §5):
        a.  If positive hit → return cached prefix.
        b.  If negative hit → return error.
  3.  Perform DynamoDB GetItem:
        PK = "HOST#" + hostname
        SK = "MAPPING"
        ConsistentRead = false  (eventual consistency is acceptable;
                                 publish timing is not sub-second critical)
  4.  If Item not found:
        a.  Cache negative result.
        b.  Return "host_not_found".
  5.  If Item.userDisabled == true → return "user_disabled".
  6.  If Item.siteDisabled == true → return "site_disabled".
  7.  If Item.s3PublishedPrefix is empty or Item.versionId is empty
      → return "no_active_version".
  8.  If Item.s3PublishedPrefix does not start with "published/"
      → return "invalid_prefix" (defensive; never expected).
  9.  Cache positive result.
  10. Return Item.s3PublishedPrefix.
```

### 4.3 Error-to-HTTP-status mapping

All resolution errors return `404 Not Found` to the client, consistent with
VISION.md §8.6 ("return clear 404 responses for missing sites") and §12
("deleted sites must stop serving").  The error is logged at the gateway
level; the client receives a uniform "Not Found" response.

| njs error string | Resolver sentinel | HTTP status | Gateway log level |
|---|---|---|---|
| `host_not_found` | `ErrHostNotFound` | `404` | `warn` |
| `user_disabled` | `ErrUserDisabled` | `404` | `info` |
| `site_disabled` | `ErrSiteDisabled` | `404` | `info` |
| `no_active_version` | `ErrNoActiveVersion` | `404` | `info` |
| `dynamodb_error` | (gateway internal) | `404` | `error` |
| (any unexpected) | — | `404` | `error` |

**Why 404 for disabled/no-active-version?** Per VISION.md §12, a deleted
(disabled) site must stop serving.  Returning 404 (rather than 410 Gone or
403 Forbidden) avoids leaking information about whether the hostname ever
existed.  The gateway error page is uniform.

### 4.4 Gateway HTTP flow

```
Incoming request for https://foobar.myname.sites.example.com/about/

  1. njs js_set handler fires (before proxy pass):
       host-resolver.js: resolve("foobar.myname.sites.example.com")
         → cache miss
         → DynamoDB GetItem OK
         → s3PublishedPrefix = "published/users/myname/sites/foobar/versions/v_abc123/"
         → cache set
         → $s3_prefix = "published/users/myname/sites/foobar/versions/v_abc123/"

  2. NGINX constructs proxy URI:
       proxy_pass https://s3_backend;
       The S3 key = $s3_prefix + request_uri
                 = "published/users/myname/sites/foobar/versions/v_abc123/" + "/about/"
                 → directory rewrite: /about/index.html
                 → final S3 key: "published/users/myname/sites/foobar/versions/v_abc123/about/index.html"

  3. SigV4 signing operates on the final S3 key.
     (The upstream signs the actual proxy URI — no change needed.)

  4. S3 returns the object (or 404 if missing).
```

On resolution error:
```
Incoming request for https://bogus.sites.example.com/

  1. host-resolver.js: resolve("bogus.sites.example.com")
       → DynamoDB GetItem — no item found
       → negative cache set
       → return "host_not_found"

  2. njs sets $s3_prefix to empty, or sets a flag variable.

  3. NGINX returns 404 via the existing gateway_error location.
```

---

## 5. Caching and invalidation

### 5.1 Cache technology

njs `js_shared_dict` (available in njs 0.8.0+, which ships with the
upstream image's NGINX version).  `js_shared_dict` is a shared-memory
key-value store accessible from all njs worker processes.  It survives
configuration reloads but not restarts.

Each cache entry stores `{ s3Prefix: string, expiresAt: number }` or
`{ error: string, expiresAt: number }` for negative entries.

### 5.2 TTL values

| Cache type | TTL | Rationale |
|---|---|---|
| **Positive** (resolved prefix) | **5 seconds** | Publish events propagate within 5s.  Deleted/disabled site changes propagate within 5s.  At 1000 req/s, a 5s cache window captures ~5,000 requests without a DynamoDB call. |
| **Negative** (host not found) | **30 seconds** | Unknown hostnames are not expected to appear suddenly.  A longer TTL reduces DynamoDB load from scanners, crawlers, and probing. |
| **Negative** (disabled / no version) | **5 seconds** | Same as positive — a site may be un-disabled or published at any time. |

### 5.3 Why 5 seconds?

The 5-second TTL balances two constraints:

- **VISION.md §12**: "Deleted sites must stop serving."  With a 5-second
  TTL, the maximum staleness window after a soft-delete is 5 seconds (the
  time until the next cache miss triggers a DynamoDB re-read).

- **DynamoDB cost**: At 1000 requests/second across 100 unique hostnames,
  ~80% of requests map to ~20 hostnames.  A 5-second TTL means each popular
  hostname generates 1 DynamoDB GetItem every 5 seconds (0.2 RPS per
  hostname, 4 RPS total).  DynamoDB provisioned capacity or on-demand
  handles this trivially.

If the 5-second window is too aggressive for cost (many distinct hostnames),
the TTL can be increased to 30s or 60s — the design parameter is
configurable via an environment variable (`RESOLVE_CACHE_TTL_SECONDS`).

### 5.4 Invalidation

**No explicit invalidation mechanism is required.**  The 5-second TTL is
the invalidation strategy.  When a site is published, soft-deleted, or
un-disabled:

1. The backend writes the change to DynamoDB (updating the host mapping
   item if the active version changes, or the user/site disabled flag).
2. Within 5 seconds, the gateway's cache entry expires.
3. The next request for that hostname triggers a DynamoDB GetItem, which
   returns the updated item (strongly consistent read is not required;
   DynamoDB eventual consistency propagates within ~1 second).

**Why not push invalidation?** A publish or delete event could, in theory,
send an SNS notification that the gateway subscribes to for instant cache
purging.  This adds operational complexity (SNS topic, SQS queue, njs
polling) for a marginal improvement (5s → ~1s staleness).  For a static
site hosting platform, sub-5-second staleness is not a product requirement.

### 5.5 Cache size and eviction

`js_shared_dict` has a configurable size limit.  For the gateway:

- **Allocation**: 4 MiB (configurable via `RESOLVE_CACHE_SIZE`).
- **Entry size**: ~256 bytes per entry (hostname + prefix + metadata).
- **Capacity**: ~16,000 entries.
- **Eviction**: LRU (built into `js_shared_dict`).

At 16,000 entries and 5-second TTL, the cache naturally expires entries
before eviction is needed under normal traffic.  Under a scanning attack
(many random hostnames), LRU eviction drops the oldest negative entries
first, which is correct behaviour.

### 5.6 Cold start

On gateway restart or deployment, the cache is empty.  The first request
for each hostname triggers a DynamoDB GetItem (~5–10ms).  With the 5-second
TTL, the cache warms within a few seconds of traffic resuming.  No
pre-warming is required.

---

## 6. Composition with upstream signing flow

### 6.1 Where resolution fits in the request pipeline

The upstream nginx-s3-gateway processes each request through these stages:

```
Stage 1:  Receive request (server block, listen directive)
Stage 2:  Resolve S3 URI  (js_set $s3uri, s3gateway.s3uri)
Stage 3:  Sign with SigV4  (js_set $s3_date, $s3_auth, etc.)
Stage 4:  Proxy to S3      (proxy_pass https://s3_backend)
Stage 5:  Handle errors    (proxy_intercept_errors, error_page)
```

The host-to-prefix resolution module inserts a **pre-resolution stage**
between Stage 1 and Stage 2:

```
Stage 1:  Receive request
Stage 1a: Resolve host → s3_prefix  (js_set $s3_prefix, hostResolver.prefix)
           If error → internal redirect to @gateway_error
Stage 2:  Resolve S3 URI using $s3_prefix as path prefix
Stage 3–5: Unchanged
```

### 6.2 Integrating with the upstream S3 URI construction

The upstream `s3gateway.s3uri` function constructs the S3 key from the
request URI.  Our resolution module sets `$s3_prefix` as an NGINX variable.
The upstream template is extended to prepend `$s3_prefix` to the S3 key:

```nginx
# Upstream (unchanged):
js_set $s3uri s3gateway.s3uri;

# Our addition — prepend the resolved prefix to the upstream URI:
set $s3_key "${s3_prefix}${s3uri}";
```

The `proxy_pass` directive uses `$s3_key` instead of the upstream's
`$s3uri`.  The SigV4 signing operates on the final `$s3_key` (the upstream
signer reads `r.uri`, which reflects the rewritten URI — our approach
ensures the canonical request matches the actual S3 key).

### 6.3 Upstream files we touch

| File | Change | Type |
|---|---|---|
| `etc/nginx/njs/host-resolver.js` | **New** — resolution module | Addition |
| `etc/nginx/templates/default.conf.template` | Add `js_import hostResolver`, add `js_set $s3_prefix`, add `$s3_key` construction, add `js_shared_dict_zone` | Modification |
| `etc/nginx/nginx.conf` | Add `js_shared_dict_zone` directive in `http` block | Modification |
| Dockerfile | No changes (njs module is copied in via existing COPY) | None |

All other upstream files (SigV4 signing, credential management, directory
indexing, error handling, cache configuration) are **unchanged**.

---

## 7. Backend changes required

The host-to-prefix resolution design requires one small change to the Go
backend: **denormalizing `userDisabled` and `siteDisabled` into the host
mapping item.**  This eliminates the need for the gateway to perform
separate GetUser and GetSite calls.

### 7.1 Host mapping item — add disabled fields

**File**: `backend/go-api/internal/metadata/dynamodb/site_metadata.go`

```go
type hostMappingItem struct {
    PK                string `dynamodbav:"pk"`
    SK                string `dynamodbav:"sk"`
    UserID            string `dynamodbav:"userId"`
    UserSlug          string `dynamodbav:"userSlug"`
    SiteID            string `dynamodbav:"siteId"`
    SiteSlug          string `dynamodbav:"siteSlug"`
    VersionID         string `dynamodbav:"versionId"`
    S3PublishedPrefix string `dynamodbav:"s3PublishedPrefix"`
    UserDisabled      bool   `dynamodbav:"userDisabled"`  // NEW
    SiteDisabled      bool   `dynamodbav:"siteDisabled"`  // NEW
}
```

### 7.2 Keep disabled flags in sync

Three mutation operations must update the host mapping items when disabled
flags change:

1. **`DeleteUser`** (soft-delete): After setting `disabled = true` on the
   User item, also update all host mappings for that user to set
   `userDisabled = true`.  This requires a Query for all
   `PK = USER#<userId>, SK begins_with SITE#` to find the user's sites,
   then updating each corresponding host mapping.

2. **`DeleteSite`** (soft-delete): After setting `disabled = true` on the
   Site item, update both host mappings (preferred and alias) to set
   `siteDisabled = true`.

3. **`UpdateUser` / `UpdateSite`** (un-disable): If the `disabled` flag is
   being set to `false`, propagate to the host mappings as
   `userDisabled = false` / `siteDisabled = false`.

**Design note**: The `UpdateActiveVersion` call already touches both host
mappings in a `TransactWriteItems`.  The disabled-flag sync could be
piggybacked on that transaction, or done in the `DeleteSite` /
`DeleteUser` methods as a separate update.  The implementation issue
(`gateway-resolution-impl`) should choose the simplest approach that
maintains consistency.

### 7.3 CreateHostMapping — set initial disabled values

When creating a host mapping (in `handler/sites.go` `CreateSite`), set
`userDisabled` and `siteDisabled` to `false` initially.  Read the User
item to verify the user is not disabled before creating the mapping.

### 7.4 Metadata types — add disabled fields

**File**: `backend/go-api/internal/metadata/types.go`

```go
type HostMapping struct {
    Hostname          string
    UserID            string
    UserSlug          string
    SiteID            string
    SiteSlug          string
    VersionID         string
    S3PublishedPrefix string
    UserDisabled      bool   // NEW
    SiteDisabled      bool   // NEW
}
```

### 7.5 No new backend endpoint

This design does **not** add a new backend API endpoint.  The gateway
resolves directly from DynamoDB.  The existing resolver (`resolver.go`)
continues to work for backend use (the backend uses `Resolve` for validation
and management operations), but the gateway does not call it.

---

## 8. IAM implications

### 8.1 Gateway task role

**No IAM changes required.**  The gateway task role (`ecs_gateway`) already
has the necessary permissions:

```json
// Existing policy: gateway_dynamodb (modules/iam/main.tf:269–287)
{
  "Effect": "Allow",
  "Action": ["dynamodb:GetItem", "dynamodb:Query"],
  "Resource": ["<site_metadata_table_arn>"]
}
```

The gateway needs only `dynamodb:GetItem` for host mapping lookups.
`dynamodb:Query` is not used by the gateway but is harmless.

### 8.2 DynamoDB access pattern

The gateway uses the following DynamoDB access pattern:

| Operation | Key | Purpose |
|---|---|---|
| `GetItem` | `PK=HOST#<hostname>, SK=MAPPING` | Resolve hostname to serving prefix |

No scans, no queries, no writes.  The access pattern is a point lookup
exactly matching the IAM policy's scope.

### 8.3 Compared to Candidate A

Candidate A (backend API endpoint) would require:
- **Removing** the gateway's DynamoDB permission (defense-in-depth).
- **Adding** a Lambda function URL or VPC endpoint for Lambda invocation
  from the ECS task.
- The backend Lambda already has DynamoDB access — no new IAM there.

Candidate B (selected) requires zero IAM changes.  The gateway already has
the right permissions; the infrastructure was designed for this pattern.

---

## 9. Private-compatibility analysis

### 9.1 VPC endpoint for DynamoDB

The VPC module (`infrastructure/aws/modules/vpc`) provisions a DynamoDB
VPC endpoint:

```hcl
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.aws_region}.dynamodb"
  vpc_endpoint_type   = "Gateway"
  route_table_ids     = aws_route_table.private[*].id
}
```

The gateway task runs in private subnets with `assign_public_ip = false`.
All DynamoDB calls from njs (`ngx.fetch()`) are routed through the VPC
endpoint — no internet access is required.

### 9.2 No other dependencies

The resolution path depends on:
- **DynamoDB** (via VPC endpoint) — private-compatible ✓
- **In-memory cache** (local to the NGINX process) — no network dependency ✓

The resolution path does **not** depend on:
- The backend Lambda
- The internet
- DNS resolution (the hostname is the cache key; no external lookup)
- Any AWS service other than DynamoDB and S3

### 9.3 Deployment-mode invariance

The gateway configuration (container image, env vars, njs modules, NGINX
templates) is **identical** in public and private deployment modes.  The
only mode-dependent behaviour is at the infrastructure layer (ALB scheme,
DNS zone visibility, IGW existence).  The host-to-prefix resolution module
works the same in both modes.

---

## 10. Security considerations

### 10.1 Host header injection

An attacker could send requests with arbitrary `Host` headers.  The gateway
responds with 404 for any hostname not in the DynamoDB host mapping table.
There is no fallback or default host — unknown hosts are rejected uniformly.

### 10.2 DynamoDB injection

The DynamoDB GetItem key is constructed by prepending `HOST#` to the raw
`Host` header.  DynamoDB keys are opaque strings; there is no injection
surface because the key is used only for an exact-match GetItem lookup.

### 10.3 Information leakage

All resolution errors (host not found, disabled, no active version) return
a uniform 404 response.  An attacker cannot probe for whether a hostname
exists, was deleted, or never existed.

### 10.4 Cache poisoning

An attacker could attempt to pollute the in-memory cache by requesting many
different hostnames, evicting legitimate entries.  Mitigations:

1. **LRU eviction** favors frequently-accessed entries (legitimate sites).
2. **Negative cache TTL (30s)** means unknown-host entries are short-lived
   and don't accumulate under normal traffic.
3. The cache is per-gateway-task, not shared — an attack on one task does
   not affect others behind the ALB.

---

## 11. Implementation handoff

### 11.1 Files to create

| File | Description |
|---|---|
| `gateway/nginx-s3-gateway/etc/nginx/njs/host-resolver.js` | njs module: host resolution, DynamoDB GetItem, SigV4 signing for DynamoDB, in-memory caching via `js_shared_dict` |

### 11.2 Files to modify

| File | Change |
|---|---|
| `gateway/nginx-s3-gateway/etc/nginx/nginx.conf` | Add `js_shared_dict_zone` directive for resolution cache (in `http` block) |
| `gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template` | Add `js_import hostResolver`, `js_set $s3_prefix`, construct `$s3_key` from prefix + upstream URI, add error redirect for resolution failures |
| `backend/go-api/internal/metadata/types.go` | Add `UserDisabled`, `SiteDisabled` to `HostMapping` struct |
| `backend/go-api/internal/metadata/dynamodb/site_metadata.go` | Add `UserDisabled`, `SiteDisabled` to `hostMappingItem` struct; keep fields in sync in `DeleteUser`, `DeleteSite`, `UpdateUser`, `UpdateSite`, `CreateHostMapping`, `UpdateActiveVersion` |
| `infrastructure/aws/modules/ecs-gateway/main.tf` | Add `RESOLVE_CACHE_TTL_SECONDS` env var (optional, default 5) |

### 11.3 Files to verify (no changes expected)

| File | Reason |
|---|---|
| `infrastructure/aws/modules/iam/main.tf` | Gateway DynamoDB permission already exists |
| `infrastructure/aws/modules/vpc/main.tf` | DynamoDB VPC endpoint already exists |
| `gateway/nginx-s3-gateway/Dockerfile` | No changes — njs module is in the existing COPY path |
| `gateway/nginx-s3-gateway/etc/nginx/njs/s3-auth.js` | Replaced by upstream's `awssig4.js` in the migration, but no changes needed for resolution specifically |

### 11.4 Backend test impact

The denormalization of disabled flags into host mappings requires:

1. **Add assertions to existing `CreateHostMapping` tests**: verify
   `userDisabled` and `siteDisabled` are initialised to `false`.
2. **Add assertions to existing `DeleteUser` / `DeleteSite` tests**:
   verify host mappings are updated with the disabled flag.
3. **Add assertions to existing `UpdateActiveVersion` tests**: verify the
   disabled flags are preserved through the transaction.

No new test files are required — the existing table-driven test pattern
(`repository_test.go`, `sites_test.go`) accommodates the new fields.

### 11.5 Acceptance criteria for the implementation issue

1. `host-resolver.js` resolves a known hostname to the correct
   `S3PublishedPrefix` from DynamoDB.
2. Unknown hostnames, disabled users, disabled sites, and sites with no
   active version all return 404.
3. Resolution results are cached and reused within the TTL window.
4. Soft-deleting a site (setting `disabled = true` in DynamoDB) causes
   the gateway to return 404 within 5 seconds of the next request.
5. Publishing a new version (updating `S3PublishedPrefix` on the host
   mapping) takes effect within 5 seconds.
6. All existing backend tests pass after the `HostMapping` type changes.
7. The gateway continues to pass the acceptance criteria in
   `docs/acceptance-validation-24.md` §Static serving behaviours.
8. The gateway works in both public and private deployment modes (private
   mode validated per `docs/private-compatibility-review-26.md`).

---

## 12. Rejected alternatives recap

| Approach | Reason for rejection |
|---|---|
| **A: Backend API endpoint** | Lambda cold-start penalty on the serving hot path; couples static-site serving to management-plane availability.  Kept as a fallback if DynamoDB parsing in njs proves impractical. |
| **C: auth_request / internal rewrite** | Disrupts upstream NGINX configuration flow; `auth_request` 401/403 semantics don't map cleanly to "host not found" (which should be 404); makes future upstream updates harder to integrate. |
| **No caching** | Every request would perform a DynamoDB GetItem (~5–10ms + per-request cost).  Unacceptable for a static content gateway. |
| **Push invalidation (SNS/SQS)** | Adds SNS topic, SQS queue, and njs polling loop for marginal staleness improvement (5s → ~1s).  Not justified for static site hosting. |
