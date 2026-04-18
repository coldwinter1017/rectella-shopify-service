#!/bin/bash

# Backup PostgreSQL database to compressed SQL file.
# Run via systemd timer (rectella-backup.timer) every 6h.
#
# Pings $HC_PING_URL_BACKUP on success, /fail on error (if set in .env).

BACKUP_DIR="/home/bast/backups/rectella"
OFFSITE_DIR="/home/bast/Tresorit/rectella-backups"
RETENTION_DAYS=30

# Load .env for healthcheck ping URL (optional)
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -f "$PROJECT_DIR/.env" ]]; then
  set -a
  . "$PROJECT_DIR/.env"
  set +a
fi
HC_URL="${HC_PING_URL_BACKUP:-}"

ping_hc() {
  [[ -z "$HC_URL" ]] && return 0
  curl -fsS --max-time 10 --retry 3 "${HC_URL}${1}" >/dev/null 2>&1 || true
}

mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_FILE="$BACKUP_DIR/rectella-$TIMESTAMP.sql.gz"

if docker exec rectella-shopify-service-postgres-1 pg_dump -U rectella rectella | gzip > "$BACKUP_FILE"; then
  echo "Backup created: $BACKUP_FILE ($(du -h "$BACKUP_FILE" | cut -f1))"
else
  echo "Backup FAILED" >&2
  ping_hc "/fail"
  exit 1
fi

# Off-site copy via Tresorit (folder syncs to cloud automatically).
if [[ -d "$(dirname "$OFFSITE_DIR")" ]]; then
  mkdir -p "$OFFSITE_DIR"
  if cp "$BACKUP_FILE" "$OFFSITE_DIR/"; then
    echo "Off-site copy: $OFFSITE_DIR/$(basename "$BACKUP_FILE")"
    # Apply same retention to off-site
    find "$OFFSITE_DIR" -name "rectella-*.sql.gz" -mtime +$RETENTION_DAYS -delete
  else
    echo "Off-site copy FAILED" >&2
    ping_hc "/fail"
    exit 1
  fi
else
  echo "WARNING: Tresorit folder not present, skipping off-site copy" >&2
fi

# Remove local backups older than retention period.
find "$BACKUP_DIR" -name "rectella-*.sql.gz" -mtime +$RETENTION_DAYS -delete

ping_hc ""
