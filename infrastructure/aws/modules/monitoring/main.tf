# ==============================================================================
# main.tf — Observability resources: metric filters, alarms, and dashboard
#
# Metric Filters extract structured metrics from component log groups.
# Alarms fire on anomalous error/failure rates.  The dashboard provides a
# single-pane operator view.
#
# EMF metrics from the Go backend/worker (namespace Sites/Platform) are
# automatically captured by CloudWatch — no metric filters needed for those.
# The filters here cover the NGINX gateway and any log-based signals not
# emitted via EMF.
#
# VISION.md §14 — Observability requirements.
# VISION.md §15 — Logging and basic alarms.
# ==============================================================================

# ==============================================================================
# Metric Filters — extract structured metrics from component log groups
#
# Each filter defines a pattern that matches a log line and a metric value to
# extract (default 1 per match).  The metric is published under the platform
# namespace Sites/Platform.
# ==============================================================================

# ------------------------------------------------------------------------------
# Gateway metric filters — from JSON access logs
# ------------------------------------------------------------------------------

# Gateway 5xx errors — matches JSON lines where status is 500-599.
resource "aws_cloudwatch_log_metric_filter" "gateway_5xx" {
  name           = "${var.name_prefix}-gateway-5xx"
  pattern        = "{ $.status >= 500 && $.status < 600 }"
  log_group_name = var.gateway_log_group_name

  metric_transformation {
    name          = "Gateway5xxCount"
    namespace     = "Sites/Platform"
    value         = "1"
    default_value = "0"
    unit          = "Count"
  }
}

# Gateway 4xx errors — matches JSON lines where status is 400-499.
resource "aws_cloudwatch_log_metric_filter" "gateway_4xx" {
  name           = "${var.name_prefix}-gateway-4xx"
  pattern        = "{ $.status >= 400 && $.status < 500 }"
  log_group_name = var.gateway_log_group_name

  metric_transformation {
    name          = "Gateway4xxCount"
    namespace     = "Sites/Platform"
    value         = "1"
    default_value = "0"
    unit          = "Count"
  }
}

# Gateway request count — every access log line.
resource "aws_cloudwatch_log_metric_filter" "gateway_requests" {
  name           = "${var.name_prefix}-gateway-requests"
  pattern        = "{ $.method = \"*\" }"
  log_group_name = var.gateway_log_group_name

  metric_transformation {
    name          = "GatewayRequestCount"
    namespace     = "Sites/Platform"
    value         = "1"
    default_value = "0"
    unit          = "Count"
  }
}

# Gateway latency — extract request_time from JSON access logs.
resource "aws_cloudwatch_log_metric_filter" "gateway_latency" {
  name           = "${var.name_prefix}-gateway-latency"
  pattern        = "{ $.request_time = * }"
  log_group_name = var.gateway_log_group_name

  metric_transformation {
    name          = "GatewayLatencyMs"
    namespace     = "Sites/Platform"
    value         = "$.request_time"
    default_value = "0"
    unit          = "Milliseconds"
  }
}

# ------------------------------------------------------------------------------
# Backend API log-level metric filters — error log lines (not EMF-emitted)
#
# EMF automatically captures request count, latency, and error rates.
# These filters catch unexpected errors logged outside the EMF path.
# ------------------------------------------------------------------------------

# Backend ERROR-level log lines — catch panics, infrastructure failures.
resource "aws_cloudwatch_log_metric_filter" "backend_errors" {
  name           = "${var.name_prefix}-backend-errors"
  pattern        = "{ $.level = \"ERROR\" }"
  log_group_name = var.backend_log_group_name

  metric_transformation {
    name          = "BackendErrorLogLines"
    namespace     = "Sites/Platform"
    value         = "1"
    default_value = "0"
    unit          = "Count"
  }
}

# ==============================================================================
# CloudWatch Alarms — alert on anomalous error/failure rates
#
# Thresholds are lab-appropriate.  In production these would be tighter
# and the alarm actions would include an SNS topic for notifications.
# ==============================================================================

# ------------------------------------------------------------------------------
# Gateway error rate alarm (> 10 5xx errors in 5 minutes)
# ------------------------------------------------------------------------------
resource "aws_cloudwatch_metric_alarm" "gateway_high_5xx" {
  alarm_name          = "${var.name_prefix}-gateway-high-5xx"
  alarm_description   = "Gateway is returning elevated 5xx errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Gateway5xxCount"
  namespace           = "Sites/Platform"
  period              = 300
  statistic           = "Sum"
  threshold           = var.gateway_5xx_threshold
  treat_missing_data  = "notBreaching"

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# Backend error rate alarm (> 10 ERROR log lines in 5 minutes)
# ------------------------------------------------------------------------------
resource "aws_cloudwatch_metric_alarm" "backend_high_errors" {
  alarm_name          = "${var.name_prefix}-backend-high-errors"
  alarm_description   = "Backend API is logging elevated ERROR-level messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "BackendErrorLogLines"
  namespace           = "Sites/Platform"
  period              = 300
  statistic           = "Sum"
  threshold           = var.backend_error_threshold
  treat_missing_data  = "notBreaching"

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# Upload failure rate alarm (> 5 failed uploads in 5 minutes)
# ------------------------------------------------------------------------------
resource "aws_cloudwatch_metric_alarm" "upload_failures" {
  alarm_name          = "${var.name_prefix}-upload-failures"
  alarm_description   = "Elevated upload failure rate detected"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "UploadFailed"
  namespace           = "Sites/Platform"
  period              = 300
  statistic           = "Sum"
  threshold           = var.upload_failure_threshold
  treat_missing_data  = "notBreaching"

  tags = var.common_tags
}

# ------------------------------------------------------------------------------
# Validation failure rate alarm (> 5 validation failures in 5 minutes)
# ------------------------------------------------------------------------------
resource "aws_cloudwatch_metric_alarm" "validation_failures" {
  alarm_name          = "${var.name_prefix}-validation-failures"
  alarm_description   = "Elevated validation failure rate detected"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "ValidationFailed"
  namespace           = "Sites/Platform"
  period              = 300
  statistic           = "Sum"
  threshold           = var.validation_failure_threshold
  treat_missing_data  = "notBreaching"

  tags = var.common_tags
}

# ==============================================================================
# CloudWatch Dashboard — single-pane operator view of platform health
#
# Shows request volume, error rates, latency, and upload/publish metrics.
# The dashboard is a lab convenience — operators can add more widgets as the
# platform grows.
# ==============================================================================

resource "aws_cloudwatch_dashboard" "platform" {
  dashboard_name = "${var.name_prefix}-dashboard"
  dashboard_body = jsonencode({
    widgets = [
      # -----------------------------------------------------------------------
      # Row 1 — Gateway health (request count, errors, latency)
      # -----------------------------------------------------------------------
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 8
        height = 6
        properties = {
          title   = "Gateway — Request Count"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "GatewayRequestCount", { label = "Requests" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 8
        y      = 0
        width  = 8
        height = 6
        properties = {
          title   = "Gateway — 4xx / 5xx Errors"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "Gateway4xxCount", { label = "4xx" }],
            ["Sites/Platform", "Gateway5xxCount", { label = "5xx" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 16
        y      = 0
        width  = 8
        height = 6
        properties = {
          title   = "Gateway — Latency (p50 / p99)"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          period  = 300
          metrics = [
            ["Sites/Platform", "GatewayLatencyMs", { stat = "p50", label = "p50" }],
            ["Sites/Platform", "GatewayLatencyMs", { stat = "p99", label = "p99" }]
          ]
        }
      },

      # -----------------------------------------------------------------------
      # Row 2 — Backend API health
      # -----------------------------------------------------------------------
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 8
        height = 6
        properties = {
          title   = "Backend — Request Count"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "RequestCount", { label = "Requests" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 8
        y      = 6
        width  = 8
        height = 6
        properties = {
          title   = "Backend — Error / Fault Count"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "ErrorCount", { label = "Errors (4xx+)" }],
            ["Sites/Platform", "FaultCount", { label = "Faults (5xx)" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 16
        y      = 6
        width  = 8
        height = 6
        properties = {
          title   = "Backend — Latency (p50 / p99)"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          period  = 300
          metrics = [
            ["Sites/Platform", "LatencyMs", { stat = "p50", label = "p50" }],
            ["Sites/Platform", "LatencyMs", { stat = "p99", label = "p99" }]
          ]
        }
      },

      # -----------------------------------------------------------------------
      # Row 3 — Uploads and validation
      # -----------------------------------------------------------------------
      {
        type   = "metric"
        x      = 0
        y      = 12
        width  = 8
        height = 6
        properties = {
          title   = "Upload Events"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "UploadRequested", { label = "Requested" }],
            ["Sites/Platform", "UploadCompleted", { label = "Completed" }],
            ["Sites/Platform", "UploadFailed", { label = "Failed" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 8
        y      = 12
        width  = 8
        height = 6
        properties = {
          title   = "Publish Events"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "PublishSucceeded", { label = "Succeeded" }],
            ["Sites/Platform", "PublishFailed", { label = "Failed" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 16
        y      = 12
        width  = 8
        height = 6
        properties = {
          title   = "Validation Events"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = [
            ["Sites/Platform", "ValidationSucceeded", { label = "Succeeded" }],
            ["Sites/Platform", "ValidationFailed", { label = "Failed" }]
          ]
        }
      },

      # -----------------------------------------------------------------------
      # Row 4 — Alarm status (text widgets showing alarm state)
      # -----------------------------------------------------------------------
      {
        type   = "alarm"
        x      = 0
        y      = 18
        width  = 24
        height = 5
        properties = {
          title = "Alarms"
          alarms = [
            aws_cloudwatch_metric_alarm.gateway_high_5xx.arn,
            aws_cloudwatch_metric_alarm.backend_high_errors.arn,
            aws_cloudwatch_metric_alarm.upload_failures.arn,
            aws_cloudwatch_metric_alarm.validation_failures.arn,
          ]
        }
      }
    ]
  })
}
