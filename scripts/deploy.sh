#!/usr/bin/env bash
# Build the portal + backend Lambda, apply Terraform against the account's
# only public Route 53 zone, then push the gateway image and roll ECS.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TF_DIR="${ROOT_DIR}/infrastructure/aws"
AWS_REGION="${AWS_REGION:-ca-central-1}"
AUTO_APPROVE="${AUTO_APPROVE:-0}"
export AWS_REGION AWS_DEFAULT_REGION="${AWS_REGION}"

log() { printf '[deploy] %s\n' "$*"; }
o() { terraform -chdir="${TF_DIR}" output -raw "$1"; }

zones="$(aws route53 list-hosted-zones --query 'HostedZones[?Config.PrivateZone==`false`].Name' --output text)"
if [[ "$(wc -w <<<"${zones}")" -ne 1 ]]; then
  echo "[deploy] expected exactly one public hosted zone, found: ${zones:-none}" >&2
  exit 1
fi
domain="${zones%.}"
log "Region ${AWS_REGION}, hosted zone ${domain}"

log "Building portal"
(cd "${ROOT_DIR}/frontend/portal-vue" && npm install --no-audit --no-fund && npm run build)

log "Building backend Lambda package"
"${ROOT_DIR}/scripts/build-backend.sh"

approve=()
[[ "${AUTO_APPROVE}" == "1" ]] && approve=(-auto-approve)
terraform -chdir="${TF_DIR}" init -input=false
terraform -chdir="${TF_DIR}" apply -input=false "${approve[@]}" \
  -var "aws_region=${AWS_REGION}" \
  -var "domain_name=${domain}"

repo="$(o ecr_repository_url)"
log "Pushing gateway image to ${repo}"
aws ecr get-login-password | docker login --username AWS --password-stdin "${repo%%/*}"
docker build --platform linux/amd64 -t "${repo}:latest" "${ROOT_DIR}/gateway/nginx-s3-gateway"
docker push "${repo}:latest"
aws ecs update-service --cluster "$(o ecs_cluster_name)" --service "$(o ecs_service_name)" \
  --force-new-deployment >/dev/null

pool="$(o cognito_user_pool_id)"
user="alice@example.com"
password="Aa1!$(openssl rand -hex 8)"
if ! aws cognito-idp admin-get-user --user-pool-id "${pool}" --username "${user}" >/dev/null 2>&1; then
  aws cognito-idp admin-create-user --user-pool-id "${pool}" --username "${user}" \
    --user-attributes Name=email,Value="${user}" Name=email_verified,Value=true \
    --message-action SUPPRESS >/dev/null
fi
aws cognito-idp admin-set-user-password --user-pool-id "${pool}" --username "${user}" \
  --password "${password}" --permanent

log "Portal: $(o portal_url)"
log "Test user: ${user} / ${password}"
log "Sites:  https://<site>--<user>.$(o sites_base_domain)/"
