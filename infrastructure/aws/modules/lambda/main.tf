# ==============================================================================
# Lambda module — Serverless backend API
#
# Provisions:
#   1. Lambda function (Go, arm64, provided.al2023 runtime)
#   2. CloudWatch log group
#
# The function is an ALB target (registered in the root main.tf); there is no
# API Gateway, so every request has passed the ALB's authenticate-cognito.
#
# The Lambda function is deployed with the backend_api IAM role (created by
# the IAM module).  Environment variables are injected for all configuration
# required by the backend (table names, bucket name, region, etc.).
#
# Architecture (VISION.md §8.2, §8.8):
#   ALB → Cognito auth → Lambda (serves /api/* and the built portal)
#
# Deployment note:
#   The Lambda deployment package (zip) is built by scripts/build-backend.sh
#   and referenced via var.lambda_source_path.  In CI, the build script runs
#   before terraform apply.  For local development, the function can be
#   invoked via the API Gateway endpoint or through the local HTTP server
#   mode (cmd/api/main.go without AWS_LAMBDA_RUNTIME_API).
# ==============================================================================

# ==============================================================================
# Lambda function — Go serverless backend API
#
# Runtime: provided.al2023 (custom Go runtime, arm64)
# Handler: bootstrap (the compiled Go binary)
# ==============================================================================

resource "aws_lambda_function" "api" {
  function_name = "${var.name_prefix}-api"
  description   = "Backend API for the self-service static site hosting platform"

  role          = var.backend_api_role_arn
  architectures = [var.lambda_architecture]

  runtime = "provided.al2023"
  handler = "bootstrap"

  memory_size = var.lambda_memory_mb
  timeout     = var.lambda_timeout_seconds

  filename         = var.lambda_source_path
  source_code_hash = fileexists(var.lambda_source_path) ? filebase64sha256(var.lambda_source_path) : null

  # --------------------------------------------------------------------------
  # Environment variables — injected by Terraform from module inputs
  #
  # These map to config.Load() in internal/config/config.go.  All are
  # configurable per environment through Terraform variables.
  # --------------------------------------------------------------------------
  environment {
    variables = {
      SITES_BUCKET         = var.sites_bucket_name
      SITE_METADATA_TABLE  = var.site_metadata_table_name
      UPLOAD_RECORDS_TABLE = var.upload_records_table_name
      COGNITO_USER_POOL_ID = var.cognito_user_pool_id
      COGNITO_CLIENT_ID    = var.cognito_client_id
      SITES_DOMAIN         = var.sites_domain
      ALIAS_ENABLED        = "true"
      LOG_LEVEL            = "info"
    }
  }

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-api"
    Component   = "backend"
    Description = "Backend API Lambda - site CRUD and presigned upload URLs"
  })
}

# ==============================================================================
# CloudWatch Logs — Lambda execution logs
# ==============================================================================

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${aws_lambda_function.api.function_name}"
  retention_in_days = var.lambda_log_retention_days

  tags = merge(var.common_tags, {
    Name = "/aws/lambda/${aws_lambda_function.api.function_name}"
  })
}
