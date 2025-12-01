# ==============================================================================
# IAM module — Scoped roles and policies per platform component
#
# Three roles, each with least-privilege policies:
#   1. Backend API  — Lambda: presigned staging URLs, full DynamoDB CRUD
#   2. Worker       — Lambda: read staging, write published, update metadata
#   3. ECS Gateway  — ECS:   read published objects, read host mappings
#
# Frontend has no AWS credentials at all (VISION.md §13.4).
#
# See VISION.md §13.4 and §13.5 for security requirements.
# ==============================================================================

# ==============================================================================
# Backend API role — Lambda function for the Go serverless backend
#
# Responsibilities (VISION.md §8.2):
#   - CRUD on site metadata and upload records
#   - Issue presigned upload URLs scoped to staging/S3 keys
#   - Trigger validation/publish workflows
#
# Constraint: can write to staging/S3 prefix (for presigned URL signing) but
#             NOT to published/S3 (VISION.md §8.4, §8.5).
# ==============================================================================

resource "aws_iam_role" "backend_api" {
  name = "${var.name_prefix}-backend-api"
  path = "/platform/"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-backend-api"
    Component   = "backend"
    Description = "Backend API Lambda - presigned staging URLs and metadata CRUD"
  })
}

# Backend API policy — DynamoDB CRUD on both tables
resource "aws_iam_role_policy" "backend_dynamodb" {
  name = "${var.name_prefix}-backend-dynamodb"
  role = aws_iam_role.backend_api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
        ]
        Resource = [
          var.site_metadata_table_arn,
          var.upload_records_table_arn,
        ]
      },
    ]
  })
}

# Backend API policy — S3 staging access (presigned URL signing)
resource "aws_iam_role_policy" "backend_s3" {
  name = "${var.name_prefix}-backend-s3"
  role = aws_iam_role.backend_api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
        ]
        # The backend publishes directly (no worker): it reads staging and
        # writes the published version the gateway serves.
        Resource = [
          "${var.sites_bucket_arn}/staging/*",
          "${var.sites_bucket_arn}/published/*",
        ]
      },
    ]
  })
}

# Backend API policy — register each published site's
# https://<host>/oauth2/idpresponse on the ALB app client (Cognito has no
# wildcard callback URLs).
resource "aws_iam_role_policy" "backend_cognito" {
  name = "${var.name_prefix}-backend-cognito"
  role = aws_iam_role.backend_api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "cognito-idp:DescribeUserPoolClient",
          "cognito-idp:UpdateUserPoolClient",
        ]
        Resource = var.cognito_user_pool_arn
      },
    ]
  })
}

# Backend API policy — CloudWatch Logs (Lambda execution logging)
resource "aws_iam_role_policy" "backend_logs" {
  name = "${var.name_prefix}-backend-logs"
  role = aws_iam_role.backend_api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "arn:aws:logs:*:*:log-group:/aws/lambda/*"
      },
    ]
  })
}

# ==============================================================================
# Worker role — Lambda function for validation and publishing
#
# Responsibilities (VISION.md §8.5):
#   - Read staged uploads (extract and validate)
#   - Write published site versions (immutable versioned prefixes)
#   - Update upload status, validation results, and active version pointer
#
# Constraint: the ONLY role allowed to write to the published/ prefix.
# ==============================================================================

resource "aws_iam_role" "worker" {
  name = "${var.name_prefix}-worker"
  path = "/platform/"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-worker"
    Component   = "worker"
    Description = "Validation/publish worker Lambda - read staging and write published"
  })
}

# Worker policy — DynamoDB read and update (status updates, active version pointer)
resource "aws_iam_role_policy" "worker_dynamodb" {
  name = "${var.name_prefix}-worker-dynamodb"
  role = aws_iam_role.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:UpdateItem",
          "dynamodb:Query",
        ]
        Resource = [
          var.site_metadata_table_arn,
          var.upload_records_table_arn,
        ]
      },
    ]
  })
}

# Worker policy — SQS receive and delete (staging upload events)
resource "aws_iam_role_policy" "worker_sqs" {
  name = "${var.name_prefix}-worker-sqs"
  role = aws_iam_role.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes",
          "sqs:ChangeMessageVisibility",
        ]
        Resource = var.staging_events_queue_arn
      },
    ]
  })
}

# Worker policy — S3 staging read and published write
resource "aws_iam_role_policy" "worker_s3" {
  name = "${var.name_prefix}-worker-s3"
  role = aws_iam_role.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      # Read from staging to extract and validate uploads
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
        ]
        Resource = "${var.sites_bucket_arn}/staging/*"
      },
      # Write to published — the ONLY role permitted to do so
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
        ]
        Resource = "${var.sites_bucket_arn}/published/*"
      },
      # Clean up staging objects after successful publish
      {
        Effect = "Allow"
        Action = [
          "s3:DeleteObject",
        ]
        Resource = "${var.sites_bucket_arn}/staging/*"
      },
    ]
  })
}

# ==============================================================================
# ECS Gateway role — ECS task role for the NGINX S3 gateway
#
# Responsibilities (VISION.md §8.6):
#   - Map Host header to site metadata (host → owner → site → version → prefix)
#   - Fetch published site files from S3
#   - Serve index.html for directory requests
#
# Constraint: read-only access to published/ objects and host mappings.
# ==============================================================================

resource "aws_iam_role" "ecs_gateway" {
  name = "${var.name_prefix}-ecs-gateway"
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

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-ecs-gateway"
    Component   = "gateway"
    Description = "ECS NGINX S3 gateway - read published objects and host mappings"
  })
}

# Gateway policy — DynamoDB host-mapping lookup (single GetItem on HOST# pk).
# The gateway's host-resolver.js njs module queries DynamoDB with GetItem
# (not Query/Scan), so only GetItem is granted.
resource "aws_iam_role_policy" "gateway_dynamodb" {
  name = "${var.name_prefix}-gateway-dynamodb"
  role = aws_iam_role.ecs_gateway.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
        ]
        Resource = [
          var.site_metadata_table_arn,
        ]
      },
    ]
  })
}

# Gateway policy — S3 read-only on published content
resource "aws_iam_role_policy" "gateway_s3" {
  name = "${var.name_prefix}-gateway-s3"
  role = aws_iam_role.ecs_gateway.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      # Serve published site files
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
        ]
        Resource = "${var.sites_bucket_arn}/published/*"
      },
      # List published objects for directory index support
      {
        Effect = "Allow"
        Action = [
          "s3:ListBucket",
        ]
        Resource = [
          var.sites_bucket_arn,
        ]
        Condition = {
          StringLike = {
            "s3:prefix" = "published/*"
          }
        }
      },
    ]
  })
}
