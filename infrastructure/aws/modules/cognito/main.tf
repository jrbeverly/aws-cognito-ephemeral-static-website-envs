# ==============================================================================
# Cognito module — Authentication and user management
#
# Provisions:
#   1. User pool        — identity store for platform users
#   2. App client       — OIDC client for ALB authenticate-cognito actions
#   3. Hosted UI domain — Cognito-managed login, sign-up, and logout pages
#
# Token claim strategy (VISION.md §2.6, §8.2):
#   The Cognito-issued sub claim is the stable, immutable user identifier.
#   The backend maps sub → DynamoDB cognitoSub and derives the platform user
#   namespace from the corresponding user profile.  No custom attributes are
#   required for identity mapping.
#
# SAML federation compatibility (VISION.md §2.5):
#   The pool and client are designed so that adding a SAML identity provider
#   later is a configuration change, not an architectural one:
#     - append the provider name to supported_identity_providers on the client
#     - add an aws_cognito_identity_provider resource to this module
#   The user pool natively supports both built-in users and federated
#   identities — no pool-level changes are needed to enable SAML.
#
# Callback URL note:
#   The ALB authenticate-cognito action constructs redirect URIs as
#   https://<host>/oauth2/idpresponse.  The portal callback URL is registered
#   here.  Additional hosted-site callback URLs must be added programmatically
#   (via Cognito API) as sites are created, because Cognito does not support
#   wildcard domains in callback URLs.  A lifecycle ignore_changes rule on
#   callback_urls prevents Terraform from removing API-added URLs.
#
# See VISION.md §2.5, §2.6, §8.1, §8.2, §8.7, §13.1, §15.
# ==============================================================================

# ------------------------------------------------------------------------------
# User pool — Identity store for platform users
#
# Email is the primary username attribute.  The pool allows self-registration
# (lab convenience) and will accept federated identities from SAML providers
# added later.
#
# The sub claim is the permanent, unique user identifier emitted in ID and
# access tokens.  Backend services use sub to look up the DynamoDB User entity
# (cognitoSub attribute) and derive the platform user namespace.
# ------------------------------------------------------------------------------

resource "aws_cognito_user_pool" "main" {
  name = "${var.name_prefix}-user-pool"

  # --------------------------------------------------------------------------
  # Sign-in / username configuration
  # --------------------------------------------------------------------------
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # --------------------------------------------------------------------------
  # Standard attributes
  # --------------------------------------------------------------------------
  schema {
    name                = "email"
    attribute_data_type = "String"
    required            = true
    mutable             = true

    string_attribute_constraints {
      min_length = 1
      max_length = 256
    }
  }

  # --------------------------------------------------------------------------
  # Password policy — reasonable defaults for a lab environment
  # --------------------------------------------------------------------------
  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_uppercase                = true
    require_numbers                  = true
    require_symbols                  = true
    temporary_password_validity_days = 7
  }

  # --------------------------------------------------------------------------
  # Verification and recovery — email-based, no phone required
  # --------------------------------------------------------------------------
  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  # --------------------------------------------------------------------------
  # User creation — allow self-service sign-up for the lab prototype
  #
  # Self-registration is convenient for early testing.  In a production
  # deployment behind corporate SSO, this can be switched to admin-only
  # without affecting the pool or client architecture.
  # --------------------------------------------------------------------------
  admin_create_user_config {
    allow_admin_create_user_only = false
  }

  # --------------------------------------------------------------------------
  # Email delivery — use Cognito's built-in email service
  # --------------------------------------------------------------------------
  email_configuration {
    email_sending_account = "COGNITO_DEFAULT"
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-user-pool"
  })
}

# ------------------------------------------------------------------------------
# App client — OIDC client for ALB authenticate-cognito actions
#
# Configured for the authorization code grant (OAuth 2.0 code flow).
# No client secret is generated — the ALB integration does not use one.
#
# The ALB constructs redirect URIs dynamically from the request Host header
# as https://<host>/oauth2/idpresponse.  Only the portal callback URL is
# registered here; additional hosted-site URLs are added via API at runtime.
# ------------------------------------------------------------------------------

resource "aws_cognito_user_pool_client" "alb" {
  name                          = "${var.name_prefix}-alb"
  user_pool_id                  = aws_cognito_user_pool.main.id
  generate_secret               = true # required by ALB authenticate-cognito
  enable_token_revocation       = true
  prevent_user_existence_errors = "ENABLED"

  # --------------------------------------------------------------------------
  # OAuth 2.0 — authorization code grant for ALB integration
  # --------------------------------------------------------------------------
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes = [
    "openid",
    "email",
    "profile",
  ]

  # --------------------------------------------------------------------------
  # Callback and logout URLs
  #
  # Portal callback is registered statically.  Hosted-site callback URLs
  # (https://<slug>--<user>.sites.<domain>/oauth2/idpresponse) must be added
  # via the Cognito API when sites are created.  The lifecycle ignore_changes
  # rule on callback_urls prevents Terraform from removing them.
  # --------------------------------------------------------------------------
  callback_urls = [
    "https://${var.portal_fqdn}/oauth2/idpresponse",
  ]
  logout_urls = [
    "https://${var.portal_fqdn}/",
  ]

  # --------------------------------------------------------------------------
  # Identity providers — currently only the built-in Cognito user pool
  #
  # To add SAML federation:
  #   1. Add the provider name to this list (e.g. ["COGNITO", "CorporateSSO"])
  #   2. Add an aws_cognito_identity_provider resource
  #   3. Map SAML attributes → user pool attributes in the provider config
  # --------------------------------------------------------------------------
  supported_identity_providers = ["COGNITO"]

  # --------------------------------------------------------------------------
  # Token validity — 60 min for access/id, 30 days for refresh
  # --------------------------------------------------------------------------
  access_token_validity  = 60
  id_token_validity      = 60
  refresh_token_validity = 30

  token_validity_units {
    access_token  = "minutes"
    id_token      = "minutes"
    refresh_token = "days"
  }

  # --------------------------------------------------------------------------
  # Lifecycle — ignore callback_urls drift from API-added hosted-site URLs
  # --------------------------------------------------------------------------
  lifecycle {
    ignore_changes = [callback_urls]
  }
}

# ------------------------------------------------------------------------------
# Hosted UI domain — Cognito-managed login, sign-up, and logout pages
#
# Uses the AWS-provided domain (cognito_domain = name_prefix).  A custom
# domain is not required for the lab prototype; if one is needed later it can
# be added alongside (with its own ACM certificate and DNS record).
# ------------------------------------------------------------------------------

resource "aws_cognito_user_pool_domain" "main" {
  domain       = var.name_prefix
  user_pool_id = aws_cognito_user_pool.main.id
}
