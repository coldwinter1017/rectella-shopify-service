# Rectella Phase 1 — Ship by 16th

## To Do
- Finish batch processor design
- Build batch processor
- Add non-stocked SORTOI lines (gift cards)
- GET /orders?status=failed endpoint

## In Progress
- Batch processor design (almost done)

## Blocked
- Gift card GL code + VAT config (waiting on Liz)

## Ready to Test
- SYSPRO connectivity test with real credentials
- End-to-end: webhook → DB → batch → SYSPRO test company

## Done
- Webhook handler
- SYSPRO e.net client
- SORTOI XML builder
- VPN split tunnel script
- Dev tooling (run/check/test/reset/nuke)
