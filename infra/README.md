# Azure Infrastructure

Bicep templates for deploying the Rectella Shopify Service to Azure.

## What this deploys

Into the existing `Shopify-RG` resource group:

- **db-subnet** (delegated to PostgreSQL Flexible Server)
- **apps-subnet** (delegated to Container Apps)
- **Private DNS zone** for PostgreSQL + VNet link
- **PostgreSQL Flexible Server** (Burstable B1ms, PG 16, private VNet access)
- **Log Analytics workspace** for Container Apps diagnostics
- **Container Apps Environment** (VNet-integrated, Consumption plan)
- **Container App** pulling from `ghcr.io/trismegistus0/rectella-shopify-service:latest`

**Pre-existing, not managed by this template:**
- `Rectella-Network` VNet
- `RectellaVPN` (Virtual network gateway)
- `Office-Meraki` (Local network gateway)
- `Azure-to-Office` (site-to-site connection)

## Prerequisites

1. **Azure CLI** — install with `sudo pacman -S azure-cli` (Arch) or per [Microsoft docs](https://learn.microsoft.com/cli/azure/install-azure-cli)
2. **Log in** — `az login`
3. **Set subscription** — `az account set --subscription "Rectella Azure Plan"`
4. **Env vars** — export these before running `deploy.sh`:

```bash
export PG_ADMIN_PASSWORD='strong-password-here'
export SHOPIFY_WEBHOOK_SECRET='from-shopify-custom-app'
export SHOPIFY_ACCESS_TOKEN='shpat_...'
export SYSPRO_PASSWORD='from-sarah'
export SYSPRO_COMPANY_ID='from-sarah'
export ADMIN_TOKEN='strong-random-token'
```

Plus fill in non-secret params in `main.bicepparam`:
- `sysproWarehouse` (from Melanie)
- `sysproSkus` (comma-separated, from Melanie)
- `shopifyLocationId` (optional, auto-discovered if blank)

## Usage

```bash
# Preview changes without deploying
./infra/deploy.sh --what-if

# Validate template syntax + deployment feasibility
./infra/deploy.sh --validate

# Deploy
./infra/deploy.sh
```

After deploying, the script prints the Container App FQDN and webhook URL. Use the webhook URL when registering the Shopify `orders/create` webhook.

## First-time notes

- **Database**: the `rectella` database is created automatically as part of the Bicep deployment — no manual `CREATE DATABASE` needed.
- **VPN**: the Container App will reach SYSPRO over the existing `Azure-to-Office` tunnel automatically, since `apps-subnet` is in the same VNet. Confirm the tunnel is **Connected** before expecting SYSPRO calls to succeed.
- **First deploy takes ~15 minutes** (PostgreSQL provisioning is the slowest step).
- **Subsequent deploys are idempotent** — re-running `deploy.sh` updates in place.

## Changing the deployed image

The Container App is pinned to `:latest`. To roll out a new image:

```bash
az containerapp update \
  --name rectella-shopify-service \
  --resource-group Shopify-RG \
  --image ghcr.io/trismegistus0/rectella-shopify-service:sha-<commit>
```

Pinning to a `sha-<commit>` tag (available from GHCR) gives you reproducible rollbacks.

## Subnet allocations

| Subnet | CIDR | Purpose |
|--------|------|---------|
| `default` | 10.0.0.0/24 | Existing — unused by this template |
| `GatewaySubnet` | 10.0.1.0/24 | Existing — VPN Gateway |
| `db-subnet` | 10.0.2.0/24 | **New** — PostgreSQL delegated |
| `apps-subnet` | 10.0.4.0/23 | **New** — Container Apps delegated (/23 minimum) |

`10.0.3.0/24` is deliberately left as a gap to allow future `db-subnet` expansion.
