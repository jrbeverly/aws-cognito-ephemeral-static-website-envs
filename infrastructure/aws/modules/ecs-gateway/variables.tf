# ==============================================================================
# ECS Gateway module — NGINX S3 gateway for serving hosted sites
#
# Variables consumed by this module.  Values that differ between public and
# private deployment modes are set at the root level and passed through;
# the ECS service configuration itself is identical in both modes.
# ==============================================================================

# ------------------------------------------------------------------------------
# Environment
# ------------------------------------------------------------------------------
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

# ------------------------------------------------------------------------------
# Networking — provided by the VPC module
# ------------------------------------------------------------------------------
variable "vpc_id" {
  description = "ID of the platform VPC"
  type        = string
}

variable "private_subnet_ids" {
  description = "IDs of private subnets — ECS tasks run here"
  type        = list(string)
}

variable "ecs_security_group_id" {
  description = "ID of the ECS task security group (accepts traffic only from ALB SG)"
  type        = string
}

# ------------------------------------------------------------------------------
# ALB attachment — provided by the ALB module
# ------------------------------------------------------------------------------
variable "gateway_target_group_arn" {
  description = "ARN of the ALB target group for hosted-site traffic"
  type        = string
}

# ------------------------------------------------------------------------------
# IAM — provided by the IAM module
# ------------------------------------------------------------------------------
variable "ecs_gateway_role_arn" {
  description = "ARN of the ECS gateway IAM role (S3 read + DynamoDB host lookups)"
  type        = string
}

# ------------------------------------------------------------------------------
# AWS configuration — passed as environment variables to the gateway container.
#
# The task definition maps these to the upstream-native S3_REGION and
# S3_BUCKET_NAME environment variables.  A docker-entrypoint.sh bridge
# derives S3_SERVER from S3_REGION and recomputes S3_UPSTREAM before
# delegating to the upstream entrypoint.
# ------------------------------------------------------------------------------
variable "aws_region" {
  description = "AWS region for all platform resources (mapped to S3_REGION env var)"
  type        = string
}

variable "s3_bucket_name" {
  description = "Name of the S3 bucket for site content (mapped to S3_BUCKET_NAME env var)"
  type        = string
}

variable "site_metadata_table_name" {
  description = "Name of the DynamoDB metadata table for host-to-prefix lookups"
  type        = string
}

variable "resolve_cache_ttl_seconds" {
  description = "Host-resolution cache TTL in seconds (positive entries and disabled/no-version negative entries)"
  type        = string
  default     = "5"
}

# ------------------------------------------------------------------------------
# Container image
# ------------------------------------------------------------------------------
variable "container_image" {
  description = "URI of the gateway container image (ECR repository URL or Docker Hub reference)"
  type        = string
  default     = "" # Defaults to the module-managed ECR repository when empty
}

# ------------------------------------------------------------------------------
# Task sizing — Fargate CPU/memory combos
#
# Valid combos for awsvpc + FARGATE:
#   256/512, 256/1024, 512/1024, 512/2048, 1024/2048, 1024/3072, …
# ------------------------------------------------------------------------------
variable "task_cpu" {
  description = "CPU units for the Fargate task (256 = 0.25 vCPU)"
  type        = number
  default     = 256
}

variable "task_memory" {
  description = "Memory for the Fargate task in MiB"
  type        = number
  default     = 512
}

variable "container_port" {
  description = "Port the gateway container listens on"
  type        = number
  default     = 80
}

# ------------------------------------------------------------------------------
# Service scaling
# ------------------------------------------------------------------------------
variable "desired_count" {
  description = "Number of gateway tasks to run"
  type        = number
  default     = 1
}

# ------------------------------------------------------------------------------
# Logging
# ------------------------------------------------------------------------------
variable "log_retention_days" {
  description = "CloudWatch Logs retention period in days"
  type        = number
  default     = 30
}
