# ==============================================================================
# ECS Gateway module outputs
# ==============================================================================

output "cluster_name" {
  description = "Name of the ECS cluster"
  value       = aws_ecs_cluster.gateway.name
}

output "cluster_arn" {
  description = "ARN of the ECS cluster"
  value       = aws_ecs_cluster.gateway.arn
}

output "service_name" {
  description = "Name of the ECS service"
  value       = aws_ecs_service.gateway.name
}

output "service_arn" {
  description = "ARN of the ECS service"
  value       = aws_ecs_service.gateway.id
}

output "task_definition_arn" {
  description = "ARN of the active task definition revision"
  value       = aws_ecs_task_definition.gateway.arn
}

output "task_definition_family" {
  description = "Family name of the task definition"
  value       = aws_ecs_task_definition.gateway.family
}

output "ecr_repository_url" {
  description = "URL of the ECR repository for the gateway image"
  value       = aws_ecr_repository.gateway.repository_url
}

output "ecr_repository_arn" {
  description = "ARN of the ECR repository"
  value       = aws_ecr_repository.gateway.arn
}

output "execution_role_arn" {
  description = "ARN of the task execution IAM role"
  value       = aws_iam_role.execution.arn
}

output "execution_role_name" {
  description = "Name of the task execution IAM role"
  value       = aws_iam_role.execution.name
}

output "log_group_name" {
  description = "Name of the CloudWatch Logs group for gateway container logs"
  value       = aws_cloudwatch_log_group.gateway.name
}
