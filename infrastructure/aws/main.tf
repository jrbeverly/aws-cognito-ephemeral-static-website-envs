# ==============================================================================
# main.tf — Module composition
#
# Each module maps to a platform component defined in VISION.md §8.
# ==============================================================================

# ------------------------------------------------------------------------------
# VPC — Network fabric for the platform (VISION.md §2.7, §8.6, §8.7, §15)
#
# Provides the VPC, subnets, route tables, NAT Gateways, VPC endpoints,
# and security groups consumed by the ALB, ECS, and Lambda modules.
# Must be provisioned first — downstream modules depend on its outputs.
# ------------------------------------------------------------------------------
module "vpc" {
  source = "./modules/vpc"

  environment     = var.environment
  deployment_mode = var.deployment_mode
  name_prefix     = local.name_prefix
  common_tags     = local.common_tags
  aws_region      = var.aws_region
}

# ------------------------------------------------------------------------------
# Cognito — Authentication and user management (VISION.md §8.1, §8.2)
#
# Provisions the user pool, app client, and hosted UI domain.  The pool and
# client are structured so that adding a SAML identity provider later is a
# configuration change (append provider name + add identity_provider resource)
# rather than an architectural redesign.  (VISION.md §2.5)
# ------------------------------------------------------------------------------
module "cognito" {
  source = "./modules/cognito"

  environment     = var.environment
  deployment_mode = var.deployment_mode
  name_prefix     = local.name_prefix
  common_tags     = local.common_tags
  portal_fqdn     = local.portal_fqdn
}

# ------------------------------------------------------------------------------
# S3 — Staging and published content storage (VISION.md §8.4, §8.5)
#
# Block Public Access is enforced by the module — no public buckets, policies,
# ACLs, or static website hosting endpoints.  (VISION.md §2.3)
# ------------------------------------------------------------------------------
module "s3_sites" {
  source = "./modules/s3-sites"

  environment     = var.environment
  deployment_mode = var.deployment_mode
  name_prefix     = local.name_prefix
  common_tags     = local.common_tags
  portal_fqdn     = local.portal_fqdn
}

# ------------------------------------------------------------------------------
# DynamoDB — Platform metadata store (VISION.md §8.3)
# ------------------------------------------------------------------------------
module "dynamodb" {
  source = "./modules/dynamodb"

  environment = var.environment
  name_prefix = local.name_prefix
  common_tags = local.common_tags
}

# ------------------------------------------------------------------------------
# ALB — Application Load Balancer with Cognito authentication (VISION.md §8.7)
# ------------------------------------------------------------------------------
module "alb" {
  source = "./modules/alb"

  environment                 = var.environment
  deployment_mode             = var.deployment_mode
  name_prefix                 = local.name_prefix
  common_tags                 = local.common_tags
  alb_scheme                  = local.alb_scheme
  vpc_id                      = module.vpc.vpc_id
  subnet_ids                  = local.is_public ? module.vpc.public_subnet_ids : module.vpc.private_subnet_ids
  alb_security_group_id       = module.vpc.alb_security_group_id
  certificate_arn             = module.dns.certificate_arn
  cognito_user_pool_arn       = module.cognito.user_pool_arn
  cognito_user_pool_client_id = module.cognito.user_pool_client_id
  cognito_user_pool_domain    = module.cognito.user_pool_domain
  portal_fqdn                 = local.portal_fqdn
}

# ------------------------------------------------------------------------------
# ECS Gateway — NGINX S3 gateway for serving hosted sites (VISION.md §8.6)
# ------------------------------------------------------------------------------
module "ecs_gateway" {
  source = "./modules/ecs-gateway"

  environment     = var.environment
  deployment_mode = var.deployment_mode
  name_prefix     = local.name_prefix
  common_tags     = local.common_tags

  # Networking
  vpc_id                = module.vpc.vpc_id
  private_subnet_ids    = module.vpc.private_subnet_ids
  ecs_security_group_id = module.vpc.ecs_security_group_id

  # ALB attachment
  gateway_target_group_arn = module.alb.gateway_target_group_arn

  # IAM
  ecs_gateway_role_arn = module.iam.ecs_gateway_role_arn

  # AWS configuration
  aws_region               = var.aws_region
  s3_bucket_name           = module.s3_sites.bucket_name
  site_metadata_table_name = module.dynamodb.site_metadata_table_name

  # Optional container image override (defaults to module-managed ECR repository)
  container_image = var.gateway_container_image
}

# ------------------------------------------------------------------------------
# Lambda — Serverless backend API and validation/publish workers (VISION.md §8.2, §8.5)
# ------------------------------------------------------------------------------
module "lambda" {
  source = "./modules/lambda"

  environment = var.environment
  name_prefix = local.name_prefix
  common_tags = local.common_tags

  # AWS configuration
  aws_region = var.aws_region

  # IAM
  backend_api_role_arn = module.iam.backend_api_role_arn

  # Resource references
  sites_bucket_name         = module.s3_sites.bucket_name
  site_metadata_table_name  = module.dynamodb.site_metadata_table_name
  upload_records_table_name = module.dynamodb.upload_records_table_name

  # Cognito
  cognito_user_pool_id = module.cognito.user_pool_id
  cognito_client_id    = module.cognito.user_pool_client_id

  # Routing
  sites_domain = local.sites_domain
}

# ------------------------------------------------------------------------------
# IAM — Scoped roles and policies per component (VISION.md §13.4)
#
# Three roles with least-privilege policies:
#   - Backend API:  presigned staging URLs, DynamoDB CRUD on both tables
#   - Worker:       read staging, write published (only role allowed), update metadata
#   - ECS Gateway:  read published objects, read host mappings
#
# Policies are constrained by S3 prefix and DynamoDB table ARN.
# No role grants broad administrative permissions. Frontend gets no credentials.
# ------------------------------------------------------------------------------
module "iam" {
  source = "./modules/iam"

  environment = var.environment
  name_prefix = local.name_prefix
  common_tags = local.common_tags

  sites_bucket_arn         = module.s3_sites.bucket_arn
  site_metadata_table_arn  = module.dynamodb.site_metadata_table_arn
  upload_records_table_arn = module.dynamodb.upload_records_table_arn
  staging_events_queue_arn = module.s3_sites.staging_events_queue_arn
  cognito_user_pool_arn    = module.cognito.user_pool_arn
}

# ------------------------------------------------------------------------------
# DNS — Route53 records and ACM certificates for portal and hosted sites
# (VISION.md §6, §15)
#
# Wildcard depth constraint: ACM does not support multi-level wildcards
# (*.*.example.com).  The certificate covers *.sites.<domain> (one level),
# which matches the compatibility alias {slug}--{user}.sites.<domain>.
# The routing/metadata model is designed for the deeper pattern
# {slug}.{user}.sites.<domain> when DNS infrastructure supports it (VISION.md §6.1).
# ------------------------------------------------------------------------------
module "dns" {
  source = "./modules/dns"

  environment         = var.environment
  deployment_mode     = var.deployment_mode
  name_prefix         = local.name_prefix
  common_tags         = local.common_tags
  domain_name         = var.domain_name
  portal_fqdn         = local.portal_fqdn
  sites_domain        = local.sites_domain
  dns_zone_visibility = local.dns_zone_visibility
  vpc_id              = module.vpc.vpc_id

  # ALB DNS name / zone ID are not passed here because it would create a
  # circular module dependency: ALB needs certificate_arn from DNS, DNS
  # needs dns_name from ALB.  The Route53 alias records are instead created
  # at the root level below.
}

# ------------------------------------------------------------------------------
# Monitoring — CloudWatch metric filters, alarms, and dashboard (VISION.md §14-15)
#
# Consumes log group names from the ECS Gateway and Lambda modules.  Metric
# filters extract structured metrics from access logs; alarms trigger on
# elevated error/failure rates.  EMF metrics emitted by the Go backend and
# worker are automatically captured by CloudWatch.
# ------------------------------------------------------------------------------
module "monitoring" {
  source = "./modules/monitoring"

  name_prefix = local.name_prefix
  common_tags = local.common_tags
  aws_region  = var.aws_region

  gateway_log_group_name = module.ecs_gateway.log_group_name
  backend_log_group_name = module.lambda.backend_log_group_name
}

# ------------------------------------------------------------------------------
# Route53 alias records — portal and hosted-sites wildcard → ALB
#
# These records live at the root level (rather than inside the DNS module)
# to avoid a circular module dependency: the ALB needs the ACM certificate
# ARN from the DNS module, and the DNS module would need the ALB DNS name
# for its alias records.  By placing the records here, both module outputs
# are available without a cycle.
# ------------------------------------------------------------------------------

resource "aws_route53_record" "portal" {
  zone_id = module.dns.zone_id
  name    = local.portal_fqdn
  type    = "A"

  alias {
    name                   = module.alb.dns_name
    zone_id                = module.alb.zone_id
    evaluate_target_health = true
  }
}

resource "aws_route53_record" "sites" {
  zone_id = module.dns.zone_id
  name    = "*.${local.sites_domain}"
  type    = "A"

  alias {
    name                   = module.alb.dns_name
    zone_id                = module.alb.zone_id
    evaluate_target_health = true
  }
}

# ------------------------------------------------------------------------------
# ALB → API Lambda — the Lambda serves /api/* and the portal on the portal host
# ------------------------------------------------------------------------------

resource "aws_lambda_permission" "alb" {
  statement_id  = "AllowALBInvoke"
  action        = "lambda:InvokeFunction"
  function_name = module.lambda.api_function_name
  principal     = "elasticloadbalancing.amazonaws.com"
  source_arn    = module.alb.api_target_group_arn
}

resource "aws_lb_target_group_attachment" "api" {
  target_group_arn = module.alb.api_target_group_arn
  target_id        = module.lambda.api_function_arn
  depends_on       = [aws_lambda_permission.alb]
}
