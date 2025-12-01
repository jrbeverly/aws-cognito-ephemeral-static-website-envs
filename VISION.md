# Per-user static sites behind ALB, Cognito and an ECS NGINX S3 gateway

Can an ALB that performs Cognito login, in front of an NGINX S3 gateway on ECS
Fargate, serve `https://<site>.<user>.<domain>/` from a private S3 prefix
`<user>/<site>/`, where the gateway derives the prefix from the `Host` header
and a user can publish only into their own prefix?

The serving path must be exactly:

```text
browser -> ALB (authenticate-cognito) -> ECS NGINX S3 gateway -> private S3
```

Hard constraints:

- no CloudFront anywhere;
- S3 Block Public Access on, no bucket policy granting public access, no S3
  website endpoint; only the gateway task role can read objects;
- nothing in the serving path may depend on being internet-facing, so the same
  design can later move behind a private network (an internal ALB) unchanged.

The experiment should:

1. deploy one stack: VPC, private bucket, Cognito user pool with two hardcoded
   test users, a wildcard ACM certificate for `*.*.<domain>` (or the nearest
   wildcard ALB supports), one ALB with an HTTPS listener, and an ECS Fargate
   service running the upstream `nginxinc/nginx-s3-gateway` image with a small
   host-to-prefix mapping added;
2. provide one publish path that enforces ownership: a small Lambda target on
   the same ALB (`<user>` taken from the Cognito identity the ALB passes in
   `x-amzn-oidc-data`, never from the request) that accepts a `.zip` or a
   single `index.html` and writes it under `<user>/<site>/`;
3. include two example sites (a Hugo build and a single `index.html`) and one
   script that publishes both as each test user.

Observations to record:

- a site is reachable only after Cognito login, and the right files come back
  for nested paths, `index.html` resolution and 404s;
- user A cannot publish into user B's prefix, and whether user A can *read*
  user B's site (the ALB authenticates but does not authorise per host; record
  what actually happens);
- direct S3 requests for the objects are denied;
- whether one ALB certificate covers two-level wildcard hostnames, or what had
  to change;
- Content-Type correctness for HTML, CSS, JS, fonts and images through the
  gateway;
- time from publish to the site being served.

Constraints: Terraform, one region hardcoded (`ca-central-1`), an existing
Route 53 hosted zone passed in as a variable, local state, and a teardown that
removes everything.

Out of scope unless results show a need: a portal UI, a separate validation or
publishing worker, SQS staging, a metadata database, pasted-HTML uploads,
content scanning, site deletion or expiry, observability dashboards, reusable
Terraform modules, local-development emulation, and the private (ZTNA)
deployment itself.
