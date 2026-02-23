# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Rectella Shopify Service** is a middleware integration service that bridges Shopify with Syspro8 ERP for Rectella (a CtrlAltInsight client). Built in Go with PostgreSQL for persistence.

### What It Does

- Receives Shopify webhooks (orders, products, inventory, etc.)
- Transforms Shopify data into Syspro8-compatible formats
- Pushes data to Syspro8 via its e.net/REST APIs
- Tracks sync state and provides retry/error handling

### Deployment Phases

1. **Local development** -- Go service + PostgreSQL (Docker Compose), mock Shopify webhook calls
2. **Customer test environment** -- Deploy against Rectella's test Syspro8 database/app server on their network
3. **Cloud staging** -- Full staging deployment in the cloud

## Build & Run

```bash
go build ./...                    # Build all packages
go run ./cmd/server               # Run the service (once cmd/server exists)
go test ./...                     # Run all tests
go test ./internal/webhook/...    # Run tests for a specific package
go test -run TestOrderCreate ./...  # Run a single test by name
go vet ./...                      # Static analysis
```

## Tech Stack

- **Language**: Go (managed via mise in ~/Work/)
- **Database**: PostgreSQL (local dev via Docker Compose)
- **ERP target**: Syspro8 (SOAP e.net web services / REST API)
- **Webhook source**: Shopify Admin API webhooks

## Architecture Notes

Planned layout:

```
cmd/server/          # Main entrypoint
internal/
  webhook/           # Shopify webhook handlers + HMAC verification
  syspro/            # Syspro8 API client (e.net SOAP / REST)
  sync/              # Sync state machine, retries, idempotency
  store/             # PostgreSQL data access layer
  model/             # Shared domain types (orders, products, inventory)
config/              # Configuration loading (env vars, config files)
migrations/          # SQL migration files
docker-compose.yml   # Local dev stack (PostgreSQL)
```

## Key Design Considerations

- **Shopify webhook verification**: All incoming webhooks must be verified via HMAC-SHA256 using the app's shared secret. Reject unverified requests.
- **Idempotency**: Shopify may send the same webhook multiple times. Use the `X-Shopify-Webhook-Id` header to deduplicate.
- **Syspro8 sessions**: Syspro8 APIs require a session token (logon/logoff). Handle token lifecycle and expiry.
- **Retry & dead letter**: Failed Syspro8 pushes should be retried with backoff. After max retries, move to a dead letter queue for manual review.
- **Graceful shutdown**: The service must drain in-flight requests before stopping (important for webhook processing).

## MCP Servers

The following MCP servers are recommended for this project. Install globally or per-project:

```bash
# Shopify API docs and schema reference (essential)
claude mcp add shopify-dev -- npx -y @shopify/dev-mcp@latest

# PostgreSQL read-only access for inspecting dev/test databases
claude mcp add postgres -- npx -y @modelcontextprotocol/server-postgres "postgresql://user:pass@localhost:5432/rectella"

# Up-to-date library documentation (Go stdlib, pgx, chi, etc.)
claude mcp add context7 -- npx -y @upstash/context7-mcp@latest

# HTTP requests for testing endpoints and fetching docs
claude mcp add fetch -- npx -y @anthropic/mcp-server-fetch

# REST API testing with custom headers (Shopify HMAC, Syspro auth)
claude mcp add rest-api -- npx -y @dkmaker/mcp-rest-api
```

Optional depending on workflow:

```bash
# Go LSP intelligence (requires gopls v0.20.0+)
claude mcp add gopls -- mcp-gopls --workspace ~/Work/ctrlaltinsight/rectella-shopify-service

# Terraform/AWS for cloud staging infrastructure
claude mcp add terraform -- npx -y @hashicorp/terraform-mcp-server
```

## Environment

- This is an Omarchy (Arch Linux + Hyprland) system
- Go toolchain managed via mise (`~/Work/.mise.toml` adds `./bin` to PATH)
- Git default branch is `master`, pull rebases, push auto-sets upstream
