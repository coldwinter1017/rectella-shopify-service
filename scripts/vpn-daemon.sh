#!/bin/bash

# Foreground VPN daemon for systemd. Starts openconnect, fixes DNS/hosts,
# then sleeps until killed. systemd handles restart-on-failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

VPN_HOST="rectella-internationa-wireless-w-tqngtmvdtj.dynamic-m.com"
OPENCONNECT="/usr/bin/openconnect"
PID_FILE="/tmp/rectella-vpn.pid"

# Load VPN credentials from .env
if [[ -f .env ]]; then
  VPN_USER=$(grep -oP '^VPN_USERNAME=\K.*' .env)
  VPN_PASS=$(grep -oP '^VPN_PASSWORD=\K.*' .env)
fi

if [[ -z "${VPN_USER:-}" || -z "${VPN_PASS:-}" ]]; then
  echo "vpn-daemon: missing VPN_USERNAME or VPN_PASSWORD"
  exit 1
fi

# Post-connect hook: after tun0 comes up, apply the DNS fix. Without this,
# vpnc-script leaves tun0 with DefaultRoute=yes and unprefixed search domains,
# so generic DNS queries race between Mullvad (via router) and rectella's
# office DNS (via tun0) — leaking to the corporate resolver.
(
  for i in $(seq 1 30); do
    if ip link show tun0 &>/dev/null; then
      sleep 1
      "$SCRIPT_DIR/vpn.sh" post-connect 2>&1 | logger -t rectella-vpn-daemon
      exit 0
    fi
    sleep 1
  done
  logger -t rectella-vpn-daemon "tun0 never came up; post-connect skipped"
) &

# Write PID file so vpn-monitor.sh and vpn.sh down can find the session.
# $$ stays the same across exec, so the main PID tracked by systemd is what
# we write here. ExecStopPost=vpn.sh down cleans it up on shutdown.
echo $$ > "$PID_FILE"

# Run openconnect in the FOREGROUND (no --background) so systemd tracks it.
# This process is the main PID — when it exits, systemd restarts.
echo "$VPN_PASS" | exec sudo "$OPENCONNECT" \
  --user="$VPN_USER" \
  --passwd-on-stdin \
  "$VPN_HOST"
