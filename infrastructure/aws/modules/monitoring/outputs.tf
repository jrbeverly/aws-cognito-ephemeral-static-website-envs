# ==============================================================================
# outputs.tf — Monitoring module
# ==============================================================================

output "dashboard_name" {
  description = "Name of the CloudWatch dashboard"
  value       = aws_cloudwatch_dashboard.platform.dashboard_name
}

output "alarm_arns" {
  description = "Map of alarm names to ARNs"
  value = {
    gateway_high_5xx    = aws_cloudwatch_metric_alarm.gateway_high_5xx.arn
    backend_high_errors = aws_cloudwatch_metric_alarm.backend_high_errors.arn
    upload_failures     = aws_cloudwatch_metric_alarm.upload_failures.arn
    validation_failures = aws_cloudwatch_metric_alarm.validation_failures.arn
  }
}
