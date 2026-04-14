#!/bin/bash
# Deploy the Rectella Shopify Service infrastructure to Azure.
#
# Prereqs:
#   - az login
#   - Subscription set: az account set --subscription "Rectella Azure Plan"
#   - Required env vars exported before running (see infra/README.md):
#       PG_ADMIN_PASSWORD, SHOPIFY_WEBHOOK_SECRET, SHOPIFY_ACCESS_TOKEN,
#       SYSPRO_PASSWORD, SYSPRO_COMPANY_ID, ADMIN_TOKEN
#
# Usage:
#   ./infra/deploy.sh                  # validate + deploy
#   ./infra/deploy.sh --what-if        # preview changes
#   ./infra/deploy.sh --validate       # syntax + template check only

set -euo pipefail

RESOURCE_GROUP="Shopify-RG"
LOCATION="uksouth"
DEPLOYMENT_NAME="rectella-$(date +%Y%m%d-%H%M%S)"
BICEP_FILE="$(dirname "$0")/main.bicep"
PARAM_FILE="$(dirname "$0")/main.bicepparam"

required_vars=(
  PG_ADMIN_PASSWORD
  SHOPIFY_WEBHOOK_SECRET
  SHOPIFY_ACCESS_TOKEN
  SYSPRO_PASSWORD
  SYSPRO_COMPANY_ID
  ADMIN_TOKEN
)

missing=()
for v in "${required_vars[@]}"; do
  if [[ -z "${!v:-}" ]]; then
    missing+=("$v")
  fi
done

if (( ${#missing[@]} > 0 )); then
  echo "ERROR: missing required env vars:" >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 1
fi

case "${1:-}" in
  --validate)
    echo "Validating Bicep template..."
    az deployment group validate \
      --resource-group "$RESOURCE_GROUP" \
      --template-file "$BICEP_FILE" \
      --parameters "$PARAM_FILE"
    ;;
  --what-if)
    echo "Running what-if analysis..."
    az deployment group what-if \
      --resource-group "$RESOURCE_GROUP" \
      --template-file "$BICEP_FILE" \
      --parameters "$PARAM_FILE"
    ;;
  *)
    echo "Deploying to $RESOURCE_GROUP ($LOCATION)..."
    az deployment group create \
      --resource-group "$RESOURCE_GROUP" \
      --name "$DEPLOYMENT_NAME" \
      --template-file "$BICEP_FILE" \
      --parameters "$PARAM_FILE"
    echo
    echo "Deployment complete. Outputs:"
    az deployment group show \
      --resource-group "$RESOURCE_GROUP" \
      --name "$DEPLOYMENT_NAME" \
      --query properties.outputs
    ;;
esac
