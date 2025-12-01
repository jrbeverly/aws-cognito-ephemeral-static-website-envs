output "bucket_name" {
  description = "Name of the S3 bucket for site content"
  value       = aws_s3_bucket.sites.id
}

output "bucket_arn" {
  description = "ARN of the S3 bucket for site content"
  value       = aws_s3_bucket.sites.arn
}

output "staging_prefix" {
  description = "S3 prefix for staging uploads"
  value       = "staging/"
}

output "published_prefix" {
  description = "S3 prefix for published site versions"
  value       = "published/"
}

output "staging_events_queue_arn" {
  description = "ARN of the SQS queue that receives staging upload S3 event notifications"
  value       = aws_sqs_queue.staging_events.arn
}

output "staging_events_queue_url" {
  description = "URL of the SQS queue that receives staging upload S3 event notifications"
  value       = aws_sqs_queue.staging_events.url
}

output "staging_events_queue_name" {
  description = "Name of the SQS queue that receives staging upload S3 event notifications"
  value       = aws_sqs_queue.staging_events.name
}
