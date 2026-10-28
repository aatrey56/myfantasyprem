#!/bin/sh
# Dump the database (pg_dump custom format, compressed), check the dump
# reads back, upload it with a checksum, then prune old backups.
#
#   backup.sh          (run by `deploy.sh backup` and the nightly timer)
#
# Environment: see lib.sh, plus
#   BACKUP_RETENTION_DAYS  delete remote dumps older than this (default 30)
#   BACKUP_LOCAL_KEEP_DAYS keep local copies this long (default 3)
#   BACKUP_HEARTBEAT_URL   optional; pinged on success, <url>/fail on failure
#                          (a Healthchecks.io check, for example)
#   BACKUP_TAG             optional label in the file name (e.g. predeploy)
#
# A database with no tables (a new VM before its restore) is not backed up:
# that dump would become the newest and `restore.sh latest` would pick it.
# No heartbeat ping then, so a nightly run against an empty database alerts.

. /usr/local/bin/lib.sh

fail() {
	log "backup FAILED: $*"
	[ -n "${BACKUP_HEARTBEAT_URL:-}" ] && wget -q -T 10 -O /dev/null "${BACKUP_HEARTBEAT_URL%/}/fail" || true
	exit 1
}
trap 'fail "unexpected error (line $LINENO)"' HUP INT TERM

stamp=$(date -u +%Y%m%dT%H%M%SZ)
name="${PGDATABASE}-${stamp}${BACKUP_TAG:+-$BACKUP_TAG}.dump"
file="$LOCAL_DIR/$name"

tables=$(psql -X -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p') AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%'") || fail "count tables"
if [ "$tables" = 0 ]; then
	log "skipped: $PGDATABASE has no tables (not initialised), nothing to back up"
	exit 0
fi

log "dumping $PGDATABASE on $PGHOST to $file"
pg_dump --format=custom --compress=9 --no-owner --no-acl --file "$file.partial" || fail "pg_dump"
pg_restore --list "$file.partial" >/dev/null || fail "dump does not read back"
mv "$file.partial" "$file"
(cd "$LOCAL_DIR" && sha256sum "$name" >"$name.sha256")
size=$(wc -c <"$file" | tr -d ' ')
log "dump ok: $size bytes, $(pg_restore --list "$file" | grep -c 'TABLE DATA') tables"

log "uploading to $REMOTE/"
rclone copyto --s3-no-check-bucket "$file" "$REMOTE/$name" || fail "upload"
rclone copyto --s3-no-check-bucket "$file.sha256" "$REMOTE/$name.sha256" || fail "upload checksum"
remote_size=$(rclone lsf --format s "$REMOTE/$name") || fail "remote listing"
[ "$remote_size" = "$size" ] || fail "remote size $remote_size != local $size"
log "upload ok: $REMOTE/$name"

days="${BACKUP_RETENTION_DAYS:-30}"
log "pruning remote backups older than ${days}d"
rclone delete --min-age "${days}d" --include '*.dump' --include '*.dump.sha256' "$REMOTE" || log "warning: remote prune failed"
find "$LOCAL_DIR" -maxdepth 1 -name '*.dump*' -mtime "+${BACKUP_LOCAL_KEEP_DAYS:-3}" -delete || true

if [ -n "${BACKUP_HEARTBEAT_URL:-}" ]; then
	wget -q -T 10 -O /dev/null "$BACKUP_HEARTBEAT_URL" || log "warning: heartbeat ping failed"
fi
log "backup done: $name"
echo "$name"
