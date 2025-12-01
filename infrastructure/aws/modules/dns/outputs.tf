output "zone_id" {
  description = "ID of the Route53 hosted zone"
  value       = data.aws_route53_zone.primary.zone_id
}

output "zone_name_servers" {
  description = "Name servers for the Route53 hosted zone (for NS delegation)"
  value       = data.aws_route53_zone.primary.name_servers
}

output "portal_fqdn" {
  description = "FQDN of the management portal record"
  value       = var.portal_fqdn
}

output "sites_wildcard_fqdn" {
  description = "Wildcard FQDN for hosted user sites"
  value       = "*.${var.sites_domain}"
}

output "certificate_arn" {
  description = "ARN of the ACM certificate covering portal and hosted-site hostnames"
  value       = aws_acm_certificate.main.arn
}
