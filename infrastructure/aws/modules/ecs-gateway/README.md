# ECS Gateway

NGINX S3 gateway running on Amazon ECS Fargate. Maps `Host` headers to S3 prefixes
and serves static site content through platform-controlled AWS permissions.

Must be deployable in both public (internet-facing ALB) and private
(internal ALB behind ZTNA) modes.

## Resources

| Resource | Purpose |
|---|---|
| `aws_ecr_repository` | Stores the NGINX S3 gateway container image |
| `aws_ecs_cluster` | Fargate cluster for the gateway service |
| `aws_ecs_task_definition` | Task definition (awsvpc, Fargate, 256 CPU / 512 MB) |
| `aws_ecs_service` | Service with ALB target group attachment, private subnets, no public IP |
| `aws_iam_role` (execution) | Task execution role for ECR pull and CloudWatch Logs write |
| `aws_cloudwatch_log_group` | Container log group with configurable retention |

## Security

- Tasks run in private subnets with `assign_public_ip = false`.
- The ECS task security group (from the VPC module) only allows inbound
  HTTP (80) from the ALB security group.
- S3 and DynamoDB traffic routes over VPC gateway endpoints (no public
  internet egress required).
- ECR image pulls and CloudWatch Logs streaming use VPC interface endpoints
  in private deployments.

See `VISION.md` §8.6 for requirements.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
