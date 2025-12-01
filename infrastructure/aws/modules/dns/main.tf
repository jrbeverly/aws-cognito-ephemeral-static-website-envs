# ==============================================================================
# DNS module — Route53 records, ACM certificates, and hostname patterns
#
# Provisions:
#   - Route53 hosted zone (public or private per deployment_mode)
#   - ACM certificate covering portal and hosted-site hostname patterns
#   - DNS validation records for the certificate
#   - Route53 alias records pointing portal and wildcard sites at the ALB
#
# Wildcard depth constraint:
#   ACM and Route53 do not support multi-level wildcards (e.g., *.*.example.com
#   is rejected).  The standard wildcard *.sites.example.com covers exactly
#   one label level — it matches the temporary compatibility alias
#   {siteSlug}--{userSlug}.sites.example.com but does NOT match the deeper
#   product model {siteSlug}.{userSlug}.sites.example.com.
#
#   The routing/metadata model is designed for the deeper pattern; when DNS
#   infrastructure supports it, the certificate can be replaced or augmented.
#   See VISION.md §6.1 for the full discussion.
# ==============================================================================

# ------------------------------------------------------------------------------
# Route53 hosted zone
# ------------------------------------------------------------------------------

locals {
  is_private_zone = var.dns_zone_visibility == "private"

  # ACM certificate domain names
  certificate_subject_alternative_names = [
    var.portal_fqdn,
    "*.${var.sites_domain}",
  ]
}

# The zone already exists in the account; deploy.sh discovers its name.
data "aws_route53_zone" "primary" {
  name         = var.domain_name
  private_zone = local.is_private_zone
}

# ------------------------------------------------------------------------------
# ACM certificate — covers portal and hosted-site hostnames
# ------------------------------------------------------------------------------

resource "aws_acm_certificate" "main" {
  domain_name               = var.sites_domain
  subject_alternative_names = local.certificate_subject_alternative_names
  validation_method         = "DNS"

  tags = var.common_tags

  lifecycle {
    create_before_destroy = true
  }
}

# ------------------------------------------------------------------------------
# Route53 validation records for ACM DNS validation
# ------------------------------------------------------------------------------

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.main.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      type   = dvo.resource_record_type
      record = dvo.resource_record_value
    }
  }

  # sites.<domain> and *.sites.<domain> share one validation CNAME.
  allow_overwrite = true
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.record]
  ttl             = 60
  zone_id         = data.aws_route53_zone.primary.zone_id
}

resource "aws_acm_certificate_validation" "main" {
  certificate_arn         = aws_acm_certificate.main.arn
  validation_record_fqdns = [for record in aws_route53_record.cert_validation : record.fqdn]
}

# ------------------------------------------------------------------------------
# Portal DNS record — ALIAS to the Application Load Balancer
# ------------------------------------------------------------------------------

resource "aws_route53_record" "portal" {
  count = var.alb_dns_name != null ? 1 : 0

  zone_id = data.aws_route53_zone.primary.zone_id
  name    = var.portal_fqdn
  type    = "A"

  alias {
    name                   = var.alb_dns_name
    zone_id                = var.alb_zone_id
    evaluate_target_health = true
  }
}

# ------------------------------------------------------------------------------
# Hosted-sites wildcard record — ALIAS to the ALB
# ------------------------------------------------------------------------------

resource "aws_route53_record" "sites" {
  count = var.alb_dns_name != null ? 1 : 0

  zone_id = data.aws_route53_zone.primary.zone_id
  name    = "*.${var.sites_domain}"
  type    = "A"

  alias {
    name                   = var.alb_dns_name
    zone_id                = var.alb_zone_id
    evaluate_target_health = true
  }
}
