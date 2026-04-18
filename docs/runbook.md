# Rectella Shopify Service — Operator Runbook

One-page reference for keeping the Shopify-SYSPRO integration healthy. Written for Rectella IT / Melanie / Reece. Not for customers.

## What the service does

Middleware between the Barbequick Shopify store and SYSPRO 8 ERP. Three data flows:

1. **Orders in**: Shopify webhook → Postgres (staging) → batch → SYSPRO SORTOI
2. **Stock out**: SYSPRO INVQRY (every 15 min) → Shopify inventory
3. **Fulfilment back**: SYSPRO SORQRY (every 30 min, status 9 = complete) → Shopify fulfilment

All orders post to SYSPRO customer `WEBS01`. Single warehouse. ~40 SKUs.

## Escalation contacts

| Role | Name | Contact | When |
|---|---|---|---|
| Developer | Sebastian Adamo | sebastian@ctrlaltinsight.co.uk | Any code/service issue |
| SYSPRO consultant | Sarah Adamo | sarah@ctrlaltinsight.co.uk | SYSPRO behaviour questions |
| SYSPRO admin (Rectella) | Melanie Higgins | higginsm@rectella.com | Stock codes, warehouse, live SYSPRO data |
| SYSPRO admin (Rectella) | Reece Taylor | taylorr@rectella.com | SYSPRO operator account, session conflicts |
| Rectella Finance | Liz Buckley | buckleyl@rectella.com | Payment posting, commercial decisions |
| Managed IT | Ross Tomlinson (NCS) | helpdesk@ncs.cloud | VPN tunnel, Meraki, Azure infra |
| Project manager | (vacant — Clare Braithwaite departed) | — | (Liz escalates) |

## Common operational tasks

### Check the status of a specific order

```bash
# By database ID, Shopify order ID, or order number — use the admin endpoint:
curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
  "https://<service-url>/orders?status=submitted" | jq
curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
  "https://<service-url>/orders?status=failed" | jq
curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
  "https://<service-url>/orders?status=pending" | jq
```

Statuses: `pending` (waiting for batch), `processing` (batch in flight to SYSPRO), `submitted` (accepted by SYSPRO), `failed` (SYSPRO rejected), `dead_letter` (3+ infra failures), `fulfilled` (shipped + Shopify fulfilment created), `cancelled`.

### Retry a failed or dead-lettered order

```bash
curl -sS -X POST -H "X-Admin-Token: $ADMIN_TOKEN" \
  "https://<service-url>/orders/<ID>/retry"
```

This moves the order from `failed` or `dead_letter` back to `pending`, to be picked up on the next batch cycle (within 5 minutes).

### Tail the service logs (Azure App Service)

```bash
az webapp log tail -g Shopify-RG -n rectella-shopify-service
```

Watch for ERROR and WARN lines. Key strings:
- `order submitted to SYSPRO` — success, look for `syspro_order` field
- `SYSPRO rejected order` — business error, see `error` field
- `SYSPRO submission failed (infra)` — network / VPN / session error
- `webhook HMAC verification failed` — secret mismatch (see below)
- `without traceable number (clean import)` — order went to SYSPRO with no returned number (known limitation, reconcile manually)

### Roll back to a previous revision

```bash
# List recent revisions
az webapp deployment list-publishing-profiles -g Shopify-RG -n rectella-shopify-service

# Or pin the container image to a previous SHA
az webapp config container set -g Shopify-RG -n rectella-shopify-service \
  --docker-custom-image-name ghcr.io/trismegistus0/rectella-shopify-service:sha-<commit>
```

The service restarts automatically after an image swap. Shopify retries webhooks for 48h so no orders are lost during the restart window.

## Incident playbook

### "An order was cancelled in Shopify but the warehouse is still going to ship it"

**Known limitation.** The service does NOT receive `orders/cancelled` webhooks in Phase 1.

1. Immediately call Reece/Melanie to cancel the order in SYSPRO manually (Sales Order Entry → find the order number → cancel).
2. If the order hasn't reached SYSPRO yet (still in `pending` in the service), you can use the admin API to mark it `cancelled`:
   ```bash
   # TODO: this endpoint does not yet exist. Until it does, cancel in SYSPRO only.
   ```
3. Refund the customer via the Shopify admin.

**Do not** cancel orders in Shopify and expect the integration to react. Always cancel in SYSPRO first.

### "SYSPRO is unreachable — batch processor is failing"

1. Check the VPN tunnel health: `az network vpn-connection show -g Shopify-RG -n Azure-to-Office --query connectionStatus`. Should be `Connected`.
2. If `NotConnected`: raise with NCS (Ross Tomlinson, helpdesk@ncs.cloud). Reference ticket #44257 if needed.
3. While the tunnel is down: orders continue to stage in Postgres safely. No customer impact until the backlog gets large (several hours).
4. Once the tunnel is restored, the batch processor drains the pending queue automatically within one cycle (5 minutes).

### "Stock quantities in Shopify look wrong"

1. Compare SYSPRO `INVQRY` output for the affected SKUs against what Shopify shows: use the admin product detail page.
2. If wrong in Shopify but right in SYSPRO: the next stock-sync cycle (every 15 min, or triggered by next order) will correct it.
3. If you need an immediate fix: trigger a sync by placing a test order (any order fires the debounced sync), or restart the service.
4. If wrong in SYSPRO: fix in SYSPRO and wait for the sync. The service never writes inventory to SYSPRO.

### "Webhook HMAC rejection storm — every Shopify webhook returns 401"

1. The `SHOPIFY_WEBHOOK_SECRET` app setting does not match what Shopify is signing with.
2. In Shopify admin → Settings → Notifications → Webhooks, click the webhook, find the "Signing secret" (or similar label).
3. Update the App Service setting:
   ```bash
   az webapp config appsettings set -g Shopify-RG -n rectella-shopify-service \
     --settings SHOPIFY_WEBHOOK_SECRET=<new-value>
   ```
4. The service auto-restarts. Shopify retries the failed webhooks within 48 hours — no orders lost.

### "Unpaid order skipped in logs"

**Expected behaviour.** The service rejects orders with `financial_status` other than `paid` or `partially_paid`. This prevents shipping unpaid inventory.

**IMPORTANT — how recovery actually works:**

Shopify does NOT re-fire `orders/create` when the payment later succeeds. It fires `orders/paid` or `orders/updated` instead, neither of which this service subscribes to in Phase 1. That means a "paid-later" order would be silently lost unless the reconciliation sweeper catches it.

**The reconciliation sweeper is the recovery path.** It polls Shopify Admin REST every `RECONCILIATION_INTERVAL` (mandatory `15m` for launch) and re-stages any orders that exist in Shopify but not in our DB. On service startup it runs immediately (not after the first interval tick), so a manual restart is the fast-recovery lever when an operator notices a missing order.

**Triage: "customer paid in Shopify, nothing in our /orders":**

1. Confirm the order ID is missing from our DB:
   ```bash
   curl -sS -H "X-Admin-Token: $ADMIN_TOKEN" \
     "https://<service-url>/orders?status=pending" | jq '.[].shopify_order_id'
   ```
2. Check Shopify admin to confirm the order is marked `paid` (or `partially_paid`) and its `created_at` is within the last 48 hours.
3. Force a reconciliation cycle: restart the service. `az webapp restart -g Shopify-RG -n rectella-shopify-service`. The sweeper runs its first tick immediately on boot.
4. Wait 60 seconds. Verify the order now appears in `/orders?status=pending`. Batch processor picks it up within `BATCH_INTERVAL` (5 min default).
5. If the order STILL hasn't appeared after a full reconciliation cycle, the order may have been created before `RECONCILIATION_INTERVAL`'s lookback window (default 48h). Contact Sebastian — manual re-stage is needed.

**Known gap:** if `RECONCILIATION_INTERVAL` is unset or zero, this recovery path is DISABLED. The Bicep defaults it to `15m` but verify with `az webapp config appsettings list -g Shopify-RG -n rectella-shopify-service --query "[?name=='RECONCILIATION_INTERVAL']"` if in doubt.

### "Postgres is down or slow"

1. `az postgres flexible-server show -g Shopify-RG -n rectella-db --query state`. Should be `Ready`.
2. If failing: check Azure service health. For maintenance: patience (Shopify retries webhooks for 48h). For real outage: PITR restore to a fresh server, swap `DATABASE_URL`, restart.
3. Contact Sebastian for the PITR restore procedure.

### "Order in `submitted` status has empty `syspro_order_number`"

**Known limitation.** SYSPRO 8 SORTOI clean imports (no warnings) do not return a sales order number in the response. The order IS in SYSPRO — but we can't trace it from this service. To reconcile:

1. Use the Shopify order number (e.g. `#BBQ1010`) as the `CustomerPoNumber` in SYSPRO.
2. In SYSPRO Sales Order Entry, search for that `CustomerPoNumber` to find the actual sales order number.
3. Update the `orders` table in Postgres manually if traceability is critical.

Long-term fix is in the Phase 2 backlog.

### "Order stuck in `processing` status for more than 10 minutes"

This means the service marked the order as `processing` (atomic transition from `pending`) and then something crashed or hung before it could finalise.

1. Check logs for the order_id: `az webapp log tail ... | grep "order_id.*<ID>"`.
2. If the SORTOI call completed successfully: look for the `order submitted to SYSPRO` line with a `syspro_order` value. Update the DB row manually to `submitted`.
3. If the SORTOI call failed mid-flight: the order is in an ambiguous state. Check SYSPRO directly to see if the order was created. If yes, update the DB manually. If no, reset to `pending` (`UPDATE orders SET status='pending' WHERE id=<ID>`) and it will be retried.
4. This should be rare. If it happens repeatedly, escalate to Sebastian.

## Known limitations (Phase 1)

The following are NOT implemented and require manual workarounds:

- **Order cancellation handler** — cancelling in Shopify does not cancel in SYSPRO. Cancel in SYSPRO directly.
- **Gift cards** — gift card products must be disabled on the live store until Phase 2. Pending Liz Buckley sign-off on the technical approach.
- **Returns / refunds** — not handled by the service. Process manually in Shopify and SYSPRO.
- **Multi-warehouse** — the service reads from one warehouse only (`SYSPRO_WAREHOUSE` env var).
- **ERP-to-Shopify pricing** — Shopify owns pricing. SYSPRO prices are not synced.
- **Payment posting** — Finance posts payments manually in SYSPRO after reconciliation.
- **Clean-import traceability** — see incident playbook above.

## Service configuration reference

Key App Service env vars:

| Variable | Purpose | Required |
|---|---|---|
| `DATABASE_URL` | Postgres connection string | yes |
| `SHOPIFY_WEBHOOK_SECRET` | HMAC secret from Shopify webhook settings | yes |
| `SHOPIFY_ACCESS_TOKEN` | Admin API token from the custom app (for stock + fulfilment sync) | yes for full function |
| `SHOPIFY_STORE_URL` | e.g. `rectella.myshopify.com` | yes |
| `SHOPIFY_LOCATION_ID` | Shopify location GID (auto-discovered if empty) | no |
| `SYSPRO_ENET_URL` | SYSPRO e.net REST endpoint | yes |
| `SYSPRO_OPERATOR` | SYSPRO operator account used by the service | yes |
| `SYSPRO_PASSWORD` | SYSPRO operator password | yes |
| `SYSPRO_COMPANY_ID` | SYSPRO company code | yes |
| `SYSPRO_WAREHOUSE` | Warehouse code (e.g. `WEBS`) | yes for stock sync |
| `SYSPRO_SKUS` | Comma-separated list of SKUs to sync | yes for stock sync |
| `ADMIN_TOKEN` | Shared secret for admin endpoints | recommended |
| `BATCH_INTERVAL` | Default 5m | no |
| `STOCK_SYNC_INTERVAL` | Default 15m | no |
| `FULFILMENT_SYNC_INTERVAL` | Default 30m | no |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | default `info` |

## When to call Sebastian

- Any code change is needed
- Unexplained errors or panics in logs
- Data corruption or inconsistent state between Shopify, the service DB, and SYSPRO
- Postgres restore required
- App Service rollback to a specific commit
- Anything that is not covered by this runbook

## When to call NCS

- VPN tunnel `Azure-to-Office` is not `Connected`
- Meraki-side routing changes
- Azure network fault
- Questions about Rectella IT infrastructure outside Shopify-RG

## When to call Melanie or Reece

- SYSPRO operator session conflicts (service was evicted by a human logging in)
- Confirming SYSPRO stock codes and warehouse codes
- Cancelling orders in SYSPRO
- Checking whether an order reached SYSPRO when the service is unclear
