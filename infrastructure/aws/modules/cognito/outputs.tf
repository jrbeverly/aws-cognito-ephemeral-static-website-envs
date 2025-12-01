output "user_pool_id" {
  description = "ID of the Cognito user pool"
  value       = aws_cognito_user_pool.main.id
}

output "user_pool_arn" {
  description = "ARN of the Cognito user pool"
  value       = aws_cognito_user_pool.main.arn
}

output "user_pool_client_id" {
  description = "ID of the app client used by the ALB authenticate-cognito action"
  value       = aws_cognito_user_pool_client.alb.id
}

output "user_pool_domain" {
  description = "Cognito hosted UI domain prefix (for ALB authenticate-cognito user_pool_domain)"
  value       = aws_cognito_user_pool_domain.main.domain
}
