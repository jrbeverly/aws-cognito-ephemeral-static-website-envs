# ==============================================================================
# outputs.tf — Root module outputs
# ==============================================================================

# ------------------------------------------------------------------------------
# Cognito
# ------------------------------------------------------------------------------
output "cognito_user_pool_id" {
  description = "ID of the Cognito user pool"
  value       = module.cognito.user_pool_id
}

output "cognito_user_pool_arn" {
  description = "ARN of the Cognito user pool"
  value       = module.cognito.user_pool_arn
}

output "cognito_user_pool_client_id" {
  description = "ID of the app client for ALB authenticate-cognito actions"
  value       = module.cognito.user_pool_client_id
}

output "cognito_user_pool_domain" {
  description = "Cognito hosted UI domain prefix"
  value       = module.cognito.user_pool_domain
}

# ------------------------------------------------------------------------------
# VPC
# ------------------------------------------------------------------------------
output "vpc_id" {
  description = "ID of the platform VPC"
  value       = module.vpc.vpc_id
}

output "vpc_cidr" {
  description = "CIDR block of the platform VPC"
  value       = module.vpc.vpc_cidr
}

output "public_subnet_ids" {
  description = "IDs of public subnets (one per AZ)"
  value       = module.vpc.public_subnet_ids
}

output "private_subnet_ids" {
  description = "IDs of private subnets (one per AZ)"
  value       = module.vpc.private_subnet_ids
}

output "alb_security_group_id" {
  description = "ID of the ALB security group"
  value       = module.vpc.alb_security_group_id
}

output "ecs_security_group_id" {
  description = "ID of the ECS task security group"
  value       = module.vpc.ecs_security_group_id
}

# ------------------------------------------------------------------------------
# ALB
# ------------------------------------------------------------------------------
output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = module.alb.dns_name
}

output "alb_arn" {
  description = "ARN of the Application Load Balancer"
  value       = module.alb.arn
}

# ------------------------------------------------------------------------------
# S3
# ------------------------------------------------------------------------------
output "sites_bucket_name" {
  description = "Name of the S3 bucket for site content"
  value       = module.s3_sites.bucket_name
}

output "sites_bucket_arn" {
  description = "ARN of the S3 bucket for site content"
  value       = module.s3_sites.bucket_arn
}

output "staging_events_queue_arn" {
  description = "ARN of the SQS queue for staging upload S3 event notifications"
  value       = module.s3_sites.staging_events_queue_arn
}

output "staging_events_queue_url" {
  description = "URL of the SQS queue for staging upload S3 event notifications"
  value       = module.s3_sites.staging_events_queue_url
}

output "staging_events_queue_name" {
  description = "Name of the SQS queue for staging upload S3 event notifications"
  value       = module.s3_sites.staging_events_queue_name
}

# ------------------------------------------------------------------------------
# DynamoDB
# ------------------------------------------------------------------------------
output "site_metadata_table_name" {
  description = "Name of the site metadata DynamoDB table"
  value       = module.dynamodb.site_metadata_table_name
}

output "site_metadata_table_arn" {
  description = "ARN of the site metadata DynamoDB table"
  value       = module.dynamodb.site_metadata_table_arn
}

output "upload_records_table_name" {
  description = "Name of the upload records DynamoDB table"
  value       = module.dynamodb.upload_records_table_name
}

output "upload_records_table_arn" {
  description = "ARN of the upload records DynamoDB table"
  value       = module.dynamodb.upload_records_table_arn
}

# ------------------------------------------------------------------------------
# ECS
# ------------------------------------------------------------------------------
output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = module.ecs_gateway.cluster_name
}

output "ecs_cluster_arn" {
  description = "ARN of the ECS cluster"
  value       = module.ecs_gateway.cluster_arn
}

output "ecs_service_name" {
  description = "Name of the ECS service"
  value       = module.ecs_gateway.service_name
}

output "ecs_service_arn" {
  description = "ARN of the ECS service"
  value       = module.ecs_gateway.service_arn
}

output "ecs_task_definition_arn" {
  description = "ARN of the active gateway task definition revision"
  value       = module.ecs_gateway.task_definition_arn
}

output "ecr_repository_url" {
  description = "URL of the ECR repository for the gateway image"
  value       = module.ecs_gateway.ecr_repository_url
}

output "ecs_execution_role_arn" {
  description = "ARN of the ECS task execution IAM role"
  value       = module.ecs_gateway.execution_role_arn
}

# ------------------------------------------------------------------------------
# Lambda
# ------------------------------------------------------------------------------
output "lambda_api_function_name" {
  description = "Name of the backend API Lambda function"
  value       = module.lambda.api_function_name
}

output "lambda_worker_function_name" {
  description = "Name of the validation/publish worker Lambda function"
  value       = module.lambda.worker_function_name
}

# ------------------------------------------------------------------------------
# IAM
# ------------------------------------------------------------------------------
output "backend_api_role_arn" {
  description = "ARN of the backend API IAM role"
  value       = module.iam.backend_api_role_arn
}

output "backend_api_role_name" {
  description = "Name of the backend API IAM role"
  value       = module.iam.backend_api_role_name
}

output "worker_role_arn" {
  description = "ARN of the validation/publish worker IAM role"
  value       = module.iam.worker_role_arn
}

output "worker_role_name" {
  description = "Name of the validation/publish worker IAM role"
  value       = module.iam.worker_role_name
}

output "ecs_gateway_role_arn" {
  description = "ARN of the ECS gateway IAM role"
  value       = module.iam.ecs_gateway_role_arn
}

output "ecs_gateway_role_name" {
  description = "Name of the ECS gateway IAM role"
  value       = module.iam.ecs_gateway_role_name
}

# ------------------------------------------------------------------------------
# Monitoring
# ------------------------------------------------------------------------------
output "cloudwatch_dashboard_name" {
  description = "Name of the CloudWatch dashboard for operator visibility"
  value       = module.monitoring.dashboard_name
}

output "cloudwatch_alarm_arns" {
  description = "Map of alarm names to ARNs"
  value       = module.monitoring.alarm_arns
}

# ------------------------------------------------------------------------------
# Platform
# ------------------------------------------------------------------------------
output "portal_url" {
  description = "URL of the management portal"
  value       = "https://${local.portal_fqdn}/"
}

output "sites_base_domain" {
  description = "Base domain for hosted user sites"
  value       = local.sites_domain
}
