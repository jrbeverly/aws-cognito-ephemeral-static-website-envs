# ==============================================================================
# DynamoDB module — Platform metadata store
#
# Provisions two tables:
#   1. Site metadata  — single-table design for users, sites, and host mappings
#   2. Upload records — upload lifecycle status and validation results
#
# See VISION.md §6 (hostname/namespace model) and §8.3 (metadata requirements).
# ==============================================================================

# ------------------------------------------------------------------------------
# Site metadata table — users, sites, and host mappings
#
# Single-table design covering three entity types:
#
#   Entity       PK                  SK                  Key attributes
#   -----------  ------------------  ------------------  --------------------------
#   User         USER#<userId>       PROFILE             userSlug, cognitoSub,
#                                                        createdAt, updatedAt,
#                                                        disabled
#   Site         USER#<userId>       SITE#<siteId>       siteSlug, activeVersionId,
#                                                        s3PublishedPrefix, createdAt,
#                                                        updatedAt, disabled
#   HostMapping  HOST#<hostname>     MAPPING             userId, userSlug, siteId,
#                                                        siteSlug, versionId,
#                                                        s3PublishedPrefix
#
# Access patterns:
#   - List a user's sites:    Query pk=USER#<userId>, sk begins_with SITE#
#   - Host → owner + site +
#     active version + prefix: GetItem pk=HOST#<hostname>, sk=MAPPING
#   - Get user profile:       GetItem pk=USER#<userId>, sk=PROFILE
#
# The host-mapping lookup is a single GetItem returning a denormalised item
# that carries the full resolution chain in one read (VISION.md §6).
# ------------------------------------------------------------------------------

resource "aws_dynamodb_table" "site_metadata" {
  name         = "${var.name_prefix}-metadata"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  point_in_time_recovery {
    enabled = false
  }

  server_side_encryption {
    enabled = true
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-metadata"
  })
}

# ------------------------------------------------------------------------------
# Upload records table — upload lifecycle and validation results
#
#   Entity   PK                SK                  upload_id (GSI-1 PK)
#   -------  ----------------  ------------------  --------------------
#   Upload   USER#<userId>     UPLOAD#<uploadId>   <uploadId>
#
# Additional attributes: siteId, status, validationResult, s3StagingKey,
#   compressedSizeBytes, uncompressedSizeBytes, fileCount, createdAt,
#   updatedAt
#
# Access patterns:
#   - List uploads for a user:         Query pk=USER#<userId>,
#                                      sk begins_with UPLOAD#
#   - Look up upload by ID (status):   Query GSI upload-by-id,
#                                      pk=<uploadId>
#   - Look up validation result:       stored inline on the upload item
# ------------------------------------------------------------------------------

resource "aws_dynamodb_table" "upload_records" {
  name         = "${var.name_prefix}-uploads"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  attribute {
    name = "upload_id"
    type = "S"
  }

  global_secondary_index {
    name            = "upload-by-id"
    hash_key        = "upload_id"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = false
  }

  server_side_encryption {
    enabled = true
  }

  tags = merge(var.common_tags, {
    Name = "${var.name_prefix}-uploads"
  })
}
