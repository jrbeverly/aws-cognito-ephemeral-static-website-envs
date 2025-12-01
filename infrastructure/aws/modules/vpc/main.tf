# ==============================================================================
# VPC module — Network fabric for the platform
#
# Provisions:
#   - VPC with DNS hostnames and DNS support enabled
#   - Public and private subnets across multiple AZs
#   - Internet Gateway for public subnets
#   - NAT Gateway(s) for private subnet egress
#   - Route tables and associations
#   - VPC endpoints for private AWS service connectivity (S3, DynamoDB,
#     ECR, CloudWatch Logs)
#   - Security groups for the ALB and ECS tasks
#
# The VPC is the shared network layer consumed by the ALB, ECS, and Lambda
# modules.  In private deployment mode the ALB becomes internal and DNS
# moves to a private hosted zone, but the VPC topology is the same.
#
# VPC endpoints eliminate the need for public internet egress to reach S3
# and DynamoDB, satisfying VISION.md §2.3 (no public S3 access) and §2.7
# (private compatibility).  ECR and CloudWatch Logs endpoints are provisioned
# so ECS tasks can pull images and emit logs without a NAT Gateway in
# locked-down private deployments.
#
# See VISION.md §2.7, §8.6, §8.7, §13.5.
# ==============================================================================

# ------------------------------------------------------------------------------
# Availability Zones
# ------------------------------------------------------------------------------

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, var.az_count)
}

# ------------------------------------------------------------------------------
# VPC
# ------------------------------------------------------------------------------

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpc"
  })
}

# ------------------------------------------------------------------------------
# Internet Gateway
# ------------------------------------------------------------------------------

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-igw"
  })
}

# ------------------------------------------------------------------------------
# Public subnets — one per AZ
#
# Hosts the Application Load Balancer.  In public mode the ALB is
# internet-facing; in private mode it is internal but still placed in
# public subnets so it can be reached from peered networks or ZTNA fabric
# through the IGW.  The ALB module controls the scheme.
# ------------------------------------------------------------------------------

resource "aws_subnet" "public" {
  count = var.az_count

  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, count.index)
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = true

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-public-${local.azs[count.index]}"
    Tier = "public"
  })
}

# ------------------------------------------------------------------------------
# Private subnets — one per AZ
#
# Host ECS tasks and (via VPC configuration) Lambda functions.  No direct
# inbound internet access.  Outbound internet goes through the NAT Gateway
# in the same AZ.
# ------------------------------------------------------------------------------

resource "aws_subnet" "private" {
  count = var.az_count

  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index + var.az_count)
  availability_zone = local.azs[count.index]

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-private-${local.azs[count.index]}"
    Tier = "private"
  })
}

# ------------------------------------------------------------------------------
# Elastic IPs — one per NAT Gateway
# ------------------------------------------------------------------------------

resource "aws_eip" "nat" {
  count = var.single_nat_gateway ? 1 : var.az_count

  domain = "vpc"

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-nat-eip-${local.azs[count.index]}"
  })
}

# ------------------------------------------------------------------------------
# NAT Gateways — one per AZ (or single for cost-sensitive labs)
#
# Placed in public subnets so the NAT Gateway itself has a route to the
# IGW.  Private subnets route 0.0.0.0/0 through the NAT Gateway in the
# same AZ to keep traffic local.
#
# When single_nat_gateway is true only one NAT Gateway is provisioned in
# the first AZ and all private subnets route through it.  This reduces
# cost but creates a single-AZ failure point.
# ------------------------------------------------------------------------------

resource "aws_nat_gateway" "main" {
  count = var.single_nat_gateway ? 1 : var.az_count

  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-nat-${local.azs[count.index]}"
  })

  depends_on = [aws_internet_gateway.main]
}

# ------------------------------------------------------------------------------
# Public route table — routes 0.0.0.0/0 to the Internet Gateway
# ------------------------------------------------------------------------------

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-public-rt"
  })
}

resource "aws_route_table_association" "public" {
  count = var.az_count

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------------------
# Private route tables — one per AZ, routes 0.0.0.0/0 to the NAT Gateway
# in the same AZ (or the single NAT Gateway when single_nat_gateway is true)
# ------------------------------------------------------------------------------

resource "aws_route_table" "private" {
  count = var.az_count

  vpc_id = aws_vpc.main.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main[var.single_nat_gateway ? 0 : count.index].id
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-private-rt-${local.azs[count.index]}"
  })
}

resource "aws_route_table_association" "private" {
  count = var.az_count

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}

# ==============================================================================
# VPC Endpoints — Private AWS service connectivity
#
# Gateway endpoints (S3, DynamoDB) are free and route traffic through the
# AWS backbone instead of the public internet.  Interface endpoints (ECR,
# CloudWatch Logs) are needed so ECS tasks can pull images and emit logs
# without a NAT Gateway in private deployments.
# ==============================================================================

# ------------------------------------------------------------------------------
# Security group for VPC interface endpoints
# ------------------------------------------------------------------------------

resource "aws_security_group" "vpce" {
  name        = "${var.name_prefix}-vpce"
  description = "VPC interface endpoints - allows inbound HTTPS from the VPC CIDR"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "HTTPS from VPC"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce"
  })
}

# ------------------------------------------------------------------------------
# S3 Gateway endpoint — free, routes S3 traffic over the AWS backbone
#
# VISION.md §2.3 and §2.7 require no public S3 access.  The gateway
# endpoint lets the ECS gateway and Lambda workers reach S3 without
# traversing the internet.
# ------------------------------------------------------------------------------

resource "aws_vpc_endpoint" "s3" {
  vpc_id       = aws_vpc.main.id
  service_name = "com.amazonaws.${var.aws_region}.s3"

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce-s3"
  })
}

resource "aws_vpc_endpoint_route_table_association" "s3_public" {
  route_table_id  = aws_route_table.public.id
  vpc_endpoint_id = aws_vpc_endpoint.s3.id
}

resource "aws_vpc_endpoint_route_table_association" "s3_private" {
  for_each = { for idx in range(var.az_count) : idx => aws_route_table.private[idx].id }

  route_table_id  = each.value
  vpc_endpoint_id = aws_vpc_endpoint.s3.id
}

# ------------------------------------------------------------------------------
# DynamoDB Gateway endpoint — free, routes DynamoDB traffic over the AWS backbone
# ------------------------------------------------------------------------------

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id       = aws_vpc.main.id
  service_name = "com.amazonaws.${var.aws_region}.dynamodb"

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce-dynamodb"
  })
}

resource "aws_vpc_endpoint_route_table_association" "dynamodb_public" {
  route_table_id  = aws_route_table.public.id
  vpc_endpoint_id = aws_vpc_endpoint.dynamodb.id
}

resource "aws_vpc_endpoint_route_table_association" "dynamodb_private" {
  for_each = { for idx in range(var.az_count) : idx => aws_route_table.private[idx].id }

  route_table_id  = each.value
  vpc_endpoint_id = aws_vpc_endpoint.dynamodb.id
}

# ------------------------------------------------------------------------------
# ECR interface endpoints — needed for ECS to pull container images
#
# Two endpoints are required:
#   - ecr.api      — ECR API operations (DescribeImages, GetAuthorizationToken, …)
#   - ecr.dkr      — Docker registry API (pull/push image layers)
# ------------------------------------------------------------------------------

resource "aws_vpc_endpoint" "ecr_api" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.aws_region}.ecr.api"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpce.id]

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce-ecr-api"
  })
}

resource "aws_vpc_endpoint" "ecr_dkr" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.aws_region}.ecr.dkr"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpce.id]

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce-ecr-dkr"
  })
}

# ------------------------------------------------------------------------------
# CloudWatch Logs interface endpoint — needed for ECS tasks to stream logs
# ------------------------------------------------------------------------------

resource "aws_vpc_endpoint" "logs" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.aws_region}.logs"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpce.id]

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-vpce-logs"
  })
}

# ==============================================================================
# Security Groups
#
# Two SGs with the ECS constraint required by the acceptance criteria:
# "The ECS task security group accepts traffic only from the ALB security group."
# ==============================================================================

# ------------------------------------------------------------------------------
# ALB security group — allows inbound HTTPS from the internet (public mode)
# or a restricted CIDR (private mode)
#
# In private mode the ALB itself is internal, so only traffic from the
# private network / ZTNA fabric reaches it.  The SG rule can be narrowed
# to the specific ingress CIDR.
# ------------------------------------------------------------------------------

resource "aws_security_group" "alb" {
  name        = "${var.name_prefix}-alb"
  description = "ALB - inbound HTTPS from clients"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "HTTPS from allowed ingress CIDRs"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = var.alb_ingress_cidrs
  }

  ingress {
    description = "HTTP from allowed ingress CIDRs (redirect to HTTPS)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = var.alb_ingress_cidrs
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-alb"
  })
}

# ------------------------------------------------------------------------------
# ECS task security group — accepts traffic ONLY from the ALB security group
#
# This satisfies the acceptance criterion: "The ECS task security group
# accepts traffic only from the ALB security group."
# ------------------------------------------------------------------------------

resource "aws_security_group" "ecs" {
  name        = "${var.name_prefix}-ecs"
  description = "ECS NGINX S3 gateway - inbound from ALB only"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "HTTP from ALB"
    from_port       = 80
    to_port         = 80
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    description = "Allow all outbound (S3, DynamoDB via VPC endpoints, ECR, CloudWatch Logs)"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-ecs"
  })
}
