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

variable "staging_expiration_days" {
  description = "Number of days after which staged uploads are automatically expired"
  type        = number
  default     = 30
}

variable "portal_fqdn" {
  description = "Portal hostname allowed to PUT to presigned staging URLs from the browser"
  type        = string
}
