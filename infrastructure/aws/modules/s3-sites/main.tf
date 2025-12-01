# ==============================================================================
# S3 Sites module — Staging and published content storage
#
# Block Public Access is enforced. No public bucket policies, ACLs,
# or static website hosting endpoints are permitted.
#
# Prefix conventions:
#   staging/   — staging/users/{userId}/uploads/{uploadId}/...
#   published/ — published/users/{userSlug}/sites/{siteSlug}/versions/{versionId}/...
#
# See VISION.md §2.3, §8.4, §8.5.
# ==============================================================================

# ------------------------------------------------------------------------------
# S3 bucket — Private object storage for staging and published content
# ------------------------------------------------------------------------------

resource "aws_s3_bucket" "sites" {
  bucket        = "${var.name_prefix}-sites"
  force_destroy = true # teardown must remove published content

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-sites"
  })
}

# ------------------------------------------------------------------------------
# Block Public Access — Enforced at the bucket level
#
# All four block settings are enabled: no public ACLs, no public bucket
# policies, and no public bucket access of any kind.  (VISION.md §2.3)
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_public_access_block" "sites" {
  bucket = aws_s3_bucket.sites.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------------------
# Versioning — Enabled for immutable published version history
#
# Keeps a record of every object revision.  Published versioning is the
# mechanism that supports future rollback (VISION.md §19).
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_versioning" "sites" {
  bucket = aws_s3_bucket.sites.id
  versioning_configuration {
    status = "Enabled"
  }
}

# ------------------------------------------------------------------------------
# Server-side encryption — SSE-S3 (AES-256)
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_server_side_encryption_configuration" "sites" {
  bucket = aws_s3_bucket.sites.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# ------------------------------------------------------------------------------
# Lifecycle — Expire staging uploads after configurable period
#
# Staging objects are temporary work-in-progress uploads.  Expired staging
# uploads are automatically removed.  (VISION.md §12)
#
# Published version lifecycle (noncurrent version expiration) is deferred
# per VISION.md §19 — not required for the first prototype.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_lifecycle_configuration" "sites" {
  bucket = aws_s3_bucket.sites.id

  rule {
    id     = "expire-staging-uploads"
    status = "Enabled"

    filter {
      prefix = "staging/"
    }

    expiration {
      days = var.staging_expiration_days
    }
  }
}

# ==============================================================================
# S3 event notification — Trigger validation worker when content lands in staging
#
# When an object is created in the staging/ prefix, an S3 event notification
# delivers the event to an SQS queue.  The validation/publish worker (M5)
# polls this queue and processes uploads (extract, validate, publish).
#
# This is the trigger boundary for the upload → validate → publish pipeline.
# The queue decouples S3 from the worker Lambda, allowing the worker to
# control its own concurrency and retry behaviour independently of S3.
#
# Reference: VISION.md §8.5, §11, §15
# ==============================================================================

# ------------------------------------------------------------------------------
# SQS dead-letter queue — captures messages that exceed retry attempts
# ------------------------------------------------------------------------------

resource "aws_sqs_queue" "staging_dlq" {
  name = "${var.name_prefix}-staging-dlq"

  message_retention_seconds = 1209600 # 14 days

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-staging-dlq"
    Description = "Dead-letter queue for staging upload event processing failures"
  })
}

# ------------------------------------------------------------------------------
# SQS queue — Staging upload events for worker processing
# ------------------------------------------------------------------------------

resource "aws_sqs_queue" "staging_events" {
  name = "${var.name_prefix}-staging-events"

  # Visibility timeout: long enough for the worker to complete validation +
  # publish (extract, validate, copy to published).  If the worker crashes
  # mid-processing, the message becomes visible again for retry.
  visibility_timeout_seconds = 300 # 5 minutes

  # Message retention: keep unprocessed messages for the staging expiration
  # window so late arrivals are still consumable.
  message_retention_seconds = 345600 # 4 days

  # Redrive policy: after 3 receive attempts without successful processing,
  # move the message to the dead-letter queue.
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.staging_dlq.arn
    maxReceiveCount     = 3
  })

  tags = merge(var.common_tags, {
    Name        = "${var.name_prefix}-staging-events"
    Description = "Staging upload events consumed by the validation/publish worker"
  })
}

# ------------------------------------------------------------------------------
# SQS queue policy — Allow S3 to send messages to the staging events queue
# ------------------------------------------------------------------------------

resource "aws_sqs_queue_policy" "staging_events" {
  queue_url = aws_sqs_queue.staging_events.url

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "s3.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.staging_events.arn
        Condition = {
          ArnLike = {
            "aws:SourceArn" = aws_s3_bucket.sites.arn
          }
        }
      }
    ]
  })
}

# ------------------------------------------------------------------------------
# S3 bucket notification — staging/ object creation → SQS
#
# Only s3:ObjectCreated:* events are forwarded; deletions and other lifecycle
# events are not relevant to the upload-to-publish pipeline.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_notification" "staging_events" {
  bucket = aws_s3_bucket.sites.id

  queue {
    queue_arn     = aws_sqs_queue.staging_events.arn
    events        = ["s3:ObjectCreated:*"]
    filter_prefix = "staging/"
  }
}

# The portal PUTs uploads straight to presigned staging URLs from the browser,
# which is a cross-origin request; without this the browser blocks it.
resource "aws_s3_bucket_cors_configuration" "sites" {
  bucket = aws_s3_bucket.sites.id

  cors_rule {
    allowed_methods = ["PUT"]
    allowed_origins = ["https://${var.portal_fqdn}"]
    allowed_headers = ["*"]
    max_age_seconds = 300
  }
}
