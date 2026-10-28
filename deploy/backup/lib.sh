#!/bin/sh
# Shared setup for backup.sh and restore.sh. Sourced, not run.
#
# Postgres: the standard libpq variables PGHOST, PGPORT, PGUSER, PGPASSWORD,
# PGDATABASE (compose.prod.yml sets them).
# Storage (S3-compatible, configured for rclone through the environment):
#   BACKUP_S3_ENDPOINT           https://<account id>.r2.cloudflarestorage.com
#   BACKUP_S3_ACCESS_KEY_ID      R2 API token's access key id
#   BACKUP_S3_SECRET_ACCESS_KEY  R2 API token's secret
#   BACKUP_S3_BUCKET             bucket name, for example myfantasyprem-backups
#   BACKUP_S3_PROVIDER           Cloudflare (default) or Minio (local tests)
#   BACKUP_S3_PREFIX             folder inside the bucket (default postgres)

set -eu

: "${PGHOST:?PGHOST is not set}"
: "${PGUSER:?PGUSER is not set}"
: "${PGDATABASE:?PGDATABASE is not set}"
: "${BACKUP_S3_ENDPOINT:?BACKUP_S3_ENDPOINT is not set}"
: "${BACKUP_S3_ACCESS_KEY_ID:?BACKUP_S3_ACCESS_KEY_ID is not set}"
: "${BACKUP_S3_SECRET_ACCESS_KEY:?BACKUP_S3_SECRET_ACCESS_KEY is not set}"
: "${BACKUP_S3_BUCKET:?BACKUP_S3_BUCKET is not set}"

# Everything is configured through the environment; an empty config file
# keeps rclone from looking for one.
export RCLONE_CONFIG=/tmp/rclone.conf
: >"$RCLONE_CONFIG"
export RCLONE_CONFIG_REMOTE_TYPE=s3
export RCLONE_CONFIG_REMOTE_PROVIDER="${BACKUP_S3_PROVIDER:-Cloudflare}"
export RCLONE_CONFIG_REMOTE_ENDPOINT="$BACKUP_S3_ENDPOINT"
export RCLONE_CONFIG_REMOTE_ACCESS_KEY_ID="$BACKUP_S3_ACCESS_KEY_ID"
export RCLONE_CONFIG_REMOTE_SECRET_ACCESS_KEY="$BACKUP_S3_SECRET_ACCESS_KEY"
export RCLONE_CONFIG_REMOTE_REGION=auto
# R2 tokens scoped to one bucket cannot create or list buckets.
export RCLONE_CONFIG_REMOTE_NO_CHECK_BUCKET=true
export RCLONE_CONFIG_REMOTE_ENV_AUTH=false

REMOTE="remote:${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX:-postgres}"
LOCAL_DIR="${BACKUP_LOCAL_DIR:-/backups}"
mkdir -p "$LOCAL_DIR"

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
