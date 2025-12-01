# ==============================================================================
# VPC module variables
# ==============================================================================

# ------------------------------------------------------------------------------
# Standard module variables
# ------------------------------------------------------------------------------

variable "environment" {
  description = "Deployment environment name"
  type        = string
}

variable "deployment_mode" {
  description = "Deployment mode: 'public' or 'private'"
  type        = string

  validation {
    condition     = contains(["public", "private"], var.deployment_mode)
    error_message = "deployment_mode must be 'public' or 'private'."
  }
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

# ------------------------------------------------------------------------------
# AWS
# ------------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region for VPC endpoint service names"
  type        = string
}

# ------------------------------------------------------------------------------
# VPC topology
# ------------------------------------------------------------------------------

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_count" {
  description = "Number of Availability Zones for subnets (2–3 recommended)"
  type        = number
  default     = 2

  validation {
    condition     = var.az_count >= 1 && var.az_count <= 6
    error_message = "az_count must be between 1 and 6."
  }
}

# ------------------------------------------------------------------------------
# NAT Gateway
# ------------------------------------------------------------------------------

variable "single_nat_gateway" {
  description = "Provision a single NAT Gateway instead of one per AZ (reduces cost for lab, creates single-AZ failure point)"
  type        = bool
  default     = false
}

# ------------------------------------------------------------------------------
# Security group ingress
# ------------------------------------------------------------------------------

variable "alb_ingress_cidrs" {
  description = "CIDR blocks allowed to reach the ALB on 80/443"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
