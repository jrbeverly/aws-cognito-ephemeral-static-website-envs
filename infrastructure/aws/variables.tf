# ==============================================================================
# variables.tf — Root module variables
# ==============================================================================

# ------------------------------------------------------------------------------
# Deployment mode
# ------------------------------------------------------------------------------
variable "deployment_mode" {
  description = "Deployment mode: 'public' for internet-facing lab, 'private' for internal/ZTNA-compatible deployment"
  type        = string
  default     = "public"

  validation {
    condition     = contains(["public", "private"], var.deployment_mode)
    error_message = "deployment_mode must be 'public' or 'private'."
  }
}

# ------------------------------------------------------------------------------
# AWS
# ------------------------------------------------------------------------------
variable "aws_region" {
  description = "AWS region for all platform resources"
  type        = string
  default     = "us-east-1"
}

# ------------------------------------------------------------------------------
# Environment
# ------------------------------------------------------------------------------
variable "environment" {
  description = "Deployment environment name (e.g., lab, prod)"
  type        = string
  default     = "lab"
}

# ------------------------------------------------------------------------------
# Project
# ------------------------------------------------------------------------------
variable "project_name" {
  description = "Short project identifier used in resource naming"
  type        = string
  default     = "sites-platform"
}

# ------------------------------------------------------------------------------
# DNS
# ------------------------------------------------------------------------------
variable "domain_name" {
  description = "Root domain for hosted sites and management portal"
  type        = string
  default     = "example.com"
}

variable "portal_subdomain" {
  description = "Subdomain for the management portal"
  type        = string
  default     = "sites-admin"
}

variable "sites_subdomain" {
  description = "Subdomain prefix for hosted user sites"
  type        = string
  default     = "sites"
}

# ------------------------------------------------------------------------------
# ECS Gateway
# ------------------------------------------------------------------------------
variable "gateway_container_image" {
  description = "Override the gateway container image URI (defaults to the module-managed ECR repository when empty)"
  type        = string
  default     = ""
}
