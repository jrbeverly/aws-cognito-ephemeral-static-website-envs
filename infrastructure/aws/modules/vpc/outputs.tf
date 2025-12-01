# ==============================================================================
# VPC module outputs
# ==============================================================================

# ------------------------------------------------------------------------------
# VPC
# ------------------------------------------------------------------------------

output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.main.id
}

output "vpc_cidr" {
  description = "CIDR block of the VPC"
  value       = aws_vpc.main.cidr_block
}

# ------------------------------------------------------------------------------
# Subnets
# ------------------------------------------------------------------------------

output "public_subnet_ids" {
  description = "IDs of public subnets (one per AZ)"
  value       = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  description = "IDs of private subnets (one per AZ)"
  value       = aws_subnet.private[*].id
}

# ------------------------------------------------------------------------------
# Security groups
# ------------------------------------------------------------------------------

output "alb_security_group_id" {
  description = "ID of the ALB security group"
  value       = aws_security_group.alb.id
}

output "ecs_security_group_id" {
  description = "ID of the ECS task security group (accepts traffic only from ALB SG)"
  value       = aws_security_group.ecs.id
}

# ------------------------------------------------------------------------------
# VPC Endpoint IDs — consumed by other modules that need to reference them
# ------------------------------------------------------------------------------

output "s3_vpce_id" {
  description = "ID of the S3 gateway VPC endpoint"
  value       = aws_vpc_endpoint.s3.id
}

output "dynamodb_vpce_id" {
  description = "ID of the DynamoDB gateway VPC endpoint"
  value       = aws_vpc_endpoint.dynamodb.id
}
