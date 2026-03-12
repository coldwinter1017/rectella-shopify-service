# Rectella Shopify Service — Project Constraints

Last updated: 2026-03-12

## Contractual

| Constraint | Detail | Source |
|------------|--------|--------|
| Fixed price | £11,000, invoiced at go-live | SOW section 8 |
| Go-live date | 31 March 2026 | SOW section 7 |
| Hypercare | 4 weeks post go-live | SOW section 7 |
| Change control | Written variation required, £650/day | SOW section 9 |
| Hosting excluded | Cloud subscription held by Rectella, not us | SOW section 6 |
| Hosting budget | £80–150/month (SOW estimate) | SOW section 6 |

## Phase 1 Scope — What We Deliver

| Deliverable | Status | SOW ref | GH issue |
|-------------|--------|---------|----------|
| Shopify → SYSPRO order integration | In progress | 2.1 | #1 |
| Stock synchronisation (SYSPRO → Shopify) | Not started | 2.2 | — |
| Shipment status feedback (SYSPRO → Shopify) | Not started | 2.3 | — |
| Payment references carried into SYSPRO | In progress (via SORTOI) | 2.4 | — |
| Staging database + queue-based processing | Built | 2.5 | — |
| Error logging + failure visibility | Not started | 2.5 | #6 |
| Documentation (process + technical) | In progress | 3 | — |
| Test plan + test scripts + results | Partial (31 unit tests, 10 integration) | 3 | — |
| Go-live support | Not started | 3 | — |
| Four weeks hypercare | Not started | 3 | — |

### SOW Deliverable Details

**2.1 Order integration** includes:
- Receive confirmed B2C orders via secure webhooks ✅
- Store in staging database before SYSPRO submission ✅
- Queue-based workflow for reliability (batch processor — #1, designed, not built)
- Create Sales Orders via SORTOI business object (client built, not wired)
- All orders to WEBS01 ✅
- Carry order lines, pricing, discounts, delivery charges, VAT, payment references (discounts + delivery charges not yet implemented in SORTOI XML)
- Support cancellations prior to fulfilment (not started)

**2.2 Stock synchronisation** includes:
- Scheduled sync from single nominated warehouse
- Update Shopify availability from SYSPRO quantities
- NOT real-time, NOT multi-warehouse
- Must use e.net business objects (Query/Browse), NOT direct SQL

**2.3 Shipment status feedback** includes:
- When shipment confirmed in SYSPRO, update order status in Shopify
- Basic customer visibility only
- No warehouse dashboard, no 3PL UI

**2.4 Payments**:
- Carry payment references and values into SYSPRO alongside Sales Order
- No automated posting to Debtors/Cash Book

## Phase 1 Scope — What We Do NOT Deliver

Explicit exclusions from SOW section 5 + Phase 1 scope boundaries:

- Multi-warehouse allocation logic
- ERP-driven promotion synchronisation into Shopify
- Automated posting of payments into SYSPRO Debtors and Cash Book
- Returns and refund workflows
- 3PL operational dashboard
- Carrier integrations beyond basic shipment status
- Hosting provision, cloud subscription management, backups, security patching, infrastructure monitoring
- Subscription products
- Real-time stock sync

## Technical Constraints

### SYSPRO e.net

| Constraint | Detail |
|------------|--------|
| Protocol | NetTcp on port 31001 (read/write, business objects) |
| Backup protocol | REST HTTP on port 40000 (read, SOAP-based) |
| No direct SQL | All reads/writes through e.net business objects (user confirmed 2026-03-12) |
| App server | RIL-APP01 (192.168.3.150) |
| Business object | SORTOI for sales order import |
| Session model | Logon → N transactions → Logoff (reuse session per batch) |
| Customer | All orders → WEBS01 |
| Company ID | From environment variable |

### Shopify

| Constraint | Detail |
|------------|--------|
| Webhook verification | HMAC-SHA256 required on all webhooks |
| Idempotency | Deduplicate on X-Shopify-Webhook-Id |
| Pricing | Shopify controls pricing Phase 1 (not ERP) |
| SKU count | 13 simple stocked items at launch |
| Gift cards | Multi-purpose, zero VAT, non-stocked lines in SORTOI (pending Liz approval) |

### Database

| Constraint | Detail |
|------------|--------|
| Engine | PostgreSQL 16 |
| Driver | pgx/v5 (only external dependency) |
| Migrations | Embedded SQL, auto-applied at startup |
| Connection pooling | pgxpool |

## Deployment Constraints

### Recommended Architecture

| Component | Service | Est. monthly cost |
|-----------|---------|-------------------|
| Application | Azure Container Apps (single Go binary) | £15–30 |
| Database | Azure Database for PostgreSQL Flexible Server (Burstable B1ms) | £10–15 |
| Connectivity | Azure VPN Gateway (Basic SKU) → Rectella Meraki | ~£25 |
| **Total** | | **~£55–75/month** |

### Why Container Apps (not serverless)

- Service is a long-running Go process with batch polling loop
- Serverless would require architectural rework (timer triggers, cold starts, connection pooling)
- Container Apps supports scale-to-zero for cost savings
- Identical binary to local development — no code changes for deployment

### Connectivity

- Azure VPN Gateway creates site-to-site tunnel to Rectella's Cisco Meraki
- NCS (Rectella's managed IT) must configure their end
- Service reaches RIL-APP01 over private network, same as local dev over VPN
- Ports required: inbound 31001, 40000 on RIL-APP01 (already requested from Reece)

### Responsibility Split

| Responsibility | Owner |
|----------------|-------|
| Application code + Docker image | Ctrl Alt Insight |
| Azure subscription + billing | Rectella |
| Azure resource provisioning | Ctrl Alt Insight (guided setup) |
| Meraki VPN config (Rectella side) | NCS |
| Azure VPN Gateway config | Ctrl Alt Insight |
| Ongoing monitoring | Ctrl Alt Insight (hypercare), then Rectella |
| Infrastructure patching/security | Rectella (excluded from SOW) |

## Operational Constraints

| Constraint | Detail |
|------------|--------|
| SYSPRO source of truth | All stock/order data originates in SYSPRO |
| Stage-then-process | Never call SYSPRO from webhook handler |
| Batch interval | Default 5 minutes (configurable) |
| Stock sync interval | Default 15 minutes (configurable) |
| Graceful shutdown | Drain in-flight requests before stopping |
| Retry policy | 3 attempts max → dead_letter |

## Stakeholder Constraints

| Constraint | Detail |
|------------|--------|
| IT changes via NCS | Any server/network changes go through NCS helpdesk (ticket #44257) |
| Gift card approach | Sarah's non-stocked line proposal, pending Liz approval |
| Warehouse TBD | Single warehouse for stock sync, not yet nominated by Rectella |
| SYSPRO test company | Sebastian has credentials, testing blocked until e.net port confirmed |

## Open Questions

| Question | Owner | Blocker? |
|----------|-------|----------|
| Which warehouse for stock sync? | Melanie/Reece | Yes — needed before stock sync build |
| Gift card GL code + VAT config | Liz | Blocks gift card implementation (#5) |
| Are ports 31001/40000 now open on RIL-APP01? | Reece | Yes — blocks all SYSPRO connectivity |
| Which SYSPRO business object for inventory query? | Sarah | Needed for stock sync design |
| Which SYSPRO business object for shipment status? | Sarah | Needed for despatch feedback design |
| Azure subscription — does Rectella have one? | Liz/Clare | Needed before deployment |
