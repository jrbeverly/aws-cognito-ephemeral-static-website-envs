# DNS

Route53 records and ACM certificates for the management portal and hosted-site hostname patterns.

The `dns_zone_visibility` input controls public vs private hosted zones
per `VISION.md` §2.7 and §6.

See `VISION.md` §6 for hostname and namespace requirements.

## Hostname patterns

| Host | Pattern | Certificate coverage |
|---|---|---|
| Portal | `sites-admin.example.com` | SAN on primary cert |
| Hosted sites (alias) | `{slug}--{user}.sites.example.com` | `*.sites.example.com` |
| Hosted sites (product model) | `{slug}.{user}.sites.example.com` | Not yet covered (see below) |

## Wildcard depth constraint

The standard DNS wildcard `*.sites.example.com` matches exactly **one**
label in place of the `*`.  It covers:

- `foobar--myname.sites.example.com` ✓ (compatibility alias)
- `foobar.sites.example.com` ✓ (one label)

It does **not** cover:

- `foobar.myname.sites.example.com` ✗ (two labels)

AWS ACM explicitly rejects multi-level wildcards (`*.*.sites.example.com`),
and RFC 6125 §6.4.3 does not permit them in certificates.  This is a
fundamental DNS/SAN constraint, not an AWS limitation.

### Compatibility alias

The temporary compatibility alias uses `--` as a user/site separator so
both parts fit under one DNS label:

```text
https://foobar--myname.sites.example.com/
```

### Future deeper pattern

The routing and metadata model (`VISION.md` §6) is designed for the
deeper hostname pattern:

```text
https://{siteSlug}.{userSlug}.sites.example.com/
```

Enabling this requires one of:

- Individual certificates per user or per site (managed programmatically)
- A CA-issued wildcard at the parent zone (not available through ACM)
- DNS and ZTNA infrastructure that supports multi-level wildcard routing
  independent of the TLS certificate subject name

The module is structured so the certificate can be replaced or augmented
without changing the routing and metadata model.

## Resources

- `aws_route53_zone` — Public or private hosted zone for the root domain
- `aws_acm_certificate` — TLS certificate with portal + wildcard SANs
- `aws_route53_record` (cert validation) — DNS records for ACM validation
- `aws_acm_certificate_validation` — Waits for validation to complete
- `aws_route53_record` (portal) — ALIAS A record to the ALB
- `aws_route53_record` (sites wildcard) — ALIAS A record to the ALB

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
