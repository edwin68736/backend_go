#!/usr/bin/env bash
# Migraciones central + tenants activos (post-deploy o mantenimiento).
set -euo pipefail

BASE_DIR="${TUKIFAC_BASE:-/opt/tukifac}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.production.yml}"
CONTAINER="${TUKIFAC_CONTAINER:-tukifac-backend-go}"
CMD="${MIGRATE_CMD:-migrate}"

cd "${BASE_DIR}"

if ! docker ps --format '{{.Names}}' | grep -qx "${CONTAINER}"; then
  echo "ERROR: contenedor ${CONTAINER} no está en ejecución"
  exit 1
fi

echo "==> Ejecutando: ./tukifac-api ${CMD}"
docker compose -f "${COMPOSE_FILE}" exec -T backend-go ./tukifac-api "${CMD}"
