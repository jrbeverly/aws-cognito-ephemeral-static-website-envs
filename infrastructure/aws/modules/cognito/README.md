# Cognito

Authentication and user management for the platform.

Provisions the Cognito user pool, OIDC app client for ALB integration, and
hosted UI domain.  The pool and client are designed so that adding a SAML
identity provider later is a configuration change, not an architectural one
(`VISION.md` §2.5).

## Token and claim strategy

The Cognito-issued `sub` claim is the stable, immutable user identifier.
The backend maps `sub` → `cognitoSub` in DynamoDB (`User` entity) and derives
the platform user namespace from the user profile.  No custom attributes are
required for identity mapping (`VISION.md` §2.6, §8.2).

## Resources

- `aws_cognito_user_pool` — Identity store with email-as-username, self-service
  sign-up for the lab, and native support for federated identities
- `aws_cognito_user_pool_client` — OIDC client for the ALB `authenticate-cognito`
  action (authorization code grant, no client secret)
- `aws_cognito_user_pool_domain` — AWS-hosted login/sign-up/logout UI

## Callback URL management

The ALB `authenticate-cognito` action constructs redirect URIs dynamically
from the request `Host` header:

```text
https://<host>/oauth2/idpresponse
```

The portal callback URL (`https://<portal_fqdn>/oauth2/idpresponse`) is
registered statically in the Terraform module.

Hosted-site callback URLs must be added programmatically via the Cognito API
when sites are created, because Cognito does not support wildcard domains in
callback URLs.  A `lifecycle.ignore_changes` rule on `callback_urls` prevents
Terraform from removing API-added URLs on subsequent applies.

## Adding a SAML identity provider

Adding SAML federation is a configuration change — no architectural redesign
is required:

1. **Add the provider** — create an `aws_cognito_identity_provider` resource
   with the corporate IdP's SAML metadata document and attribute mappings:

   ```hcl
   resource "aws_cognito_identity_provider" "saml" {
     user_pool_id  = aws_cognito_user_pool.main.id
     provider_name = "CorporateSSO"
     provider_type = "SAML"

     provider_details = {
       MetadataFile            = file("path/to/idp-metadata.xml")
       SSORedirectBindingURI   = "https://idp.example.com/sso"
     }

     attribute_mapping = {
       email = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
     }
   }
   ```

2. **Update the app client** — add the provider name to
   `supported_identity_providers` on `aws_cognito_user_pool_client.alb`:

   ```hcl
   supported_identity_providers = ["COGNITO", "CorporateSSO"]
   ```

No changes to the user pool, domain, or client architecture are needed.
The pool natively supports both built-in users and federated identities.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
