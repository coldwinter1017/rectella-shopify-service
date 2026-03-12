#!/bin/bash

# Probe RIL-APP01 for SYSPRO e.net REST endpoints.
# Tries all candidate ports x all known URL patterns.
# Read-only — modifies nothing on this machine.
#
# Usage: ./scripts/probe-enet.sh
# Requires: VPN connected (192.168.3.150 reachable)

set -euo pipefail

HOST="192.168.3.150"
TIMEOUT=5
DELAY=3

PORTS=(80 443 8082 30110 30180 30181 30190)
PATHS=("SYSPROWCFService/Rest" "saborest" "SYSPRORestApi" "")

echo "=== e.net Port Discovery ==="
echo "Host: $HOST"
echo "Ports: ${PORTS[*]}"
echo "Delay: ${DELAY}s between requests"
echo ""

# Quick TCP check first — skip ports that aren't open
echo "--- TCP reachability ---"
open_ports=()
for port in "${PORTS[@]}"; do
  if timeout 2 bash -c "echo >/dev/tcp/$HOST/$port" 2>/dev/null; then
    echo "  OPEN  $port"
    open_ports+=("$port")
  else
    echo "  CLOSED  $port"
  fi
  sleep "$DELAY"
done
echo ""

if (( ${#open_ports[@]} == 0 )); then
  echo "No open ports found. Is VPN connected?"
  exit 1
fi

# Probe each open port with each path pattern
echo "--- HTTP probing ---"
found=()
for port in "${open_ports[@]}"; do
  for path in "${PATHS[@]}"; do
    if [[ -n $path ]]; then
      target="http://$HOST:$port/$path/Logon"
    else
      target="http://$HOST:$port/Logon"
    fi

    result=$(curl -s -o /dev/null -w "%{http_code}" --max-time "$TIMEOUT" "$target" 2>/dev/null || echo "000")

    if [[ $result == "000" ]]; then
      label="TIMEOUT/RESET"
    elif [[ $result == "200" || $result == "405" ]]; then
      # 200 = endpoint exists (GET might work)
      # 405 = Method Not Allowed (POST-only — this IS e.net!)
      label="FOUND"
      found+=("$target (HTTP $result)")
    elif [[ $result == "404" ]]; then
      label="NOT FOUND"
    else
      label="HTTP $result"
    fi

    printf "  %-6s  %s  %s\n" "$result" "$label" "$target"
    sleep "$DELAY"
  done
done

# Also try HTTPS on 443
if printf '%s\n' "${open_ports[@]}" | grep -q "^443$"; then
  echo ""
  echo "--- HTTPS probing (port 443) ---"
  for path in "${PATHS[@]}"; do
    if [[ -n $path ]]; then
      target="https://$HOST:443/$path/Logon"
    else
      target="https://$HOST:443/Logon"
    fi

    result=$(curl -s -o /dev/null -w "%{http_code}" --max-time "$TIMEOUT" -k "$target" 2>/dev/null || echo "000")

    if [[ $result == "000" ]]; then
      label="TIMEOUT/RESET"
    elif [[ $result == "200" || $result == "405" ]]; then
      label="FOUND"
      found+=("$target (HTTP $result)")
    elif [[ $result == "404" ]]; then
      label="NOT FOUND"
    else
      label="HTTP $result"
    fi

    printf "  %-6s  %s  %s\n" "$result" "$label" "$target"
    sleep "$DELAY"
  done
fi

echo ""

# For any hits, fetch response body for inspection
if (( ${#found[@]} > 0 )); then
  echo "=== CANDIDATES ==="
  for entry in "${found[@]}"; do
    url="${entry%% (*}"
    echo ""
    echo ">>> $entry"
    echo "--- Response body (first 500 chars) ---"
    curl -s --max-time "$TIMEOUT" "$url" 2>/dev/null | head -c 500
    echo ""
    echo "--- POST attempt (dummy creds) ---"
    curl -s --max-time "$TIMEOUT" -X POST \
      -H "Content-Type: application/x-www-form-urlencoded" \
      -d "Operator=probe&OperatorPassword=&CompanyId=probe" \
      "$url" 2>/dev/null | head -c 500
    echo ""
    sleep "$DELAY"
  done
else
  echo "No e.net endpoints found on any port/path combination."
  echo ""
  echo "Next steps:"
  echo "  1. Ask Reece Taylor / NCS (ticket #44257) which port e.net is on"
  echo "  2. Check if e.net service is running: get someone to check IIS on RIL-APP01"
  echo "  3. Try: http://RIL-APP01:30000/saborest from a machine ON the Rectella LAN"
fi
