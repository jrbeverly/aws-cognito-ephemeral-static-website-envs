# Documentation

Architecture notes, design documents, and validation reports. The
authoritative vision document is `VISION.md` at the repository root.

## Design Documents

| Document | Description |
|---|---|
| `design-gateway-host-to-prefix-resolution.md` | Design for the njs module that resolves `Host` headers to S3 published prefixes via DynamoDB with `js_shared_dict` caching |
| `upstream-nginx-s3-gateway-gap-analysis-78.md` | Evaluation of the upstream nginx-s3-gateway image against VISION.md requirements; records the decision to extend rather than fork |

## Validation Reports

| Document | Description |
|---|---|
| `gateway-migration-validation-85.md` | Post-migration validation of the upstream-based gateway against all serving, security, and private-compatibility criteria |
| `acceptance-validation-24.md` | (Historical) Acceptance validation of the pre-migration custom gateway |
| `private-compatibility-review-26.md` | (Historical) Private-deployment compatibility review of the pre-migration custom gateway |

## Developer Guides

| Document | Description |
|---|---|
| `local-development.md` | How to run and test the platform locally using the devcontainer, LocalStack, and `make` targets |

## Evidence

The `evidence/` directory contains test artifacts used by validation documents
(build results, test zip files for security testing).

## Gateway Documentation

The gateway has its own README at `../gateway/nginx-s3-gateway/README.md`
covering architecture, configuration surface, host-to-prefix resolution,
security headers, build and lint commands, cutover procedure, and rollback
procedure.
