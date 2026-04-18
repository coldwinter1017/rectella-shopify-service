# Shopify Webhook Testing Guide

Quick reference for sending Shopify webhooks at the service — both local (Cloudflare tunnel) and Azure production.

## Prerequisites

- Service running and reachable on a public URL (tunnel locally, or App Service hostname in prod)
- `SHOPIFY_WEBHOOK_SECRET` in the service env matches the **Signing secret** shown in Shopify admin

## Setup in Shopify admin

1. **Settings → Notifications → Webhooks → Create webhook**
2. **Event:** `Order creation`
3. **Format:** `JSON`
4. **URL:** `https://<your-public-hostname>/webhooks/orders/create`
5. **API version:** latest stable (e.g. `2025-01`)
6. Save. Shopify reveals the **Signing secret** — copy it immediately (it's only shown once on creation for some plans).
7. Update the service env var `SHOPIFY_WEBHOOK_SECRET` to the exact string Shopify shows. Restart the service.

## Testing — two options

### Option A: Static test webhook (fastest sanity check)

In the Shopify webhook settings, click **Send test notification**.

- Proves: tunnel reachable, HMAC verification works, handler path wired
- Does NOT prove: that the payload can be turned into a valid SORTOI (payload is a hard-coded placeholder order)
- Expected: HTTP 200 from the handler, `webhook_events` row inserted in Postgres, but the order may fail at batch-processor stage because the SKU likely isn't real

### Option B: Real test order (full round-trip)

Place an actual order in the dev store (either buy a product on the storefront or create a Draft order in admin and mark paid).

- Proves: end-to-end flow including SORTOI submission to SYSPRO
- Expected flow:
  1. Webhook received, HMAC verified → `200 OK` to Shopify
  2. Row in `webhook_events` + row in `orders` (status: `pending`)
  3. Batch processor picks it up within `BATCH_INTERVAL` (default 5 min)
  4. SORTOI posted to SYSPRO → response contains `<SalesOrder>NNNNNN</SalesOrder>`
  5. Order row updated: status → `submitted`, `syspro_order_number` populated

## Watching the flow

**Tail service logs** (local):

```bash
tail -f /tmp/rectella-service.log
# or if using run.sh directly, the logs print to stdout
```

**Tail service logs** (Azure App Service):

```bash
az webapp log tail -g Shopify-RG -n rectella-shopify-service
```

**Check orders by status** (admin auth if `ADMIN_TOKEN` set):

```bash
curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
  "http://localhost:8080/orders?status=submitted" | jq
curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
  "http://localhost:8080/orders?status=failed" | jq
```

**Retry a failed order:**

```bash
curl -sS -X POST -H "X-Admin-Token: $ADMIN_TOKEN" \
  http://localhost:8080/orders/<ID>/retry
```

**Confirm order on SYSPRO side** (from dev machine with VPN up):

```bash
./scripts/vpn.sh up
go run ./cmd/sorqrytest -order <syspro-order-number>
```

## Common failure modes

| Symptom | Cause | Fix |
|---|---|---|
| Shopify shows webhook delivered but service logs nothing | Tunnel down or service not on expected port | `curl https://<public-url>/health` to verify reachability |
| 401 "invalid signature" in service logs | Secret mismatch between Shopify and service env | Copy secret from Shopify settings, update env, restart service |
| Webhook 200 but order stays `pending` forever | Batch processor not running, SYSPRO unreachable, or bad SYSPRO creds | Check service startup log for `batch processor started`, `./scripts/vpn.sh test` |
| Order goes to `failed` with "invalid stock code" | SKU in Shopify doesn't exist in SYSPRO company | Use SKUs that exist in RILT for testing; for prod, ensure Shopify SKUs match SYSPRO stock codes |
| `dead_letter` status | Order failed 3+ retry attempts | Read `last_error` column, fix upstream, `POST /orders/{id}/retry` |

## Switching tunnel URL (local dev)

Cloudflare `trycloudflare.com` tunnels rotate the subdomain on every launch. When it changes:

1. Restart `cloudflared tunnel --url http://localhost:8080`
2. Copy new URL from output
3. Shopify admin → Webhooks → edit → paste new URL → save
4. Signing secret stays the same (webhook identity is preserved on URL-only changes)

## Switching to the production Azure URL

Once App Service is deployed tomorrow:

1. Delete or disable the Cloudflare-tunnel webhook in dev store
2. In the **live Rectella Shopify store** admin, create a new webhook pointing at the App Service URL
3. Copy that signing secret into App Service env vars (`az webapp config appsettings set`)
4. Do a low-value test order before announcing go-live

## Reference — relevant source files

- `internal/webhook/handler.go` — entry point for webhook delivery
- `internal/webhook/verify.go` — HMAC-SHA256 verification
- `internal/webhook/payload.go` — Shopify JSON DTO definitions
- `internal/batch/processor.go` — polling loop that submits to SYSPRO
- `internal/syspro/xml.go` — SORTOI XML builder
- `cmd/server/main.go` — HTTP routes and middleware
