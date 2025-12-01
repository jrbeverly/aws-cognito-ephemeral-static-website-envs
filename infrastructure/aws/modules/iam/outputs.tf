# ==============================================================================
# IAM module outputs — Role ARNs and names for service attachment
# ==============================================================================

# ------------------------------------------------------------------------------
# Backend API
# ------------------------------------------------------------------------------
output "backend_api_role_arn" {
  description = "ARN of the backend API IAM role"
  value       = aws_iam_role.backend_api.arn
}

output "backend_api_role_name" {
  description = "Name of the backend API IAM role"
  value       = aws_iam_role.backend_api.name
}

# ------------------------------------------------------------------------------
# Validation/publishing Worker
# ------------------------------------------------------------------------------
output "worker_role_arn" {
  description = "ARN of the validation/publish worker IAM role"
  value       = aws_iam_role.worker.arn
}

output "worker_role_name" {
  description = "Name of the validation/publish worker IAM role"
  value       = aws_iam_role.worker.name
}

# ------------------------------------------------------------------------------
# ECS Gateway
# ------------------------------------------------------------------------------
output "ecs_gateway_role_arn" {
  description = "ARN of the ECS gateway IAM role"
  value       = aws_iam_role.ecs_gateway.arn
}

output "ecs_gateway_role_name" {
  description = "Name of the ECS gateway IAM role"
  value       = aws_iam_role.ecs_gateway.name
}
