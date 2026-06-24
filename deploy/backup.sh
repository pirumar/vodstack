#!/usr/bin/env sh
# vodstack backup: Postgres dump + MinIO mirror.
#
# Run from the host (cron) or via: docker compose -f deploy/docker-compose.yml run --rm backup
# Restores: pg_restore the dump; `mc mirror` the objects back.
set -eu

STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${BACKUP_DIR:-/backups}/$STAMP"
mkdir -p "$OUT"

echo "[backup] postgres -> $OUT/vodstack.dump"
PGPASSWORD="${POSTGRES_PASSWORD:-vodstack}" pg_dump \
  -h "${POSTGRES_HOST:-postgres}" -U "${POSTGRES_USER:-vodstack}" \
  -d "${POSTGRES_DB:-vodstack}" -Fc -f "$OUT/vodstack.dump"

echo "[backup] minio bucket -> $OUT/objects"
mc alias set bk "http://${MINIO_HOST:-minio}:9000" \
  "${MINIO_ACCESS_KEY:-minioadmin}" "${MINIO_SECRET_KEY:-minioadmin}" >/dev/null
mc mirror --overwrite "bk/${MINIO_BUCKET:-vodstack-videos}" "$OUT/objects"

echo "[backup] done: $OUT"
