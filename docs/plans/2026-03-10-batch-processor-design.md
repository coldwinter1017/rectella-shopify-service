# Batch Processor Design

## Purpose

Connect webhook-persisted orders to SYSPRO. Poll the database for pending orders on a schedule, submit them via SORTOI, update their status.

## Package

`internal/batch/` — owns the polling loop, calls `syspro.Client` and store methods. Started as a goroutine from `cmd/server/main.go`.

## Polling

- Interval: `BATCH_INTERVAL` env var (default 5m)
- Single-flight mutex: if a batch is already running when the next tick fires, skip it
- Query: select orders where `status = 'pending'`, ordered by `created_at`

## SYSPRO Session

- One logon per batch, not per order
- Sequential SORTOI calls (one order per Transaction)
- Logoff when batch completes (or on error bailout)

## Gift Card Filtering

- Remove line items where `gift_card = true` before building SORTOI XML
- If all lines are gift cards, skip the order entirely (mark as `submitted` — nothing to send)
- Gift card purchases and redemptions as non-stocked lines (pending finance approval, issue #5)

## Error Handling

| Error type | Action | Retry? |
|---|---|---|
| Business (SYSPRO rejects order) | Mark `failed`, log error, continue batch | No — needs human fix |
| Infrastructure (connection/timeout) | Stop batch, leave remaining as `pending` | Yes — next cycle |
| Repeated failure (3+ attempts) | Mark `dead_letter` | No — needs investigation |

## Order Status Lifecycle

```
pending → submitted (SYSPRO accepted)
       → failed (business error, needs human fix)
       → pending with attempts++ (infra error, retried next cycle)
       → dead_letter (infra error, attempts >= 3)
```

## Schema Changes

Add to `orders` table:

- `status` text NOT NULL DEFAULT 'pending' — one of: pending, processing, submitted, failed, dead_letter
- `attempts` integer NOT NULL DEFAULT 0
- `last_error` text

## Visibility

- `GET /orders?status=failed` endpoint for operations (issue #6)
- No email alerting in Phase 1

## Dependencies

- `internal/syspro` — Client interface (already built)
- `internal/store` — order queries (needs new methods: fetch pending, update status)
- `config` — BATCH_INTERVAL env var
