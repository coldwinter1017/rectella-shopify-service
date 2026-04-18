#!/bin/bash

# Wait for PostgreSQL to accept connections (used by systemd ExecStartPre).

for i in $(seq 1 30); do
  if docker exec rectella-shopify-service-postgres-1 pg_isready -U rectella -q 2>/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "PostgreSQL did not become ready in time."
exit 1
