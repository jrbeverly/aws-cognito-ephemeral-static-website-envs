# ==============================================================================
# ECS Gateway module — NGINX S3 gateway for serving hosted sites
#
# Resources:
#   - ECR repository for the gateway container image
#   - ECS cluster (Fargate)
#   - Task execution IAM role (ECR pull + CloudWatch Logs write)
#   - Task definition (gateway image, env vars, awsvpc network mode)
#   - ECS service (private subnets, ALB target group, no public IP)
#   - CloudWatch Logs group for container logs
#
# The service is identical in public and private deployment modes.
# Mode-dependent behaviour (ALB scheme, DNS visibility, …) is handled
# by the calling root module — the ECS resources themselves do not change.
#
# See VISION.md §8.6, §13.4, §13.5.
# ==============================================================================

# ------------------------------------------------------------------------------
# ECR repository — stores the NGINX S3 gateway container image
# ------------------------------------------------------------------------------
resource "aws_ecr_repository" "gateway" {
  name         = "${var.name_prefix}-gateway"
  force_delete = true

  image_tag_mutability = "MUTABLE" # Lab convenience — allow "latest" tag updates

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = var.common_tags
}

locals {
  # When container_image is empty, default to the ECR repository + "latest" tag.
  container_image = var.container_image != "" ? var.container_image : "${aws_ecr_repository.gateway.repository_url}:latest"
}

# ------------------------------------------------------------------------------
# CloudWatch Logs group — gateway container logs
# ------------------------------------------------------------------------------
resource "aws_cloudwatch_log_group" "gateway" {
  name              = "/ecs/${var.name_prefix}-gateway"
  retention_in_days = var.log_retention_days

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# ECS Cluster
# ------------------------------------------------------------------------------
resource "aws_ecs_cluster" "gateway" {
  name = "${var.name_prefix}-gateway"

  setting {
    name  = "containerInsights"
    value = "disabled" # Disabled for lab — enable for production workloads
  }

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# Task execution IAM role — ECR pull + CloudWatch Logs write
#
# This is separate from the task role (var.ecs_gateway_role_arn) which grants
# the application-level S3 and DynamoDB permissions.  The execution role is
# used by the ECS agent to pull the container image and stream logs.
# ------------------------------------------------------------------------------
resource "aws_iam_role" "execution" {
  name = "${var.name_prefix}-gateway-execution"
  path = "/platform/"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = var.common_tags
}

resource "aws_iam_role_policy" "execution" {
  name = "${var.name_prefix}-gateway-execution"
  role = aws_iam_role.execution.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability",
        ]
        Resource = aws_ecr_repository.gateway.arn
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "${aws_cloudwatch_log_group.gateway.arn}:*"
      }
    ]
  })
}

# ------------------------------------------------------------------------------
# Task definition — Fargate, awsvpc network mode, gateway container
#
# Environment variables passed to the upstream nginx-s3-gateway container:
#   S3_BUCKET_NAME            — S3 bucket name (upstream-required; used by
#                               the entrypoint to compute S3_UPSTREAM)
#   S3_REGION                 — AWS region for the S3 endpoint and DynamoDB
#   SITE_METADATA_TABLE_NAME  — DynamoDB table queried by host-resolver.js
#                               for Host → S3 prefix resolution
#   RESOLVE_CACHE_TTL_SECONDS — shared-dict cache TTL for resolved prefixes
#
# The docker-entrypoint.sh bridge transforms these into the full set of
# configuration the upstream entrypoint expects (derives S3_SERVER, sets
# PROVIDE_INDEX_PAGE, recomputes S3_UPSTREAM/S3_HOST_HEADER).
#
# IAM credentials are obtained at runtime from the ECS task metadata endpoint
# by the upstream awscredentials.js njs module.  The task role
# (task_role_arn) must allow the gateway to read from S3 and DynamoDB.
# ------------------------------------------------------------------------------
resource "aws_ecs_task_definition" "gateway" {
  family                   = "${var.name_prefix}-gateway"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  task_role_arn            = var.ecs_gateway_role_arn
  execution_role_arn       = aws_iam_role.execution.arn

  container_definitions = jsonencode([
    {
      name  = "gateway"
      image = local.container_image

      portMappings = [
        {
          containerPort = var.container_port
          protocol      = "tcp"
        }
      ]

      environment = [
        { name = "S3_BUCKET_NAME", value = var.s3_bucket_name },
        { name = "S3_REGION", value = var.aws_region },
        { name = "SITE_METADATA_TABLE_NAME", value = var.site_metadata_table_name },
        { name = "RESOLVE_CACHE_TTL_SECONDS", value = var.resolve_cache_ttl_seconds },
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.gateway.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "gateway"
        }
      }

      # The gateway health check endpoint returns 200 on /health.
      # The ALB target group is already configured to use this path.
      healthCheck = {
        command     = ["CMD-SHELL", "curl -sf http://localhost:${var.container_port}/health || exit 1"]
        interval    = 15
        timeout     = 5
        retries     = 3
        startPeriod = 10
      }
    }
  ])

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# ECS Service — Fargate, private subnets, ALB attachment, no public IP
#
# VISION.md §8.6: "receive traffic only from the ALB security group"
# The ecs_security_group_id (from the VPC module) only allows inbound from
# the ALB security group on port 80.  Tasks are placed in private subnets
# with assign_public_ip = false — they cannot be reached from the internet.
# ------------------------------------------------------------------------------
resource "aws_ecs_service" "gateway" {
  name            = "${var.name_prefix}-gateway"
  cluster         = aws_ecs_cluster.gateway.id
  task_definition = aws_ecs_task_definition.gateway.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.private_subnet_ids
    security_groups  = [var.ecs_security_group_id]
    assign_public_ip = false # Tasks are accessed only through the ALB
  }

  load_balancer {
    target_group_arn = var.gateway_target_group_arn
    container_name   = "gateway"
    container_port   = var.container_port
  }

  deployment_maximum_percent         = 200
  deployment_minimum_healthy_percent = 100
  health_check_grace_period_seconds  = 30 # Allow time for credential fetch + NGINX startup

  enable_ecs_managed_tags = true

  tags = var.common_tags
}
