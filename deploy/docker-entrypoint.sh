#!/bin/sh
# Producción: solo arranca la API. migrate-central corre en deploy (antes del restart).
# Desarrollo local: AUTO_MIGRATE_DEV=true en go run / .env (nunca en APP_ENV=production).
set -eu

exec "$@"
