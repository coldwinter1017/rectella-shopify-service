#!/bin/bash

# Connect/disconnect Rectella VPN alongside Mullvad.
#
# Uses mullvad-exclude to run openconnect outside the Mullvad tunnel.
# Mullvad stays on permanently — no disconnect/reconnect dance.
#
# Usage: ./scripts/vpn.sh up|down|status|test

set -euo pipefail

VPN_HOST="rectella-internationa-wireless-w-tqngtmvdtj.dynamic-m.com"
OPENCONNECT="/usr/bin/openconnect"
PID_FILE="/tmp/rectella-vpn.pid"

# Known Rectella internal IP for health checks (your VPN-assigned address).
HEALTH_IP="172.18.251.117"

vpn_up() {
  if [[ -f "$PID_FILE" ]] && sudo kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "VPN already running (PID $(cat "$PID_FILE"))"
    vpn_test
    return 0
  fi

  # Load VPN credentials from .env
  if [[ -f .env ]]; then
    VPN_USER=$(grep -oP '^VPN_USERNAME=\K.*' .env)
    VPN_PASS=$(grep -oP '^VPN_PASSWORD=\K.*' .env)
  fi

  if [[ -z "${VPN_USER:-}" || -z "${VPN_PASS:-}" ]]; then
    echo "Missing VPN_USERNAME or VPN_PASSWORD in .env"
    exit 1
  fi

  # Launch openconnect excluded from Mullvad tunnel.
  # mullvad-exclude uses cgroups — child processes (via sudo) inherit the exclusion.
  echo "Connecting to Rectella VPN (excluded from Mullvad)..."
  echo "$VPN_PASS" | mullvad-exclude sudo "$OPENCONNECT" \
    --user="$VPN_USER" \
    --passwd-on-stdin \
    --background \
    --pid-file="$PID_FILE" \
    "$VPN_HOST"

  # Wait for tun0 to come up.
  echo -n "Waiting for tunnel..."
  for i in $(seq 1 15); do
    if ip link show tun0 &>/dev/null; then
      echo " up"
      break
    fi
    echo -n "."
    sleep 1
  done

  if ! ip link show tun0 &>/dev/null; then
    echo " failed"
    vpn_down
    exit 1
  fi

  echo ""
  vpn_test
}

vpn_down() {
  echo "Disconnecting Rectella VPN..."

  if [[ -f "$PID_FILE" ]]; then
    sudo kill "$(cat "$PID_FILE")" 2>/dev/null || true
    sudo rm -f "$PID_FILE"
  else
    sudo pkill openconnect 2>/dev/null || true
  fi

  # Wait for tun0 to disappear.
  for i in $(seq 1 5); do
    ip link show tun0 &>/dev/null || break
    sleep 1
  done

  echo "VPN disconnected."
}

vpn_status() {
  echo "=== Rectella VPN ==="
  if [[ -f "$PID_FILE" ]] && sudo kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "Connected (PID $(cat "$PID_FILE"))"
    ip route show dev tun0 2>/dev/null | sed 's/^/  /'
  else
    echo "Disconnected"
    rm -f "$PID_FILE" 2>/dev/null || true
  fi

  echo ""
  echo "=== Mullvad ==="
  mullvad status 2>/dev/null || echo "Not installed"
}

vpn_test() {
  echo "=== Connectivity Test ==="
  pass=0
  fail=0

  # 1. Mullvad is connected
  if mullvad status 2>/dev/null | grep -q "Connected"; then
    echo "  PASS  Mullvad connected"
    pass=$((pass + 1))
  else
    echo "  FAIL  Mullvad not connected"
    fail=$((fail + 1))
  fi

  # 2. tun0 exists (VPN tunnel interface)
  if ip link show tun0 &>/dev/null; then
    echo "  PASS  tun0 interface exists"
    pass=$((pass + 1))
  else
    echo "  FAIL  tun0 interface missing"
    fail=$((fail + 1))
  fi

  # 3. Can reach VPN-assigned IP (proves tunnel is passing traffic)
  if ping -c 1 -W 3 "$HEALTH_IP" &>/dev/null; then
    echo "  PASS  Ping $HEALTH_IP (VPN internal)"
    pass=$((pass + 1))
  else
    echo "  FAIL  Ping $HEALTH_IP (VPN internal)"
    fail=$((fail + 1))
  fi

  # 4. External traffic goes through Mullvad (not leaking real IP)
  external_ip=$(curl -4 -s --max-time 5 ifconfig.me 2>/dev/null || echo "")
  mullvad_ip=$(mullvad status 2>/dev/null | grep -oP 'IPv4: \K[0-9.]+' || echo "")
  if [[ -n "$external_ip" && "$external_ip" == "$mullvad_ip" ]]; then
    echo "  PASS  External traffic via Mullvad ($external_ip)"
    pass=$((pass + 1))
  elif [[ -n "$external_ip" ]]; then
    echo "  FAIL  External traffic NOT via Mullvad (got $external_ip, expected $mullvad_ip)"
    fail=$((fail + 1))
  else
    echo "  SKIP  Could not determine external IP"
  fi

  # 5. Rectella subnet routes exist
  rectella_routes=$(ip route show dev tun0 2>/dev/null | wc -l)
  if (( rectella_routes >= 5 )); then
    echo "  PASS  Rectella routes present ($rectella_routes routes via tun0)"
    pass=$((pass + 1))
  else
    echo "  FAIL  Rectella routes missing (only $rectella_routes via tun0)"
    fail=$((fail + 1))
  fi

  echo ""
  echo "Results: $pass passed, $fail failed"
  (( fail == 0 )) && return 0 || return 1
}

case "${1:-}" in
  up)     vpn_up ;;
  down)   vpn_down ;;
  status) vpn_status ;;
  test)   vpn_test ;;
  *)
    echo "Usage: ./scripts/vpn.sh up|down|status|test"
    exit 1
    ;;
esac
