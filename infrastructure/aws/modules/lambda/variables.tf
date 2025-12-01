# ==============================================================================
# Lambda module variables — Backend API function configuration
# ==============================================================================

# ------------------------------------------------------------------------------
# Naming and tags
# ------------------------------------------------------------------------------
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
# AWS configuration
# ------------------------------------------------------------------------------
variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

# ------------------------------------------------------------------------------
# IAM
# ------------------------------------------------------------------------------
variable "backend_api_role_arn" {
  description = "ARN of the backend API IAM role (created by the IAM module)"
  type        = string
}

# ------------------------------------------------------------------------------
# Resource references
# ------------------------------------------------------------------------------
variable "sites_bucket_name" {
  description = "Name of the S3 bucket for site content (staging and published)"
  type        = string
}

variable "site_metadata_table_name" {
  description = "Name of the site metadata DynamoDB table"
  type        = string
}

variable "upload_records_table_name" {
  description = "Name of the upload records DynamoDB table"
  type        = string
}

# ------------------------------------------------------------------------------
# Cognito
# ------------------------------------------------------------------------------
variable "cognito_user_pool_id" {
  description = "ID of the Cognito user pool (for JWKS verification in production)"
  type        = string
}

# ------------------------------------------------------------------------------
# Routing
# ------------------------------------------------------------------------------
variable "sites_domain" {
  description = "Base domain for hosted sites (e.g., sites.example.com)"
  type        = string
}

# ------------------------------------------------------------------------------
# Lambda configuration
# ------------------------------------------------------------------------------
variable "lambda_memory_mb" {
  description = "Lambda function memory allocation in MB"
  type        = number
  default     = 256
}

variable "lambda_timeout_seconds" {
  description = "Lambda function timeout in seconds"
  type        = number
  default     = 30
}

variable "lambda_log_retention_days" {
  description = "CloudWatch log group retention period in days"
  type        = number
  default     = 30
}

variable "lambda_architecture" {
  description = "Lambda instruction set architecture"
  type        = string
  default     = "arm64"
}

# ------------------------------------------------------------------------------
# Deployment package
# ------------------------------------------------------------------------------
variable "lambda_source_path" {
  description = "Path to the Lambda deployment package zip file (relative to the infrastructure/aws directory)"
  type        = string
  default     = "../../dist/backend-api.zip"
}

variable "cognito_client_id" {
  description = "ALB app client the backend registers published site callback URLs on"
  type        = string
}
