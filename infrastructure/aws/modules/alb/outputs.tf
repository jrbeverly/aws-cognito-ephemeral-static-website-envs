output "dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = aws_lb.main.dns_name
}

output "arn" {
  description = "ARN of the Application Load Balancer"
  value       = aws_lb.main.arn
}

output "zone_id" {
  description = "Canonical hosted zone ID of the ALB (for Route53 alias records)"
  value       = aws_lb.main.zone_id
}

output "listener_arn" {
  description = "ARN of the HTTPS listener"
  value       = aws_lb_listener.https.arn
}

output "security_group_id" {
  description = "ID of the ALB security group (passed through from VPC module)"
  value       = var.alb_security_group_id
}

output "gateway_target_group_arn" {
  description = "ARN of the gateway target group (for ECS service attachment)"
  value       = aws_lb_target_group.gateway.arn
}

output "api_target_group_arn" {
  description = "ARN of the backend API target group (for ECS service or API Gateway VPC endpoint attachment)"
  value       = aws_lb_target_group.api.arn
}
