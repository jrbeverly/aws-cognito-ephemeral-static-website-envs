# Go Serverless Backend

The backend API for the self-service static site hosting platform.

## Responsibility

- Read Cognito identity claims and map users to platform namespaces
- Site CRUD: create, list, delete sites
- Issue presigned upload URLs scoped to staging prefixes
- Trigger validation and publish workflows
- Enforce per-user ownership and authorization (server-side)

## Boundaries

- Must never trust client-provided owner IDs, S3 keys, or publishing destinations
- All authorization decisions are server-side — never rely on frontend behaviour
- Staging prefixes are derived from authenticated identity and server-side metadata

## Technology

Written in Go, deployed as serverless AWS services (Lambda, API Gateway).
See `VISION.md` §8.2 for full backend requirements.
