# ==============================================================================
# variables.tf — Monitoring module
# ==============================================================================

# ------------------------------------------------------------------------------
# Naming & tags
# ------------------------------------------------------------------------------
variable "name_prefix" {
  description = "Prefix for resource names (e.g., 'sites-platform-lab')"
  type        = string
}

variable "common_tags" {
  description = "Tags applied to all resources"
  type        = map(string)
}

# ------------------------------------------------------------------------------
# AWS
# ------------------------------------------------------------------------------
variable "aws_region" {
  description = "AWS region"
  type        = string
}

# ------------------------------------------------------------------------------
# Log group references
# ------------------------------------------------------------------------------
variable "gateway_log_group_name" {
  description = "Name of the ECS gateway CloudWatch Logs group"
  type        = string
}

variable "backend_log_group_name" {
  description = "Name of the backend API Lambda CloudWatch Logs group"
  type        = string
}

# ------------------------------------------------------------------------------
# Alarm thresholds — lab-appropriate defaults
# ------------------------------------------------------------------------------
variable "gateway_5xx_threshold" {
  description = "Number of 5xx errors in 5 minutes to trigger alarm"
  type        = number
  default     = 10
}

variable "backend_error_threshold" {
  description = "Number of ERROR log lines in 5 minutes to trigger alarm"
  type        = number
  default     = 10
}

variable "upload_failure_threshold" {
  description = "Number of upload failures in 5 minutes to trigger alarm"
  type        = number
  default     = 5
}

variable "validation_failure_threshold" {
  description = "Number of validation failures in 5 minutes to trigger alarm"
  type        = number
  default     = 5
}
