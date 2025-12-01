variable "environment" {
  description = "Deployment environment name"
  type        = string
}

variable "deployment_mode" {
  description = "Deployment mode: 'public' or 'private'"
  type        = string
}

variable "name_prefix" {
  description = "Prefix applied to resource names"
  type        = string
}

variable "common_tags" {
  description = "Common tags applied to all resources"
  type        = map(string)
  default     = {}
}

variable "alb_scheme" {
  description = "ALB scheme: 'internet-facing' (public) or 'internal' (private)"
  type        = string
}

# ------------------------------------------------------------------------------
# Networking
# ------------------------------------------------------------------------------

variable "vpc_id" {
  description = "VPC ID where the ALB is deployed"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for the ALB (public subnets for internet-facing, private for internal)"
  type        = list(string)
}

variable "alb_security_group_id" {
  description = "Security group ID for the ALB (from the VPC module)"
  type        = string
}

# ------------------------------------------------------------------------------
# ACM certificate
# ------------------------------------------------------------------------------

variable "certificate_arn" {
  description = "ARN of the ACM certificate for HTTPS listeners"
  type        = string
}

# ------------------------------------------------------------------------------
# Cognito authentication
# ------------------------------------------------------------------------------

variable "cognito_user_pool_arn" {
  description = "ARN of the Cognito user pool for authenticate-cognito actions"
  type        = string
}

variable "cognito_user_pool_client_id" {
  description = "ID of the Cognito app client for authenticate-cognito actions"
  type        = string
}

variable "cognito_user_pool_domain" {
  description = "Cognito hosted UI domain prefix for authenticate-cognito actions"
  type        = string
}

# ------------------------------------------------------------------------------
# Routing — hostname-based request routing
# ------------------------------------------------------------------------------

variable "portal_fqdn" {
  description = "Fully qualified domain name for the management portal (used in host-based routing rules for origin isolation)"
  type        = string
}
