# ALB

Application Load Balancer with HTTPS listeners and Cognito authentication
rules.  Unauthenticated requests are redirected to the Cognito hosted UI;
the `/health` path and `/oauth2/idpresponse` callback are allowed without
authentication.

The `alb_scheme` input controls public vs private exposure per
`VISION.md` §2.7.

## Resources

| Resource | Purpose |
|---|---|
| ALB | internet-facing or internal HTTPS load balancer |
| Gateway target group | IP-type target group for ECS Fargate NGINX S3 gateway |
| Portal target group | IP-type target group for the Vue.js management portal |
| Backend API target group | IP-type target group for the Go serverless API |
| HTTP listener (port 80) | redirect all plain-text traffic to HTTPS |
| HTTPS listener (port 443) | Cognito authenticate-cognito → host-based routing → target groups |
| Health check rule | fixed 200 response on `/health` (no auth required) |
| API path rule | portal hostname + `/api/*` → auth → API target group (priority 2) |
| Portal hostname rule | portal hostname → auth → API Lambda target group, which also serves the portal (priority 3) |

The Cognito callback path `/oauth2/idpresponse` is handled automatically
by the ALB `authenticate-cognito` action — no explicit listener rule is
needed.

## Routing model

| Priority | Condition | Actions |
|---|---|---|
| 1 | path `/health` (any hostname) | fixed 200 — no auth |
| 2 | host = portal + path `/api/*` | authenticate-cognito → API target group |
| 3 | host = portal | authenticate-cognito → API Lambda target group |
| default | all other hostnames | authenticate-cognito → gateway target group |

The default action catches all hosted-site wildcard hostnames
(`*.sites.<domain>`) and routes them through Cognito authentication to
the gateway target group.

## Origin isolation

Per `VISION.md` §6.2, user-generated content must not share an origin
with the management portal or its APIs:

- The portal hostname routes to the **API Lambda target group** (the Lambda serves the built portal).
- Hosted-site wildcard hostnames route to the **gateway target group**
  (via the default action).
- API paths (`/api/*`) use a **compound host + path condition** so they
  are only routable on the portal hostname — hosted-site hostnames
  cannot reach them.

These are separate target groups with separate origins, so a script
running on a hosted site cannot make same-origin requests to the portal
or backend API.

## Target groups

All target groups use IP target type for ECS Fargate (awsvpc network mode).
Targets are registered by the ECS service when the respective modules
(portal, gateway, backend) are implemented.  Until then the target groups
have no healthy targets and the ALB returns 503 for forwarded requests, but
authentication (the Cognito redirect flow) works regardless.

See `VISION.md` §2.7, §6.2, and §8.7 for requirements.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
