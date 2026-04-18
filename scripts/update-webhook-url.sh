#!/bin/bash

# Wait for cloudflared to register its tunnel URL, then update Shopify webhooks.
# Called by cloudflared.service ExecStartPost.
#
# Pings $HC_PING_URL_WEBHOOK_UPDATE on success, /fail on error (if set).

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  set -a
  . ./.env
  set +a
fi

HC_URL="${HC_PING_URL_WEBHOOK_UPDATE:-}"

ping_hc() {
  # $1 = "" for success, "/fail" for failure
  [[ -z "$HC_URL" ]] && return 0
  curl -fsS --max-time 10 --retry 3 "${HC_URL}${1}" >/dev/null 2>&1 || true
}

fail() {
  echo "update-webhook-url: FAIL — $1" >&2
  ping_hc "/fail"
  exit 1
}

if [[ -z "${SHOPIFY_ACCESS_TOKEN:-}" || -z "${SHOPIFY_STORE_URL:-}" ]]; then
  fail "missing SHOPIFY_ACCESS_TOKEN or SHOPIFY_STORE_URL"
fi

# Find the current tunnel URL: most recent URL in cloudflared logs wins.
# When called by cloudflared ExecStartPost the URL appears within seconds;
# for ad-hoc invocations we look back further.
TUNNEL_URL=""
for i in $(seq 1 30); do
  TUNNEL_URL=$(journalctl --user -u cloudflared --since "12 hours ago" -o cat 2>/dev/null \
    | grep -oP 'https://[a-z0-9]+(?:-[a-z0-9]+)+\.trycloudflare\.com' | tail -1) || true
  if [[ -n "$TUNNEL_URL" ]]; then
    break
  fi
  sleep 1
done

[[ -z "$TUNNEL_URL" ]] && fail "could not detect tunnel URL from cloudflared logs after 30s"

echo "update-webhook-url: detected tunnel $TUNNEL_URL"

API_BASE="https://${SHOPIFY_STORE_URL}/admin/api/2025-04"

WEBHOOKS=$(curl -fsS -H "X-Shopify-Access-Token: $SHOPIFY_ACCESS_TOKEN" "$API_BASE/webhooks.json" 2>&1) \
  || fail "could not fetch webhooks list from Shopify Admin API"

# Update each webhook — track failures
UPDATED=0
FAILED=0
while read -r WEBHOOK_ID TOPIC; do
  case "$TOPIC" in
    orders/create)    PATH_SUFFIX="/webhooks/orders/create" ;;
    orders/cancelled) PATH_SUFFIX="/webhooks/orders/cancelled" ;;
    *) echo "update-webhook-url: skipping unknown topic $TOPIC"; continue ;;
  esac

  NEW_URL="${TUNNEL_URL}${PATH_SUFFIX}"
  RESULT=$(curl -fsS -X PUT \
    -H "X-Shopify-Access-Token: $SHOPIFY_ACCESS_TOKEN" \
    -H "Content-Type: application/json" \
    "$API_BASE/webhooks/$WEBHOOK_ID.json" \
    -d "{\"webhook\":{\"id\":$WEBHOOK_ID,\"address\":\"$NEW_URL\"}}" 2>&1) || {
    echo "update-webhook-url: HTTP error updating $TOPIC: $RESULT" >&2
    FAILED=$((FAILED+1))
    continue
  }

  if echo "$RESULT" | python3 -c "import sys,json; w=json.load(sys.stdin).get('webhook',{}); sys.exit(0 if w.get('address') == '$NEW_URL' else 1)" 2>/dev/null; then
    echo "update-webhook-url: $TOPIC -> $NEW_URL"
    UPDATED=$((UPDATED+1))
  else
    echo "update-webhook-url: FAILED to update $TOPIC (response did not echo expected URL)" >&2
    FAILED=$((FAILED+1))
  fi
done < <(echo "$WEBHOOKS" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for w in data.get('webhooks', []):
    print(f'{w[\"id\"]} {w[\"topic\"]}')
" 2>/dev/null)

if [[ $FAILED -gt 0 ]]; then
  fail "$FAILED webhook update(s) failed (succeeded: $UPDATED)"
fi

if [[ $UPDATED -eq 0 ]]; then
  fail "no webhooks were updated (none found?)"
fi

echo "update-webhook-url: done — $UPDATED updated"
ping_hc ""
