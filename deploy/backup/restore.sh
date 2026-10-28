#!/bin/sh
# Restore a backup. By default it is a drill: the dump is restored into a
# scratch database (<db>_restore) next to the live one, checked, and left
# there; the live database is not touched.
#
#   restore.sh --list                  list remote backups, oldest first
#   restore.sh [latest|NAME|/path]     drill: restore into <db>_restore and report row counts
#
# latest is the newest dump that holds data: dumps without any table data
# (taken of an empty database) are skipped.
#   restore.sh [latest|NAME] --replace swap the restored copy in as the live database
#
# --replace needs RESTORE_CONFIRM=yes and the API stopped (deploy.sh
# restore does both). The previous live database is kept, renamed to
# <db>_pre_restore_<timestamp>; drop it by hand once you are happy. The
# swap is one transaction: on failure the live database is unchanged.

. /usr/local/bin/lib.sh

src=latest
replace=false
for arg in "$@"; do
	case "$arg" in
	--list)
		rclone lsl --include '*.dump' "$REMOTE" | sort -k2,3
		exit 0
		;;
	--replace) replace=true ;;
	-*)
		echo "unknown flag $arg" >&2
		exit 2
		;;
	*) src="$arg" ;;
	esac
done

live="$PGDATABASE"
scratch="${live}_restore"
mkdir -p "$LOCAL_DIR/restore"

# 1. Get the dump.
# fetch NAME: download a remote dump and its checksum into $file.
fetch() {
	file="$LOCAL_DIR/restore/$1"
	log "downloading $REMOTE/$1"
	rm -f "$file.sha256"
	rclone copyto "$REMOTE/$1" "$file"
	# copyto of a missing object succeeds without a file: check for it.
	if rclone copyto "$REMOTE/$1.sha256" "$file.sha256" 2>/dev/null && [ -f "$file.sha256" ]; then
		(cd "$LOCAL_DIR/restore" && sha256sum -c "$1.sha256") >&2 || {
			log "checksum mismatch: $1"
			exit 1
		}
	else
		log "warning: no checksum file for $1"
	fi
}
# data_tables: how many tables the dump in $file holds data for.
data_tables() { pg_restore --list "$file" | grep -c 'TABLE DATA' || true; }

case "$src" in
/*) file="$src" ;;
latest)
	file=
	for name in $(rclone lsf --include '*.dump' "$REMOTE" | sort -r); do
		fetch "$name"
		[ "$(data_tables)" -gt 0 ] && break
		log "skipping $name: no table data (a dump of an empty database)"
		file=
	done
	[ -n "$file" ] || {
		log "no backup with data in $REMOTE"
		exit 1
	}
	;;
*) fetch "$src" ;;
esac
pg_restore --list "$file" >/dev/null
[ "$(data_tables)" -gt 0 ] || log "warning: $(basename "$file") holds no table data"

# 2. Restore into a scratch database (never into the live one directly).
psql_admin() { psql -v ON_ERROR_STOP=1 -X -q -d postgres "$@"; }
log "restoring $(basename "$file") into $scratch"
psql_admin -c "DROP DATABASE IF EXISTS \"$scratch\" WITH (FORCE)"
psql_admin -c "CREATE DATABASE \"$scratch\""
pg_restore --exit-on-error --no-owner --no-acl --dbname "$scratch" "$file"

# 3. Check it: every table with its row count, and the migration version.
log "restored; row counts:"
psql -X -v ON_ERROR_STOP=1 -d "$scratch" -P pager=off -c "
SELECT table_name AS \"table\",
       (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', table_schema, table_name), false, true, '')))[1]::text::bigint AS rows
FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
ORDER BY 1"
psql -X -d "$scratch" -At -c "SELECT 'schema version: ' || max(version_id) FROM goose_db_version WHERE is_applied" >&2 || true

if [ "$replace" != true ]; then
	log "drill done: $scratch holds the backup; the live database $live is untouched"
	log "drop it with: DROP DATABASE \"$scratch\""
	exit 0
fi

# 4. Swap: live -> <db>_pre_restore_<ts>, scratch -> live.
[ "${RESTORE_CONFIRM:-}" = yes ] || {
	log "refusing --replace without RESTORE_CONFIRM=yes"
	exit 1
}
old="${live}_pre_restore_$(date -u +%Y%m%d%H%M%S)"
log "replacing $live (kept as $old)"
# allow_connections DB BOOL
allow_connections() { psql_admin -c "ALTER DATABASE \"$1\" WITH ALLOW_CONNECTIONS $2"; }
# Lock both databases first: no new sessions, and wait (up to 10s each)
# until the old ones are gone. A rename holds a lock that blocks every new
# connection (the healthcheck's included) while it waits for other sessions,
# so the swap must find none.
allow_connections "$scratch" false
allow_connections "$live" false
psql_admin -At -c "SELECT pg_terminate_backend(pid, 10000) FROM pg_stat_activity
WHERE datname IN ('$live', '$scratch') AND pid <> pg_backend_pid()" >/dev/null || true
# Both renames in one transaction (ALTER DATABASE ... RENAME may run in
# one): both happen or neither, so there is always a database named $live.
if ! psql_admin -1 -c "ALTER DATABASE \"$live\" RENAME TO \"$old\"" -c "ALTER DATABASE \"$scratch\" RENAME TO \"$live\""; then
	allow_connections "$live" true
	allow_connections "$scratch" true
	log "swap failed and was rolled back: $live is unchanged, the backup is in $scratch"
	exit 1
fi
allow_connections "$live" true
allow_connections "$old" true
log "restore done: $live is now the backup $(basename "$file"); previous data in $old"
