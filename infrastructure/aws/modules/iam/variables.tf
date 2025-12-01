# ==============================================================================
# IAM module variables — Scoped roles and policies per component
# ==============================================================================

variable "environment" {
  description = "Deployment environment name"
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
# Resource ARN references — used to scope policies to specific resources
# ------------------------------------------------------------------------------

variable "sites_bucket_arn" {
  description = "ARN of the S3 bucket for site content (staging + published)"
  type        = string
}

variable "site_metadata_table_arn" {
  description = "ARN of the site metadata DynamoDB table"
  type        = string
}

variable "upload_records_table_arn" {
  description = "ARN of the upload records DynamoDB table"
  type        = string
}

variable "staging_events_queue_arn" {
  description = "ARN of the SQS queue that receives staging upload S3 event notifications"
  type        = string
}

variable "cognito_user_pool_arn" {
  description = "User pool whose ALB app client the backend adds site callback URLs to"
  type        = string
}
