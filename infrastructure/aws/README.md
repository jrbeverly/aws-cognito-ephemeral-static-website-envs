# AWS Infrastructure

Infrastructure as code for the self-service static site hosting platform.

## Responsibility

Provision and manage all AWS resources required by the platform:

- VPC, subnets, route tables, NAT Gateways, and VPC endpoints
- Cognito user pool and federation configuration
- Application Load Balancer with Cognito authentication rules
- ECS service for the NGINX S3 gateway
- S3 bucket for staging and published content (Block Public Access enforced)
- DynamoDB metadata store
- Lambda functions for backend API and validation/publishing workers
- IAM roles and policies with scoped permissions
- DNS records and certificates for portal and hosted site hostname patterns
- Logging, metrics, and basic alarms

## Boundaries

- Must not include CloudFront resources, modules, or examples
- Must enforce S3 Block Public Access (no public bucket policies, no public ACLs)
- Must make it difficult to accidentally violate hard architectural requirements
- Users of the hosting platform must not need to write IaC to publish a site

## Technology

Built with Terraform. See `VISION.md` §15 for full infrastructure requirements.

## Module layout

```
infrastructure/aws/
├── main.tf                # Module composition
├── providers.tf           # Provider and version constraints, remote state
├── variables.tf           # Root variables (deployment_mode, domain, etc.)
├── locals.tf              # Naming, tags, mode-dependent derived values
├── outputs.tf             # Root outputs
├── data.tf                # Data sources
├── .terraform-docs.yml    # terraform-docs configuration
├── environments/
│   ├── lab.tfvars         # Public internet-facing prototype
│   └── private.tfvars.example  # Private ZTNA-compatible deployment
└── modules/
    ├── vpc/                # VPC, subnets, NAT GW, VPC endpoints, security groups
    ├── cognito/           # Authentication and user management
    ├── alb/               # Application Load Balancer
    ├── ecs-gateway/       # ECS NGINX S3 gateway
    ├── s3-sites/          # Private S3 storage (staging + published)
    ├── dynamodb/          # Platform metadata store
    ├── lambda/            # Serverless backend API and workers
    ├── iam/               # Scoped IAM roles and policies
    └── dns/               # Route53 records
```

## Usage

```sh
# Initialize (skip backend for initial validation)
terraform init -backend=false

# Plan lab environment
terraform plan -var-file=environments/lab.tfvars

# Apply lab environment (requires AWS credentials)
terraform apply -var-file=environments/lab.tfvars

# Switch to private deployment mode
terraform plan -var-file=environments/private.tfvars
```

## Deployment mode

The `deployment_mode` variable switches between public and private exposure
without changing the application modules:

| Variable | `public` (lab) | `private` |
|---|---|---|
| ALB scheme | `internet-facing` | `internal` |
| DNS zone | Public hosted zone | Private hosted zone |
| S3 access | Gateway endpoint | VPC endpoints |

See `VISION.md` §2.7 for private-compatibility requirements.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
