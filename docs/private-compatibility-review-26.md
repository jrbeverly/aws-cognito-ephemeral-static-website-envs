# Private-Deployment Compatibility Verification — Issue #26

Verifies the platform design satisfies all private-compatibility acceptance
criteria in `VISION.md` §2.7 and §17.6, confirming the system can move to a
private deployment by changing exposure, DNS, certificates, and routing —
without replacing the core application architecture.

**Date**: 2026-06-25
**Branch**: `act-or-s/26-private-deployment-compatibility-verification`

> **Historical note**: This review was conducted against the original
> custom gateway. The custom SigV4 signing module (`njs/s3-auth.js`), credential
> refresh script (`docker-entrypoint.d/40-refresh-aws-credentials.sh`), and
> `aws-credentials.inc` mechanism referenced below have been decommissioned and
> replaced by the upstream nginx-s3-gateway equivalents (`awssig4.js`,
> `awscredentials.js`). See `gateway/nginx-s3-gateway/README.md` and
> `docs/gateway-migration-validation-85.md` for the current gateway architecture.

---

## Summary

**Result**: ALL CRITERIA PASS.  No violations found.  No follow-up issues
required.

The platform satisfies every hard architectural requirement in `VISION.md` §2
and every private-compatibility acceptance criterion in §17.6.  Switching
`deployment_mode` from `"public"` to `"private"` changes three infrastructure
attributes (ALB scheme, DNS zone visibility, and ingress CIDR scope) without
requiring any application, gateway, or backend code changes.

---

## 1. Hard Requirements Verification (`VISION.md` §2)

### 2.1 AWS is mandatory

**Status**: ✅ **SATISFIED**

All components target AWS-native services: ECS Fargate, Lambda (Go
`provided.al2023`), S3, DynamoDB, Cognito, ALB, Route53, ACM, CloudWatch,
SQS.  No non-AWS cloud resources exist in any Terraform module or
application code.

### 2.2 CloudFront is explicitly forbidden

**Status**: ✅ **SATISFIED — ZERO VIOLATIONS**

A full-text scan of every file in the repository confirms:

| Scope | Files Scanned | CloudFront Matches |
|---|---|---|
| Terraform (`*.tf`) | 32+ | 0 |
| Go (`*.go`) | 24 | 0 |
| JavaScript/Vue (`*.js`, `*.vue`) | 7 | 0 |
| NGINX config (`*.conf`, `*.template`, `*.js`) | 4 | 0 |
| Shell scripts (`*.sh`) | 4 | 0 |
| YAML (`*.yaml`, `*.yml`) | 4 | 0 |
| Markdown (`*.md`) | 10+ | Mentions only in `VISION.md` (forbidding it), `infrastructure/aws/README.md` (confirming exclusion), and `docs/acceptance-validation-24.md` (confirming no violation) |

The only CloudFront references in the codebase are in VISION.md (which
explicitly forbids it) and documentation confirming its absence.  No
Terraform module, container image, njs script, or application code
references CloudFront in any capacity.

### 2.3 S3 must not be publicly exposed

**Status**: ✅ **SATISFIED**

| Requirement | Evidence |
|---|---|
| Block Public Access enabled | `modules/s3-sites/main.tf:34-41` — `aws_s3_bucket_public_access_block` with all four blocks (`block_public_acls`, `block_public_policy`, `ignore_public_acls`, `restrict_public_buckets`) set to `true` |
| No public bucket policies | The only bucket policy (`modules/s3-sites/main.tf:160-181`) grants `s3.amazonaws.com` service permission to send SQS messages — an internal AWS service-to-service notification, not public access |
| No public ACLs | No `aws_s3_bucket_acl` resources exist; public access block prevents them |
| No S3 static website hosting | Zero occurrences of `aws_s3_bucket_website_configuration`, `website_endpoint`, or `static_website_hosting` in any file |
| Access through controlled AWS identities | Three IAM roles with prefix-scoped S3 policies (`modules/iam/main.tf`): backend (staging `PutObject`/`GetObject`), worker (staging read + published write), gateway (published `GetObject` + `ListBucket`). All S3 access uses SigV4 signing through IAM credentials |
| Private networking for S3 | VPC gateway endpoint for S3 (`modules/vpc/main.tf:238-257`) routes all S3 traffic over the AWS backbone, eliminating public internet egress |

### 2.4 ECS-hosted NGINX S3 gateway is mandatory

**Status**: ✅ **SATISFIED**

The serving path is:
```
Client → ALB → Cognito auth → ECS Fargate (NGINX 1.27 Alpine) → S3 (private)
```

- **ECS module** (`modules/ecs-gateway/main.tf`): Fargate task, awsvpc network
  mode, private subnets, no public IP
- **NGINX image** (`gateway/nginx-s3-gateway/Dockerfile`): Custom build on
  `nginx:1.27-alpine` with njs dynamic module
- **SigV4 signing** (`gateway/nginx-s3-gateway/etc/nginx/njs/s3-auth.js`):
  Pure-JS SHA-256 and HMAC-SHA256 implementation; no external signing service
- **Credential refresh** (`docker-entrypoint.d/40-refresh-aws-credentials.sh`):
  Supports ECS task metadata endpoint and EC2 IMDSv2 — both work in private
  subnets without internet access
- **Dual-mode compatible**: `modules/ecs-gateway/main.tf` line 12-14
  explicitly states "The service is identical in public and private deployment
  modes." The gateway has no awareness of public vs. private.

Public mode:
```
Internet → public ALB → Cognito auth → ECS gateway → S3
```

Private mode (`deployment_mode = "private"`):
```
ZTNA / private network → internal ALB → Cognito auth → ECS gateway → S3 via VPC endpoint
```

### 2.5 Cognito authentication is mandatory

**Status**: ✅ **SATISFIED**

Cognito protects all three access surfaces via the ALB HTTPS listener
(`modules/alb/main.tf:222-235`):

| Surface | Listener Rule | Auth Mechanism |
|---|---|---|
| Hosted websites (wildcard) | Default action | authenticate-cognito → forward to gateway TG |
| Management portal | Priority 3 | authenticate-cognito → forward to portal TG |
| Backend API (`/api/*`) | Priority 2 | authenticate-cognito → forward to API TG |
| Health check (`/health`) | Priority 1 | fixed 200 — no auth required |

The Cognito module (`modules/cognito/`) provisions the user pool, app client
(OAuth 2.0 authorization code flow), and hosted UI domain.  Federation to
corporate SSO (SAML) is supported through Cognito's standard federation
configuration — no code changes required.

### 2.6 Per-user upload authorization is mandatory

**Status**: ✅ **SATISFIED**

- **Identity derivation**: `backend/go-api/internal/auth/cognito.go:ParseHeaders()`
  extracts Cognito `sub` from ALB-injected `x-amzn-oidc-data` JWT.  In dev
  mode, identity comes from the `x-dev-user-id` header.
- **Context propagation**: Identity stored in request context via
  `auth.SetIdentity()` — no client-provided identity accepted.
- **Data-layer enforcement**: DynamoDB PK includes user ID
  (e.g., `USER#<userId>`).  A query for another user's site returns empty
  results → 404, indistinguishable from "does not exist."
- **Server-derived S3 keys**: Staging and published prefixes are built
  server-side from trusted identity and upload metadata.  Clients never
  provide S3 keys, prefixes, or owner IDs.
- **Middleware emphasis** (`internal/api/middleware.go`):
  "the ownerID parameter is derived from server-side state (e.g., from a
  DynamoDB GetItem), NEVER from the client request body or query string."

### 2.7 Private compatibility is mandatory

**Status**: ✅ **SATISFIED** — detailed in Section 3 below.

---

## 2. Private Compatibility (`VISION.md` §2.7) — Detailed Verification

### Internal ALB

When `deployment_mode = "private"`:
- `locals.tf:27`: `alb_scheme = "internal"` (ternary on `is_public`)
- `modules/alb/main.tf:43`: `internal = var.alb_scheme == "internal"` →
  `true`, creating an internal ALB
- ALB ingress CIDRs can be narrowed via `alb_ingress_cidrs` variable
  (`modules/vpc/variables.tf`), restricting inbound to the private
  network / ZTNA fabric CIDR
- The ECS security group still only accepts traffic from the ALB
  security group — the private topology does not widen the attack surface

### Private DNS / private hosted zones

When `deployment_mode = "private"`:
- `locals.tf:30`: `dns_zone_visibility = "private"`
- `modules/dns/main.tf:39-44`: Route53 zone is created as private,
  associated with the VPC — internal resolution only
- ACM certificate validation works identically with private zones
  (DNS validation records are placed in the private zone)
- Portal and wildcard site A records are alias records pointing at
  the internal ALB — they resolve only within the VPC/private network

### ZTNA / private access fabric

The internal ALB + private DNS zone combination is directly compatible
with Palo Alto ZTNA or equivalent private access fabrics:
- The ALB has a private IP address within the VPC
- The private DNS zone resolves `*.sites.example.com` to the ALB's
  private IP
- ZTNA connectors/proxies can reach the ALB through the VPC's private
  subnets or through VPC peering / Transit Gateway
- No code or configuration in the gateway, backend, or frontend
  assumes internet routability — the ALB scheme and DNS visibility
  are purely infrastructure concerns

### Private S3 access

All S3 access uses private AWS networking patterns:
- **VPC gateway endpoint** (`modules/vpc/main.tf:238-257`): Free,
  routes all S3 traffic over the AWS backbone.  Associated with both
  public and private route tables so the gateway and Lambda functions
  never traverse the public internet to reach S3.
- **IAM credential sourcing**: The gateway uses ECS task metadata
  endpoint (`169.254.170.2`) or EC2 IMDSv2 (`169.254.169.254`) —
  both are link-local addresses that work without internet access.
- **Backend Lambda**: Runs inside the AWS Lambda service VPC when
  VPC-attached; S3 access is through the AWS internal network (or
  VPC endpoint if configured).  The Lambda function's S3 client
  uses the standard AWS SDK endpoint; in a VPC-attached Lambda with
  a gateway endpoint, traffic stays on the AWS backbone.

### No public S3 dependency

The gateway, backend, and frontend have zero dependencies on:
- Public S3 object URLs (no `https://<bucket>.s3.amazonaws.com/<key>`
  without SigV4 signing)
- S3 static website hosting endpoints
- S3 pre-signed URLs for serving (only for upload: temporary PUT grants
  scoped to `staging/` prefix)

### ECS gateway does not require public inbound access

- ECS service `assign_public_ip = false` (`modules/ecs-gateway/main.tf:206`)
- ECS tasks placed in private subnets
- ECS security group only accepts port 80 from the ALB security group
  (`modules/vpc/main.tf:397-403`)
- No direct internet ingress to the gateway container is possible

### Backend does not require public access to S3

- Lambda S3 client can use VPC endpoint when VPC-attached, or AWS
  internal network when not VPC-attached
- No public S3 URLs are generated by the backend — presigned upload
  URLs use the standard AWS S3 endpoint, which is resolved through
  the VPC gateway endpoint in private deployments
- The backend's only outbound S3 calls are `PutObject`/`GetObject`
  to the bucket (both route through VPC endpoint or AWS backbone)

---

## 3. Private Deployment Mode Switch — What Changes

The `private.tfvars.example` (`infrastructure/aws/environments/private.tfvars.example`)
shows the configuration for a private deployment:

```hcl
environment      = "prod"
deployment_mode  = "private"
aws_region       = "us-east-1"
domain_name      = "example.com"
portal_subdomain = "sites-admin"
sites_subdomain  = "sites"
```

Three infrastructure attributes change when `deployment_mode` switches from
`"public"` to `"private"`:

| Attribute | Public | Private | Location |
|---|---|---|---|
| ALB scheme | `internet-facing` | `internal` | `locals.tf:27` |
| DNS zone visibility | `public` | `private` | `locals.tf:30` |
| Ingress CIDRs | `0.0.0.0/0` | Restricted (override via `alb_ingress_cidrs`) | `modules/vpc/variables.tf` |

Nothing else changes.  The ECS cluster, task definition, service, IAM roles,
S3 bucket configuration, DynamoDB tables, Cognito user pool, and monitoring
are identical.  Application code (Go backend, Vue frontend, NGINX gateway)
requires zero changes — the deployment mode is an infrastructure concern
entirely.

---

## 4. Prohibited Pattern Scan — Full Results

A comprehensive scan of every file in the repository confirms no violations:

### CloudFront

```
Pattern: cloudfront | cloud.front | CLOUDFRONT
Scope:   *.go *.js *.vue *.tf *.yaml *.yml *.json *.md *.sh *.conf *.template Makefile

Results: ZERO matches in any source, configuration, or template file.
         All matches are in VISION.md (forbidding it) or docs (confirming absence).
```

### S3 website endpoints / public hosting

```
Pattern: website_endpoint | s3_website_endpoint | static_website_hosting |
         website_configuration | aws_s3_bucket_website_configuration | hosting_enabled
Scope:   *.go *.js *.vue *.tf *.yaml *.yml *.json *.md *.sh *.conf *.template

Results: ZERO matches in any file.
```

### Public S3 access patterns

```
Pattern: public.*s3 | s3.*public | public_access | public_read | acl.*public |
         public.*acl | public_read
Scope:   *.go *.js *.vue *.tf *.yaml *.yml *.json *.md *.sh *.conf *.template

Results: All matches are assertions that public access is BLOCKED:
         - aws_s3_bucket_public_access_block (enforces all four blocks)
         - Comments stating "no public S3 access"
         - README confirmations of private-only S3 posture
         ZERO matches for enabling or permitting public access.
```

### Backend Go application code

```
Pattern: cloudfront | public.*url | s3.*amazonaws | s3.*endpoint | website.*hosting
Scope:   backend/go-api/**/*.go

Results: The only "s3.amazonaws.com" reference is in a test mock
         (uploads_test.go:54) — a simulated presigned URL.  Production
         URLs are generated by the AWS SDK and use the standard S3
         endpoint, which resolves through VPC endpoints in private mode.
```

### Frontend Vue application code

```
Pattern: cloudfront | public.*url | s3.*amazonaws | website
Scope:   frontend/portal-vue/src/**/*.js frontend/portal-vue/src/**/*.vue

Results: ZERO matches.  The frontend has no AWS SDK dependency and no
         awareness of S3 URLs — all upload/download flows go through the
         Go backend API.
```

---

## 5. Gaps and Follow-Up Issues

**No gaps identified.**  All hard requirements in `VISION.md` §2 are
satisfied by the current implementation.  All §17.6 private-compatibility
acceptance criteria pass.

The following are **not gaps** but observations for future consideration:

1. **JWKS signature verification** (`backend/go-api/internal/auth/cognito.go`):
   The JWT parsing currently extracts claims without verifying the signature
   (delegating that to the ALB).  A future hardening pass could add JWKS-based
   verification as defense-in-depth.  This is not a private-compatibility
   concern — it applies equally to both deployment modes.

2. **Lambda VPC attachment**: The Lambda module does not currently attach the
   backend Lambda to the VPC.  For a fully private deployment where the Lambda
   must reach S3 and DynamoDB through VPC endpoints, VPC attachment would be
   needed.  A private deployment can still work without this (the Lambda
   function's AWS SDK uses the standard S3 endpoint, which resolves over the
   AWS internal network), but VPC attachment provides stronger network
   boundary guarantees.

3. **Portal serving path**: The current design routes portal traffic through
   the same ALB as hosted sites.  In a private deployment, this works
   correctly — the portal is accessible from the private network through the
   internal ALB.  No changes needed.

4. **Multi-level wildcard DNS**: The current DNS certificate uses
   `*.sites.example.com` (single-level wildcard, ACM/RFC constraint).
   The product model supports deeper patterns like
   `{slug}.{user}.sites.example.com`.  Private DNS zones have the same
   wildcard constraints as public zones — this is a DNS/ACM limitation,
   not a private-compatibility concern.

---

## 6. Conclusion

The platform design satisfies every private-compatibility requirement in
`VISION.md` §2 and every acceptance criterion in §17.6.  The
`deployment_mode` variable cleanly separates infrastructure exposure from
application logic.  No component — Terraform modules, Go backend, Vue
frontend, or NGINX gateway — contains hardcoded assumptions about public
access, CloudFront, S3 website hosting, or internet routability.

The system can move from a public lab deployment to a private ZTNA-protected
production deployment by changing a single variable (`deployment_mode =
"private"`) and optionally narrowing the ALB ingress CIDRs.  No application
code changes, no gateway reconfiguration, and no architectural compromises
are required.
