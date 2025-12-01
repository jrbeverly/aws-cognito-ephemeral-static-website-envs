output "site_metadata_table_name" {
  description = "Name of the site metadata DynamoDB table (users, sites, host mappings)"
  value       = aws_dynamodb_table.site_metadata.name
}

output "site_metadata_table_arn" {
  description = "ARN of the site metadata DynamoDB table"
  value       = aws_dynamodb_table.site_metadata.arn
}

output "upload_records_table_name" {
  description = "Name of the upload records DynamoDB table (status and validation results)"
  value       = aws_dynamodb_table.upload_records.name
}

output "upload_records_table_arn" {
  description = "ARN of the upload records DynamoDB table"
  value       = aws_dynamodb_table.upload_records.arn
}
