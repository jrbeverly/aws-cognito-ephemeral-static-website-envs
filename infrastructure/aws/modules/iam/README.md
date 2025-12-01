# IAM

Scoped IAM roles and policies for each platform component. No component holds
broad administrative permissions (`VISION.md` §13.4).

## Roles

| Role | Principal | S3 scope | DynamoDB scope |
|---|---|---|---|
| Backend API | `lambda.amazonaws.com` | `PutObject`, `GetObject` on `staging/*` | Full CRUD on both tables |
| Worker | `lambda.amazonaws.com` | `GetObject` staging, `PutObject` published, `DeleteObject` staging | Read + update on both tables |
| ECS Gateway | `ecs-tasks.amazonaws.com` | `GetObject` published, `ListBucket` published | `GetItem`, `Query` on site metadata only |

## Frontend

The portal frontend receives **no AWS credentials at all**. Uploads use
backend-issued presigned URLs scoped to staging prefixes. This constraint
is architectural, not optional (`VISION.md` §8.1).

## Permission boundaries

- Only the **worker** role can write to `published/` (enforced by policy, not convention).
- No role grants `s3:*`, `dynamodb:*`, `iam:*`, or any administrative privileges.
- S3 permissions are constrained by prefix; DynamoDB by table.
- No role can read or write public S3 objects (Block Public Access is enforced by
  the `s3-sites` module independently of IAM).

See `VISION.md` §13.4 and §13.5 for security requirements.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
