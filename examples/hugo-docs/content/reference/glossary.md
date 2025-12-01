---
title: "Glossary"
---

# Glossary

Key terms used across the platform and documentation.

**ALB**
: Application Load Balancer — the AWS service that routes incoming requests
and enforces authentication before forwarding traffic to the gateway or portal.

**Cognito**
: AWS managed identity service that handles user sign-up, sign-in, and
federation. All platform routes require Cognito authentication.

**ECS**
: Elastic Container Service — runs the NGINX S3 gateway as a managed
container workload.

**Gateway**
: The NGINX S3 gateway running on ECS. It maps incoming hostnames to S3
object keys, signs requests with AWS SigV4, and serves static files.

**Hugo**
: A static site generator written in Go. Builds complete HTML sites from
Markdown content and templates.

**Namespace**
: A per-user, per-site identifier that isolates content. A user can only
manage sites within namespaces they own.

**SigV4**
: Signature Version 4 — the AWS signing protocol used by the gateway to
authenticate every S3 request without static credentials.

**S3**
: Simple Storage Service — AWS object storage used for staging uploads and
serving published site content. S3 is never exposed directly to the public.

**Validation**
: The process that inspects an uploaded zip or HTML file before publishing.
Checks include path traversal prevention, file count limits, size limits,
and the presence of a root `index.html`.

[← Back to Reference](/reference/)
