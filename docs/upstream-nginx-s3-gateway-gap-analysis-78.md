# Upstream nginx-s3-gateway Evaluation & Gap Analysis — Issue #78

Evaluates the upstream [`nginx-s3-gateway`](https://github.com/nginx/nginx-s3-gateway)
(F5, Inc., Apache 2.0) against the platform requirements in `VISION.md` §8.6
and §13.3, compares it to the current custom gateway at `gateway/nginx-s3-gateway/`,
and records an adopt/extend decision with strategy.

**Date**: 2026-06-25
**Branch**: `act-or-s/78-evaluate-upstream-nginx-s3-gateway-and-produce-gap-analysis`

---

## 1. Upstream Overview

Upstream is an official NGINX project (F5, Inc.) that configures the stock
NGINX OSS or NGINX Plus image as a SigV4-signed S3 proxy.  Published as
Docker images on GitHub Container Registry:

- `ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway:latest`
- Dated tags: `latest-njs-oss-YYYYMMDD`

**Base image**: `nginx:1.25.3` with njs `0.8.2`.

**Key facts**:
- 727 stars, 170 forks, 16 watchers
- 423 commits on `main`; no formal releases published
- License: Apache 2.0 (same as our current njs modules)
- No CloudFront dependency — aligns with §2.2

---

## 2. Capability Catalogue

### 2.1 Configuration Surface

A comprehensive comparison of environment variables:

| Variable | Upstream Default | Our Gateway | Notes |
|---|---|---|---|
| `S3_BUCKET_NAME` | *required* | `S3_BUCKET` | Same concept; ours supports dynamic per-host prefixes |
| `S3_SERVER` | `s3.us-east-1.amazonaws.com` | *(computed)* | Ours derives from `S3_BUCKET` + `AWS_REGION` |
| `S3_SERVER_PORT` | `443` | *(hardcoded 443)* | Same |
| `S3_SERVER_PROTO` | `https` | *(hardcoded https)* | Same |
| `S3_REGION` | *required* | `AWS_REGION` | Same concept |
| `S3_STYLE` | `virtual-v2`, `virtual`, `path` | *(hardcoded virtual)* | Upstream is more flexible |
| `S3_SERVICE` | `s3`, `s3express` | *(hardcoded s3)* | Upstream supports S3 Express One Zone |
| `AWS_SIGS_VERSION` | `4`, `2` | *(fixed SigV4)* | We do not need SigV2 |
| `AWS_ACCESS_KEY_ID` | optional | `AWS_ACCESS_KEY_ID` | Same |
| `AWS_SECRET_ACCESS_KEY` | optional | `AWS_SECRET_ACCESS_KEY` | Same |
| `AWS_SESSION_TOKEN` | optional | `AWS_SESSION_TOKEN` | Same |
| `ALLOW_DIRECTORY_LIST` | `false` | *(not implemented)* | We do not need directory listing |
| `PROVIDE_INDEX_PAGE` | `false` | *(always on)* | Our rewrite rule is unconditional |
| `APPEND_SLASH_FOR_POSSIBLE_DIRECTORY` | `false` | *(not implemented)* | Optional UX enhancement |
| `DIRECTORY_LISTING_PATH_PREFIX` | `""` | *(N/A)* | Only relevant with directory listing |
| `STRIP_LEADING_DIRECTORY_PATH` | `""` | *(not implemented)* | ALB subfolder hosting |
| `PREFIX_LEADING_DIRECTORY_PATH` | `""` | *(not implemented)* | Path prefix injection |
| `PROXY_CACHE_MAX_SIZE` | `10g` | *(no caching)* | Substantial gap — see §3.5 |
| `PROXY_CACHE_SLICE_SIZE` | `1m` | *(no caching)* | Byte-range support gap |
| `PROXY_CACHE_INACTIVE` | `60m` | *(no caching)* | — |
| `PROXY_CACHE_VALID_OK` | `1h` | *(no caching)* | — |
| `PROXY_CACHE_VALID_NOTFOUND` | `1m` | *(no caching)* | — |
| `PROXY_CACHE_VALID_FORBIDDEN` | `30s` | *(no caching)* | — |
| `CORS_ENABLED` | `false` | *(not implemented)* | Optional; not a §8.6 requirement |
| `CORS_ALLOWED_ORIGIN` | `*` | *(N/A)* | — |
| `HEADER_PREFIXES_TO_STRIP` | `""` | *(always strips x-amz-* and incoming SigV4)* | Upstream strips `x-amz-*` by default |
| `DNS_RESOLVERS` | *(empty)* | *(not implemented)* | DNS resolver configuration |
| `JS_TRUSTED_CERT_PATH` | *(empty)* | *(not needed)* | For EKS web identity STS calls |
| `AWS_ROLE_SESSION_NAME` | `nginx-s3-gateway` | *(N/A)* | STS AssumeRole session name |
| `STS_ENDPOINT` | *(empty)* | *(N/A)* | STS endpoint override |
| `AWS_STS_REGIONAL_ENDPOINTS` | `global` | *(N/A)* | Regional STS endpoint toggle |
| `FOUR_O_FOUR_ON_EMPTY_BUCKET` | `false` | *(N/A)* | Only relevant with directory listing |
| `DEBUG` | `false` | *(not implemented)* | SigV4 debug output |

### 2.2 SigV4 Signing — Implementation Comparison

| Aspect | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Crypto** | Node.js `crypto` module (`require('crypto')`) via `createHmac`/`createHash` | Pure JavaScript SHA-256 + HMAC-SHA256 in `njs/s3-auth.js` | Upstream depends on njs built with `--with-crypto`; ours works in any njs build |
| **Version** | SigV2 + SigV4 | SigV4 only | Gap N/A — we only need SigV4 |
| **Session tokens** | Included in canonical request + signed headers | Included in proxy headers | Same |
| **Key caching** | Available (NGINX Plus keyval store only) | Not implemented | Not needed for OSS |
| **Payload hash** | Computed from `r.variables.request_body` | Hardcoded empty-SHA256 (`e3b0c...`) | Same for GET/HEAD |
| **Date source** | `awscred.Now()` (frozen per-request) | `new Date()` per js_set call | Upstream has stronger consistency guarantee |
| **Code size** | `awssig4.js` ~200 lines | `s3-auth.js` ~448 lines | Upstream is leaner; ours bundles SHA-256 implementation |

### 2.3 Credential Sourcing

| Source | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Static env vars** | ✅ | ✅ `40-refresh-aws-credentials.sh` | Same |
| **ECS task metadata** | ✅ njs via `fetchCredentials()` | ✅ bash via `fetch_credentials_ecs()` | Different tech, same outcome |
| **EC2 IMDSv2** | ✅ njs | ✅ bash | Different tech, same outcome |
| **EKS web identity / STS** | ✅ njs `_fetchWebIdentityCredentials()` | ❌ | Gap — EKS not in scope for first prototype |
| **Auto-refresh** | ✅ njs with 4.5-min buffer | ✅ bash background loop (55-min default) | Upstream refresh is more precise |
| **Caching** | Keyval store (Plus) or filesystem | Filesystem (`aws-credentials.inc` as nginx include) | Our approach is simpler; each uses the right tool |

### 2.4 Directory Indexing and Path Handling

| Feature | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Index page serving** | `PROVIDE_INDEX_PAGE=true` → njs probes for `index.html` via internal fetch | Unconditional rewrite: `^(.*)/$ $1/index.html break` | Upstream verifies the file exists; we rewrite blindly |
| **Append slash redirect** | 302 redirect for extension-less, non-directory paths | Not implemented | Optional UX improvement |
| **Directory listing** | XSLT-rendered S3 XML listing | Not implemented | Not needed — we do not support listing |
| **`public/` directory normalization** | Not applicable | Handled in validation worker `validate/zip.go:215-237` | Our concern is at publish time, not gateway time |
| **Strip/prefix leading path** | Supported via env vars | Not implemented | Not needed for current use case |

### 2.5 Error Handling

| Aspect | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **404 handling** | Intercepts all 4xx/5xx, maps to generic 404; can disable for debug | `error_page 403 =404 /gateway_error; error_page 404 /gateway_error` → clean `"Not Found\n"` | Both produce clean 404 |
| **405 handling** | `@error405` returns Allow header | Not implemented | Our `limit_except` blocks at the NGINX level |
| **Method restriction** | `limit_except GET HEAD [OPTIONS]` (CORS gated) | `limit_except GET HEAD` | Same for our use case |

### 2.6 Security Headers and Content-Type Behaviour

| Requirement (VISION.md §8.6, §13.3) | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Preserve content types** | S3 returns Content-Type; gateway proxies it | S3 returns Content-Type; gateway proxies it | Same — both rely on S3 object metadata set by the publisher |
| **`Cache-Control`** | Proxy cache controls, no client-facing Cache-Control by default | Per-extension `Cache-Control` map (`gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template:26-34`) | **Gap in upstream**: no per-extension Cache-Control |
| **`X-Content-Type-Options`** | NOT set upstream | `nosniff` always (`gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template:156`) | **Gap in upstream** |
| **`X-Frame-Options`** | NOT set upstream | `SAMEORIGIN` for content, `DENY` for errors (`gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template:157,185`) | **Gap in upstream** |
| **`Referrer-Policy`** | NOT set upstream | `strict-origin-when-cross-origin` for content, `DENY` for errors (`gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template:158,186`) | **Gap in upstream** |
| **`server_tokens`** | `off` | Not explicitly set | Minor — add `server_tokens off` |
| **Strip AWS headers** | Strips `x-amz-*` + configurable prefixes | Strips incoming SigV4 headers + sets own (`gateway/nginx-s3-gateway/etc/nginx/templates/default.conf.template:142-145`) | Both achieve the same goal |
| **Client request header blocking** | `proxy_pass_request_headers off` (in listing/index locations only) | `proxy_set_header X-Amz-* ""` (in main location) | Upstream approach is more comprehensive in listing/index flows |

### 2.7 Observability

| Aspect | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Access logs** | Combined format (default) | JSON format (`gateway/nginx-s3-gateway/etc/nginx/nginx.conf:58-70`) | Our JSON format is better for CloudWatch metric extraction |
| **Error logs** | Default stderr | Default stderr | Same |
| **Health endpoint** | `location /health { return 200; }` (no body) | `location = /health { return 200 "OK\n"; }` + security headers | Our health endpoint is more explicit |

### 2.8 Caching

| Aspect | Upstream | Our Gateway | Assessment |
|---|---|---|---|
| **Proxy caching** | Full NGINX proxy cache with configurable TTLs, slice support, stale serving, revalidation | Not implemented | **Gap**: upstream caching is a significant feature |
| **Cache key** | `$request_method$host$uri` | N/A | — |

---

## 3. Requirement → Feature Mapping

### 3.1 VISION.md §8.6 — ECS NGINX S3 Gateway Requirements

| Requirement (§8.6) | Upstream Support | Configuration | Our Gateway | Gap? |
|---|---|---|---|---|
| Receive traffic only from ALB security group | ✅ Infra concern | Security group rules (Terraform) | ✅ Same | No |
| Map request host to site metadata | ❌ **NOT SUPPORTED** | N/A — single bucket only | ❌ **NOT YET IMPLEMENTED** — njs module to query DynamoDB pending | **PRIMARY GAP**: upstream has no host-to-prefix concept |
| Resolve active published version | ❌ **NOT SUPPORTED** | N/A | ❌ **NOT YET IMPLEMENTED** | Same as above |
| Fetch files from S3 using platform IAM | ✅ SigV4 + task role | `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` omitted → ECS/IMDS | ✅ Same pattern | No |
| Serve `index.html` for directory requests | ✅ `PROVIDE_INDEX_PAGE=true` | Env var | ✅ `rewrite ... break` | No |
| Return clear 404 for missing sites/files | ✅ Default 404 sanitization | Always on (can disable) | ✅ Clean error interception | No |
| Preserve static asset content types | ✅ S3 Content-Type proxied | Default behavior | ✅ Same | No |
| Support reasonable cache headers | ❌ Not built-in | Would need custom config | ✅ Per-extension Cache-Control map | **Gap in upstream** |
| Support private deployment | ✅ Works with private S3 | Standard SigV4 | ✅ Same | No |
| Emit access/error logs | ✅ Standard NGINX logs | Default | ✅ JSON access logs | No |

### 3.2 VISION.md §13.3 — Security Header Requirements

| Requirement | Upstream | Our Gateway | Gap? |
|---|---|---|---|
| Isolate hosted sites from management portal by origin | ✅ Infra/DNS concern | ✅ Same DNS separation | No (not gateway's concern) |
| Avoid exposing privileged cookies/tokens | ✅ `proxy_pass_request_headers off` | ✅ Strip incoming SigV4 headers | No |
| Set baseline security headers | ❌ Not set by default | ✅ X-Content-Type-Options, X-Frame-Options, Referrer-Policy | **Gap in upstream** |

### 3.3 VISION.md §6 — Hostname and Namespace Model

| Requirement | Upstream Support | Gap |
|---|---|---|
| `https://{siteSlug}.{userSlug}.{sitesDomain}/` | ❌ No hostname-to-prefix resolution | **PRIMARY GAP**: upstream serves one bucket; we need per-hostname resolution → dynamic S3 key prefix |
| `https://{siteSlug}--{userSlug}.{sitesDomain}/` (alias) | ❌ Same as above | Same gap |
| Metadata mapping `host → owner → site → active version → S3 prefix` | ❌ No metadata store integration | Must query DynamoDB for host mapping (likely with in-memory caching) |

---

## 4. Primary Integration Challenge: Dynamic Host-to-Prefix Resolution

### 4.1 The Gap

The upstream nginx-s3-gateway is a **single-bucket, fixed-prefix** gateway.
All requests proxy to `{S3_BUCKET_NAME}` with paths rebuilt from the incoming
request URI.  There is no concept of per-hostname routing.

Our platform requires (VISION.md §6):

```
hostname → DynamoDB HostMapping → User + Site → ActiveVersion → S3 prefix
```

Example:

```
foobar.myname.sites.example.com
  → GET host mapping "foobar.myname.sites.example.com" from DynamoDB
  → User: sub-abc, Site: site-xyz, Version: version_abc123
  → S3 prefix: published/users/myname/sites/foobar/versions/version_abc123/
  → Request /about/ proxies to S3 key: published/users/myname/sites/foobar/versions/version_abc123/about/index.html
```

### 4.2 Candidate Approaches

The following approaches are enumerated for the design issue (to be selected
and specified in the downstream migration issue):

**Approach A: njs host-to-prefix resolver (recommended)**

Write a new njs module (`host-resolver.js`) that:
1. Parses the `Host` header on each incoming request.
2. Looks up the DynamoDB host-mapping item (using `ngx.fetch()` / `js_fetch`
   to call a metadata API endpoint, or using the AWS SDK for JavaScript
   embedded in njs to call DynamoDB directly).
3. Constructs the full S3 key = `{s3PublishedPrefix}/{requestPath}`.
4. Sets an NGINX variable (`$s3_key`) consumed by the proxy.

This slots into the upstream architecture — replace `s3gateway.s3uri`
with a custom function that additionally resolves the host prefix.  The
upstream's existing SigV4 signing, directory indexing, and error handling
are preserved.

*Pros*: Most integrated; reuses upstream's proven logic.
*Cons*: Requires njs DynamoDB access (auth complexity); latency per request.

**Approach B: Sidecar metadata API**

Run a lightweight Go sidecar (or use the existing Go backend Lambda exposed
via API Gateway VPC endpoint) that provides a `GET /resolve?host=...` endpoint
returning the S3 prefix.  The njs module calls this internal API.

*Pros*: Simpler njs code; metadata logic stays in Go.
*Cons*: Additional service to operate; added latency per request.

**Approach C: NGINX keyval store with periodic refresh**

Pre-load host→prefix mappings into NGINX's keyval store, refreshed
periodically by a sidecar process.  njs reads from the keyval store
(zero-latency, in-memory).

*Pros*: No per-request latency for resolution; no external calls in hot path.
*Cons*: Requires NGINX Plus (keyval with API), or NGINX OSS with `js_shared_dict`
       (available in njs 0.8.0+ but has memory limits); delayed propagation
       on publish.

**Approach D: Custom NGINX build with Lua/OpenResty**

Replace njs with OpenResty's Lua-based S3 proxy, using Lua scripts for
host resolution.

*Pros*: Lua ecosystem is more mature for DynamoDB integration.
*Cons*: Completely different tech stack; increases supply-chain surface;
        VISION.md does not require OpenResty; upstream comparison
        becomes moot.

### 4.3 Selection Criteria

The downstream design issue should select based on:
1. **Latency**: Resolution must not meaningfully increase request latency.
2. **Freshness**: Publish events must become visible within seconds.
3. **Operational complexity**: The approach should not require new AWS
   services beyond those already in the architecture.
4. **Compatibility with upstream**: How much upstream code is preserved.

A combined approach (in-memory cache with DynamoDB-backed refresh,
Approach C with OSS-compatible fallback via `js_shared_dict`) is the
most promising for meeting all criteria.

---

## 5. Adopt-vs-Extend Decision

### 5.1 Decision: **EXTEND** — use the upstream image as the base, not a fork

**Rationale:**

1. **Supply-chain trust**: The upstream project is maintained by F5 (the
   company behind NGINX).  SigV4 signing, credential refresh, and S3
   edge-case handling (S3 Express, path vs. virtual styles, session tokens)
   are maintained and tested by domain experts.  Forking would mean taking
   on maintenance of ~1,500 lines of njs code.

2. **Feature parity**: The upstream has features we currently lack (proxy
   caching, byte-range support, stale serving) and has a cleaner separation
   of concerns (SigV4 in dedicated module, credential refresh in dedicated
   module, request routing in dedicated module).  Our custom gateway's
   `njs/s3-auth.js` bundles SHA-256 implementation, SigV4, and credential
   logic into a single file.

3. **Our value-add is the routing layer, not the S3 proxy**: The platform's
   differentiation is the host-to-prefix resolution, dynamic version mapping,
   per-extension cache control, and security headers — all of which are
   cleanly implemented as **additions** to the upstream architecture, not
   replacements.

4. **Incremental migration**: We can replace our custom Dockerfile with a
   `FROM ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway:latest-njs-oss-20241001`
   (or equivalent tagged version), then add our customizations as additional
   template files and njs modules — preserving the upstream's SigV4,
   credentials, and directory-index logic unchanged.

5. **The pure-JS SHA-256 is not a differentiator**: Our custom `njs/s3-auth.js`
   with embedded SHA-256 was a pragmatic choice when we built a standalone
   gateway.  The upstream's use of njs `crypto` module is the standard
   approach and is supported in the official NGINX images we consume.
   There is no advantage to maintaining our own crypto implementation.

6. **Image availability**: The upstream publishes to GitHub Container Registry
   (`ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway`), making it
   trivially consumable in our Dockerfile with `FROM`.

### 5.2 Image Pinning Strategy

| Decision | Detail |
|---|---|
| **Registry** | `ghcr.io/nginxinc/nginx-s3-gateway/nginx-oss-s3-gateway` |
| **Tag strategy** | Pin a dated tag (e.g., `latest-njs-oss-20241001`), NOT `latest` |
| **Digest pinning** | Additionally pin the SHA256 digest in `Dockerfile` for supply-chain integrity |
| **Update cadence** | Quarterly review of upstream tags; manual update tested in CI |
| **Build approach** | `FROM` the pinned upstream image; `COPY` our custom templates, njs modules, and entrypoint hooks on top |
| **Fallback** | If the upstream becomes unmaintained, we can switch to building from the upstream source (Apache 2.0) with our own `Dockerfile` derived from `Dockerfile.oss` |

### 5.3 What We Discard

When migrating to the upstream base, we can **remove** our custom
`njs/s3-auth.js` (pure-JS SHA-256 + SigV4), replacing it with the
upstream's `awssig4.js` + `awscredentials.js`.  Our `40-refresh-aws-credentials.sh`
entrypoint hook is also replaced by the upstream's njs-native credential
refresh.

### 5.4 What We Add

When extending the upstream image, we add:

1. **`host-resolver.js`** — njs module implementing `VISION.md` §6 host-to-prefix resolution (see §4.2).
2. **`cache-control.conf`** — NGINX map for per-extension Cache-Control (migrated from our current `default.conf.template:26-34`).
3. **Security headers** — `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy` added in the serving location block.
4. **JSON access log format** — Our structured log format (`nginx.conf:58-70`) for CloudWatch metric extraction.
5. **Health endpoint** — Extended with explicit body and security headers.
6. **`server_tokens off`** — Suppress NGINX version.
7. **Additional NGINX template** — Server block including our customizations, layered on top of the upstream's templates.

---

## 6. Summary Gap Matrix

| VISION.md Requirement | Upstream | Gap Severity | Resolution Path |
|---|---|---|---|
| §6 Host-to-prefix resolution | ❌ None | **CRITICAL** | njs module (Approach A/B/C) — design issue |
| §6 Active version resolution | ❌ None | **CRITICAL** | Same njs module as above |
| §8.6 Serve content from S3 | ✅ Full | None | Reuse upstream as-is |
| §8.6 `index.html` for directories | ✅ Config | None | Set `PROVIDE_INDEX_PAGE=true` |
| §8.6 Clear 404 responses | ✅ Default | None | Keep upstream default |
| §8.6 Content types preserved | ✅ Default | None | Keep upstream default |
| §8.6 Reasonable cache headers | ❌ None | **MEDIUM** | Add our Cache-Control map |
| §8.6 Access/error logs | ✅ Default | Minor | Add our JSON log format |
| §8.6 `/health` endpoint | ✅ Minimal | Minor | Enhance with explicit body + headers |
| §13.3 Baseline security headers | ❌ None | **MEDIUM** | Add header directives |
| §13.3 Origin isolation | ✅ Infra | None | Not gateway's concern |
| §2.7 Private deployment compatibility | ✅ Full | None | Same SigV4 path |
| §8.6 Byte-range support | ✅ Full | None | Reuse upstream slice+proxy |
| §8.6 Proxy caching | ✅ Full | None | Optional — enable upstream caching |
| Credential refresh (ECS/EC2) | ✅ Full | None | Reuse upstream `awscredentials.js` |

---

## 7. Disposition

**Upstream is suitable as the base image.**  The critical integration gap
(host-to-prefix resolution) is addressable through njs extension without
modifying upstream source code.  The platform's value-add (dynamic routing,
structured logging, security headers, per-extension caching) layers cleanly
on top.

Downstream issues can proceed from this decision:
- **Design issue**: Select and specify the host-to-prefix resolution approach
  from the candidate list in §4.2.
- **Implementation issues**: Dockerfile migration, njs module development,
  template layering, cache-control map migration, security header addition,
  and removal of the custom `njs/s3-auth.js`.
- **Testing issue**: Validate the extended gateway against all §8.6 and §17.4
  acceptance criteria.
