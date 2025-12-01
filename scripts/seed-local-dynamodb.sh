#!/bin/bash
# ==============================================================================
# seed-local-dynamodb.sh — Create and seed DynamoDB tables in LocalStack
#
# Prerequisites:
#   - Docker with LocalStack running (see docs/local-development.md)
#   - AWS CLI installed (available in the devcontainer)
#
# Usage:
#   ./scripts/seed-local-dynamodb.sh [endpoint_url]
#
# Default endpoint: http://localhost:4566
#
# This script creates the site_metadata and upload_records DynamoDB tables
# and inserts seed fixtures (test user, site, upload, and host mapping) so
# the Go backend can serve requests without any AWS deployment.
# ==============================================================================

set -euo pipefail

ENDPOINT="${1:-http://localhost:4566}"
REGION="${AWS_REGION:-us-east-1}"
AWS_CMD="aws --endpoint-url $ENDPOINT --region $REGION"

SITE_METADATA_TABLE="site_metadata"
UPLOAD_RECORDS_TABLE="upload_records"

# ------------------------------------------------------------------------------
# Helper — idempotent table creation
# ------------------------------------------------------------------------------

create_table_if_not_exists() {
  local table_name="$1"
  local key_schema="$2"
  local attribute_defs="$3"
  local extra_args="${4:-}"

  if $AWS_CMD dynamodb describe-table --table-name "$table_name" --no-cli-pager &>/dev/null; then
    echo "[seed] Table '$table_name' already exists — skipping creation"
  else
    echo "[seed] Creating table '$table_name'..."
    # shellcheck disable=SC2086
    $AWS_CMD dynamodb create-table \
      --table-name "$table_name" \
      --key-schema "$key_schema" \
      --attribute-definitions "$attribute_defs" \
      --billing-mode PAY_PER_REQUEST \
      $extra_args \
      --no-cli-pager > /dev/null
    echo "[seed] Table '$table_name' created"
  fi
}

# ------------------------------------------------------------------------------
# Create tables
# ------------------------------------------------------------------------------

echo "[seed] Targeting LocalStack at $ENDPOINT"

create_table_if_not_exists "$SITE_METADATA_TABLE" \
  '[{"AttributeName":"pk","KeyType":"HASH"},{"AttributeName":"sk","KeyType":"RANGE"}]' \
  '[{"AttributeName":"pk","AttributeType":"S"},{"AttributeName":"sk","AttributeType":"S"}]'

create_table_if_not_exists "$UPLOAD_RECORDS_TABLE" \
  '[{"AttributeName":"pk","KeyType":"HASH"},{"AttributeName":"sk","KeyType":"RANGE"}]' \
  '[{"AttributeName":"pk","AttributeType":"S"},{"AttributeName":"sk","AttributeType":"S"},{"AttributeName":"upload_id","AttributeType":"S"}]' \
  '--global-secondary-indexes IndexName=upload-by-id,KeySchema=[{AttributeName=upload_id,KeyType=HASH}],Projection={ProjectionType=ALL}'

# ------------------------------------------------------------------------------
# Seed fixtures
# ------------------------------------------------------------------------------

# Seed test user (sub-alice)
echo "[seed] Inserting seed fixtures..."

NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# User: USER#sub-alice / PROFILE
$AWS_CMD dynamodb put-item \
  --table-name "$SITE_METADATA_TABLE" \
  --item "{
    \"pk\": {\"S\": \"USER#sub-alice\"},
    \"sk\": {\"S\": \"PROFILE\"},
    \"userSlug\": {\"S\": \"alice\"},
    \"cognitoSub\": {\"S\": \"sub-alice\"},
    \"createdAt\": {\"S\": \"$NOW\"},
    \"updatedAt\": {\"S\": \"$NOW\"},
    \"disabled\": {\"BOOL\": false}
  }" \
  --no-cli-pager > /dev/null
echo "[seed]   User: USER#sub-alice / PROFILE (alice)"

# Site: USER#sub-alice / SITE#site-001
$AWS_CMD dynamodb put-item \
  --table-name "$SITE_METADATA_TABLE" \
  --item "{
    \"pk\": {\"S\": \"USER#sub-alice\"},
    \"sk\": {\"S\": \"SITE#site-001\"},
    \"siteSlug\": {\"S\": \"my-demo-site\"},
    \"activeVersionId\": {\"S\": \"v1-001\"},
    \"s3PublishedPrefix\": {\"S\": \"published/users/sub-alice/sites/site-001/v1-001/\"},
    \"createdAt\": {\"S\": \"$NOW\"},
    \"updatedAt\": {\"S\": \"$NOW\"},
    \"disabled\": {\"BOOL\": false}
  }" \
  --no-cli-pager > /dev/null
echo "[seed]   Site: USER#sub-alice / SITE#site-001 (my-demo-site)"

# HostMapping: HOST#my-demo-site.alice.sites.example.com / MAPPING
$AWS_CMD dynamodb put-item \
  --table-name "$SITE_METADATA_TABLE" \
  --item "{
    \"pk\": {\"S\": \"HOST#my-demo-site.alice.sites.example.com\"},
    \"sk\": {\"S\": \"MAPPING\"},
    \"userId\": {\"S\": \"sub-alice\"},
    \"userSlug\": {\"S\": \"alice\"},
    \"siteId\": {\"S\": \"site-001\"},
    \"siteSlug\": {\"S\": \"my-demo-site\"},
    \"versionId\": {\"S\": \"v1-001\"},
    \"s3PublishedPrefix\": {\"S\": \"published/users/sub-alice/sites/site-001/v1-001/\"}
  }" \
  --no-cli-pager > /dev/null
echo "[seed]   HostMapping: HOST#my-demo-site.alice.sites.example.com / MAPPING"

# Upload: USER#sub-alice / UPLOAD#upload-001
$AWS_CMD dynamodb put-item \
  --table-name "$UPLOAD_RECORDS_TABLE" \
  --item "{
    \"pk\": {\"S\": \"USER#sub-alice\"},
    \"sk\": {\"S\": \"UPLOAD#upload-001\"},
    \"upload_id\": {\"S\": \"upload-001\"},
    \"userId\": {\"S\": \"sub-alice\"},
    \"siteId\": {\"S\": \"site-001\"},
    \"status\": {\"S\": \"published\"},
    \"s3StagingKey\": {\"S\": \"staging/users/sub-alice/uploads/upload-001/source.zip\"},
    \"compressedSizeBytes\": {\"N\": \"1024\"},
    \"uncompressedSizeBytes\": {\"N\": \"4096\"},
    \"fileCount\": {\"N\": \"5\"},
    \"createdAt\": {\"S\": \"$NOW\"},
    \"updatedAt\": {\"S\": \"$NOW\"}
  }" \
  --no-cli-pager > /dev/null
echo "[seed]   Upload: USER#sub-alice / UPLOAD#upload-001 (published)"

echo "[seed] Done — LocalStack DynamoDB is seeded"
