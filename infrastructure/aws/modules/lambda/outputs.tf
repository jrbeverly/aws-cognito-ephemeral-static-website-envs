# ==============================================================================
# Lambda module outputs — Function ARNs and API Gateway endpoint
# ==============================================================================

# ------------------------------------------------------------------------------
# Backend API Lambda
# ------------------------------------------------------------------------------
output "api_function_name" {
  description = "Name of the backend API Lambda function"
  value       = aws_lambda_function.api.function_name
}

output "api_function_arn" {
  description = "ARN of the backend API Lambda function"
  value       = aws_lambda_function.api.arn
}

output "api_function_invoke_arn" {
  description = "Invoke ARN of the backend API Lambda function"
  value       = aws_lambda_function.api.invoke_arn
}

# ------------------------------------------------------------------------------
# Validation/publish Worker (placeholder — implemented in a follow-up)
# ------------------------------------------------------------------------------
output "worker_function_name" {
  description = "Name of the validation/publish worker Lambda function (placeholder)"
  value       = null
}

output "worker_function_arn" {
  description = "ARN of the validation/publish worker Lambda function (placeholder)"
  value       = null
}

# ------------------------------------------------------------------------------
# Log group names
# ------------------------------------------------------------------------------
output "backend_log_group_name" {
  description = "Name of the backend API Lambda CloudWatch Logs group"
  value       = aws_cloudwatch_log_group.api.name
}
