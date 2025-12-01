variable "environment" {
  description = "Deployment environment name"
  type        = string
}

variable "deployment_mode" {
  description = "Deployment mode: 'public' or 'private'"
  type        = string
}

variable "name_prefix" {
  description = "Prefix applied to resource names"
  type        = string
}

variable "common_tags" {
  description = "Common tags applied to all resources"
  type        = map(string)
  default     = {}
}

variable "domain_name" {
  description = "Root domain name for the Route53 zone (e.g., example.com)"
  type        = string
}

variable "portal_fqdn" {
  description = "Fully qualified domain name for the management portal"
  type        = string
}

variable "sites_domain" {
  description = "Base domain for hosted user sites (wildcard)"
  type        = string
}

variable "dns_zone_visibility" {
  description = "DNS zone visibility: 'public' or 'private'"
  type        = string

  validation {
    condition     = contains(["public", "private"], var.dns_zone_visibility)
    error_message = "dns_zone_visibility must be 'public' or 'private'."
  }
}

variable "vpc_id" {
  description = "VPC ID for private hosted zone association (required when dns_zone_visibility is 'private')"
  type        = string
  default     = null
}

variable "alb_dns_name" {
  description = "DNS name of the ALB for Route53 alias records"
  type        = string
  default     = null
}

variable "alb_zone_id" {
  description = "Canonical hosted zone ID of the ALB for Route53 alias records"
  type        = string
  default     = null
}
