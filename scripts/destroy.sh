#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TF_DIR="${ROOT_DIR}/infrastructure/aws"
AWS_REGION="${AWS_REGION:-ca-central-1}"
AUTO_APPROVE="${AUTO_APPROVE:-0}"
export AWS_REGION AWS_DEFAULT_REGION="${AWS_REGION}"

zones="$(aws route53 list-hosted-zones --query 'HostedZones[?Config.PrivateZone==`false`].Name' --output text)"
domain="${zones%.}"

approve=()
[[ "${AUTO_APPROVE}" == "1" ]] && approve=(-auto-approve)
terraform -chdir="${TF_DIR}" destroy -input=false "${approve[@]}" \
  -var "aws_region=${AWS_REGION}" \
  -var "domain_name=${domain}"
