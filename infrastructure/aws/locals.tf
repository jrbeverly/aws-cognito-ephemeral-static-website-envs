# ==============================================================================
# locals.tf — Naming, tags, and mode-dependent derived values
# ==============================================================================

locals {
  is_public = var.deployment_mode == "public"

  # ---------------------------------------------------------------------------
  # Common tags — applied to all taggable resources via provider default_tags
  # ---------------------------------------------------------------------------
  common_tags = {
    Project     = var.project_name
    Environment = var.environment
    ManagedBy   = "terraform"
  }

  # ---------------------------------------------------------------------------
  # Resource naming prefix
  # ---------------------------------------------------------------------------
  name_prefix = "${var.project_name}-${var.environment}"

  # ---------------------------------------------------------------------------
  # Deployment-mode-dependent values
  # ---------------------------------------------------------------------------

  # ALB
  alb_scheme = local.is_public ? "internet-facing" : "internal"

  # DNS
  dns_zone_visibility = local.is_public ? "public" : "private"

  # S3 — Gateway VPC endpoint required for private deployments with ECS over
  # private networking (no public S3 access per VISION.md §2.3)
  require_vpc_endpoints = !local.is_public

  # ---------------------------------------------------------------------------
  # Hostname patterns
  #
  # Lab example:
  #   Portal: sites-admin.example.com
  #   Sites:  *.sites.example.com
  # ---------------------------------------------------------------------------
  portal_fqdn  = "${var.portal_subdomain}.${var.domain_name}"
  sites_domain = "${var.sites_subdomain}.${var.domain_name}"
}
