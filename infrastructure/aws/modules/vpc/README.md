# VPC

Network fabric for the platform: VPC, subnets, route tables, NAT Gateways, VPC
endpoints for private AWS connectivity, and security groups for the ALB and ECS
tasks.

The VPC is the shared network layer consumed by the ALB, ECS, and Lambda
modules.  In private deployment mode the ALB becomes internal and DNS moves to
a private hosted zone, but the VPC topology is identical — the ALB module
controls the scheme via `alb_scheme`.

See `VISION.md` §2.7, §8.6, §8.7, §13.5.

## VPC Endpoints

| Endpoint | Type | Purpose |
|---|---|---|
| S3 | Gateway | ECS gateway and Lambda workers read/write S3 without public internet |
| DynamoDB | Gateway | Metadata lookups without public internet |
| ECR API | Interface | ECS pulls container images from ECR |
| ECR DKR | Interface | Docker registry API for image layer pulls |
| CloudWatch Logs | Interface | ECS task log streaming |

Gateway endpoints are free and required for private S3/DynamoDB access per
`VISION.md` §2.3 and §2.7.  Interface endpoints allow ECS to pull images
and emit logs without NAT Gateway egress in locked-down private deployments.

## Security Groups

- **ALB SG** (`alb`) — allows HTTPS (443) and HTTP (80) from configured
  ingress CIDRs.  In public mode this defaults to `0.0.0.0/0`; in private
  mode it should be scoped to the ZTNA or private network CIDR.
- **ECS SG** (`ecs`) — accepts HTTP (80) only from the ALB security group.
  This satisfies the requirement that the ECS task security group accepts
  traffic only from the ALB.

## Topology

```
VPC {vpc_cidr}
├── Internet Gateway
│   └── Public route table → 0.0.0.0/0 via IGW
│       ├── Public subnet AZ A (ALB)
│       ├── Public subnet AZ B (ALB)
│       └── NAT Gateway(s) (in public subnets)
├── Private route table AZ A → 0.0.0.0/0 via NAT GW A
│   └── Private subnet AZ A (ECS tasks)
├── Private route table AZ B → 0.0.0.0/0 via NAT GW B
│   └── Private subnet AZ B (ECS tasks)
└── VPC Endpoints
    ├── S3 (gateway) — all route tables
    ├── DynamoDB (gateway) — all route tables
    ├── ECR API (interface) — private subnets
    ├── ECR DKR (interface) — private subnets
    └── CloudWatch Logs (interface) — private subnets
```

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
