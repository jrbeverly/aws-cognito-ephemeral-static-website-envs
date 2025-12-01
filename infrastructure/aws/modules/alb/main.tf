# ==============================================================================
# ALB module — Application Load Balancer with Cognito authentication
#
# Provisions:
#   1. Application Load Balancer (internet-facing or internal)
#   2. Gateway target group (IP type, for ECS Fargate NGINX S3 gateway)
#   3. Backend API target group (Lambda type; the Lambda also serves the portal)
#   5. HTTP listener  (port 80  → redirect to HTTPS)
#   6. HTTPS listener (port 443 → Cognito auth → forward to gateway)
#   7. Unauthenticated rule for the health-check path
#   8. Portal hostname rule  → authenticate-cognito → API target group
#   9. API path rule (portal hostname only) → authenticate-cognito → API target group
#
# Authentication model (VISION.md §8.7, §13.1):
#   - The HTTPS listener default action enforces Cognito authentication.
#     Unauthenticated requests are redirected to the Cognito hosted UI.
#   - The /health path is explicitly allowed without authentication.
#   - The /oauth2/idpresponse callback path is handled automatically by
#     the ALB authenticate-cognito action — no explicit rule is needed.
#
# Origin isolation (VISION.md §6.2):
#   - The portal hostname resolves to the API (Lambda) target group.
#   - Hosted-site wildcard hostnames resolve to the gateway target group
#     (via the default forward action).
#   - API paths are only routable on the portal hostname (compound
#     host + path condition), so hosted user sites cannot reach them.
#
# Target group note:
#   All target groups use IP target type for ECS Fargate (awsvpc network
#   mode).  Targets are registered by the ECS service when the respective
#   modules (portal, gateway, backend) are implemented.
#
# See VISION.md §2.7, §6.2, §8.7, §13.1.
# ==============================================================================

# ------------------------------------------------------------------------------
# Application Load Balancer
# ------------------------------------------------------------------------------

resource "aws_lb" "main" {
  name               = "${var.name_prefix}-alb"
  internal           = var.alb_scheme == "internal"
  load_balancer_type = "application"
  security_groups    = [var.alb_security_group_id]
  subnets            = var.subnet_ids

  # --------------------------------------------------------------------------
  # Access logs disabled for the lab prototype.
  # Enable for production to capture request logs to an S3 bucket.
  # --------------------------------------------------------------------------

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-alb"
  })
}

# ------------------------------------------------------------------------------
# Gateway target group — ECS Fargate NGINX S3 gateway (IP targets)
#
# The ECS service registers Fargate task private IPs here when the ECS Gateway
# module is implemented.  Until then, the target group has no healthy targets
# and the ALB returns 503 for forwarded requests.  Authentication still works:
# unauthenticated users are redirected to Cognito before the forward action
# runs, so the login flow is testable even without backend targets.
# ------------------------------------------------------------------------------

resource "aws_lb_target_group" "gateway" {
  name        = "${var.name_prefix}-gateway"
  port        = 80
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = var.vpc_id

  health_check {
    path                = "/health"
    interval            = 15
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
    matcher             = "200"
  }

  # --------------------------------------------------------------------------
  # Stickiness is not required for the gateway — all content is stateless
  # static files served from S3.
  # --------------------------------------------------------------------------

  tags = var.common_tags

  lifecycle {
    create_before_destroy = true
  }
}

# ------------------------------------------------------------------------------
# Backend API target group — the Go Lambda (registered in the root main.tf)
#
# Serves both /api/* and the built portal on the portal hostname.  Only
# routable on the portal hostname, so hosted sites cannot reach it.
# ------------------------------------------------------------------------------

resource "aws_lb_target_group" "api" {
  name        = "${var.name_prefix}-api"
  target_type = "lambda"

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# HTTP listener — redirect all plain-text traffic to HTTPS
# ------------------------------------------------------------------------------

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = "80"
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

# ------------------------------------------------------------------------------
# HTTPS listener — Cognito authentication, host-based routing
#
# Default action chain (for hosted-site wildcard hostnames):
#   1. authenticate-cognito  → validates session / redirects to login
#   2. forward               → sends request to the gateway target group
#
# Listener rules (evaluated in priority order before the default action):
#   Priority 1 — /health on any hostname → fixed 200 (no auth)
#   Priority 2 — portal hostname + /api/*   → auth → API target group
#   Priority 3 — portal hostname            → auth → API target group
#   Default    — all other hostnames        → auth → gateway target group
#
# The authenticate-cognito action:
#   - Checks for the AWSELBAuthSessionCookie
#   - On miss:  redirects to the Cognito hosted UI (OnUnauthenticatedRequest
#     defaults to "authenticate")
#   - On hit:   validates the token and passes claims in headers
#   - The /oauth2/idpresponse callback is handled automatically by the ALB
#     as part of the authentication flow — no explicit rule required.
#
# SSL policy:
#   ELBSecurityPolicy-2016-08 is the default AWS-managed policy.  It balances
#   broad client compatibility with strong ciphers and is appropriate for the
#   lab prototype.  A stricter policy may be substituted for production.
# ------------------------------------------------------------------------------

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = "443"
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-2016-08"
  certificate_arn   = var.certificate_arn

  # --------------------------------------------------------------------------
  # Default action — authenticate, then forward
  # --------------------------------------------------------------------------

  default_action {
    type = "authenticate-cognito"

    authenticate_cognito {
      user_pool_arn       = var.cognito_user_pool_arn
      user_pool_client_id = var.cognito_user_pool_client_id
      user_pool_domain    = var.cognito_user_pool_domain
    }
  }

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.gateway.arn
  }
}

# ------------------------------------------------------------------------------
# Health check path — no authentication required
#
# Priority 1 (evaluated before the default action).  Uses a fixed response so
# that the health check succeeds even when the gateway has no healthy targets.
# This is the external / ALB-level health endpoint; the gateway's own /health
# endpoint is probed by the target group health check configured above.
# ------------------------------------------------------------------------------

resource "aws_lb_listener_rule" "health" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 1

  action {
    type = "fixed-response"

    fixed_response {
      content_type = "text/plain"
      message_body = "OK"
      status_code  = "200"
    }
  }

  condition {
    path_pattern {
      values = ["/health"]
    }
  }
}

# ------------------------------------------------------------------------------
# Portal backend API path — authenticate then forward to API target group
#
# Priority 2 (evaluated before the portal hostname catch-all at priority 3).
# Uses a compound host + path condition so /api/* is only routable on the
# portal hostname.  Hosted-site hostnames (wildcard → default action) cannot
# match this rule, enforcing origin isolation between user-generated content
# and the portal APIs (VISION.md §6.2).
# ------------------------------------------------------------------------------

resource "aws_lb_listener_rule" "api" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 2

  action {
    type = "authenticate-cognito"

    authenticate_cognito {
      user_pool_arn       = var.cognito_user_pool_arn
      user_pool_client_id = var.cognito_user_pool_client_id
      user_pool_domain    = var.cognito_user_pool_domain
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  condition {
    host_header {
      values = [var.portal_fqdn]
    }
  }

  condition {
    path_pattern {
      values = ["/api/*"]
    }
  }
}

# ------------------------------------------------------------------------------
# Portal hostname — authenticate then forward to the API (Lambda) target group
#
# Priority 3 (evaluated after /health and /api/* rules).  Catches all
# requests to the portal hostname that don't match a higher-priority rule
# and routes them to the Vue.js management portal.
# ------------------------------------------------------------------------------

resource "aws_lb_listener_rule" "portal" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 3

  action {
    type = "authenticate-cognito"

    authenticate_cognito {
      user_pool_arn       = var.cognito_user_pool_arn
      user_pool_client_id = var.cognito_user_pool_client_id
      user_pool_domain    = var.cognito_user_pool_domain
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  condition {
    host_header {
      values = [var.portal_fqdn]
    }
  }
}
