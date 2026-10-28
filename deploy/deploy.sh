#!/usr/bin/env bash
# Operate the production stack (docs/DEPLOY.md). Run on the VM from the repo
# checkout, except `push`, which runs on a laptop.
#
#   deploy.sh up                    start everything (postgres, api, caddy, duckdns)
#   deploy.sh deploy TAG            backup, pull, migrate, roll the API to TAG; auto-rollback if unhealthy
#   deploy.sh rollback              roll the API back to the previous TAG (no migrations)
#   deploy.sh status                containers, release, health, migrations, poller state
#   deploy.sh logs [SERVICE]        follow logs (default api)
#   deploy.sh migrate [ARGS]        migrate up | status | down -to N
#   deploy.sh admin ARGS            cmd/admin (rescore -gw 7, tick, ...); `session` needs --i-know in production
#   deploy.sh web DIST              publish a web build (apps/app/dist copied to the VM) as a new release
#   deploy.sh web-rollback          point the site back at the previous web release
#   deploy.sh migrate-from-dump FILE [--replace]  load a pg_dump (-Fc or plain SQL); needs a deployed release first
#   deploy.sh backup                dump to R2 now
#   deploy.sh restore [ARGS]        restore.sh (drill by default; --replace swaps it live)
#   deploy.sh psql                  psql on the live database
#   deploy.sh build [TAG]           build the API image on this machine (no registry needed)
#   deploy.sh push TAG              build arm64+amd64 and push to API_IMAGE (laptop; docker login ghcr.io first)
#   deploy.sh install-timer         nightly backup at 03:15 UTC via systemd (sudo)
#   deploy.sh down                  stop everything (data stays in the volumes)
#
# Settings come from deploy/.env; the API tag in service is in deploy/.release.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$DIR/.." && pwd)"
ENV_FILE="${ENV_FILE:-$DIR/.env}"
RELEASE="$DIR/.release"
PREVIOUS="$DIR/.release.previous"

die() {
	echo "deploy: $*" >&2
	exit 1
}
say() { echo "==> $*" >&2; }

[ -f "$ENV_FILE" ] || die "$ENV_FILE is missing (cp deploy/.env.example deploy/.env and fill it in)"

# envget KEY: a value from the env file, without surrounding quotes.
envget() { grep -E "^$1=" "$ENV_FILE" | tail -n 1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//'; }
current_tag() { cat "$RELEASE" 2>/dev/null || echo latest; }
api_image() { local i; i="$(envget API_IMAGE)"; echo "${i:-ghcr.io/OWNER/myfantasyprem-api}"; }
dc() { API_TAG="${API_TAG:-$(current_tag)}" docker compose -f "$DIR/compose.prod.yml" --env-file "$ENV_FILE" "$@"; }

# web_root: the directory Caddy serves (WEB_ROOT, relative to deploy/).
web_root() {
	local w
	w="$(envget WEB_ROOT)"
	w="${w:-./web}"
	case "$w" in /*) echo "$w" ;; *) echo "$DIR/${w#./}" ;; esac
}

# ensure_web_root: create the web root and releases/ as the deploy user before
# compose runs. If Docker meets a missing bind-mount source it creates it as
# root, and `deploy.sh web` then cannot write into it.
ensure_web_root() {
	local root
	root="$(web_root)"
	mkdir -p "$root/releases" 2>/dev/null || true
	[ -w "$root" ] && [ -w "$root/releases" ] ||
		die "$root is not writable by $(id -un) (Docker made it as root?): sudo chown -R $(id -un): $root"
}

# sync_caddy: the Caddyfile is a bind-mounted file, so editing it does not
# recreate the container; restart caddy when its hash changed since the last run.
sync_caddy() {
	local f="$DIR/caddy/Caddyfile" stamp="$DIR/.caddyfile.sha" now
	now="$( (sha256sum "$f" 2>/dev/null || shasum -a 256 "$f") | cut -d' ' -f1)"
	if [ "$now" != "$(cat "$stamp" 2>/dev/null || true)" ]; then
		if [ -n "$(dc ps -q caddy 2>/dev/null)" ]; then
			say "Caddyfile changed: restarting caddy"
			dc restart caddy
		fi
		echo "$now" >"$stamp"
	fi
}

# web_lock ROOT: hold an exclusive lock on the whole release switch (the
# current/previous links and pruning) until this shell exits.
web_lock() {
	command -v flock >/dev/null || die "flock is missing (apt-get install util-linux)"
	exec 9>"$1/.lock"
	flock -w 60 9 || die "another web publish holds $1/.lock"
}

# switch_link ROOT NAME TARGET: point ROOT/NAME at TARGET atomically, through
# a temp link no other run shares. Relative targets resolve inside Caddy too.
switch_link() {
	local tmp
	tmp="$1/.$2.$$.tmp"
	ln -sfn "$3" "$tmp"
	mv -Tf "$tmp" "$1/$2"
}

# The tables whose row counts migrate-from-dump prints before and after.
KEY_TABLES="users leagues teams roster_slots matchups transactions trades lineups waiver_claims draft_picks imports league_refs"

# row_counts: "table rows" for KEY_TABLES on the live database (absent ones: -).
row_counts() {
	local t n
	for t in $KEY_TABLES; do
		n=-
		if [ "$(pq_app "SELECT to_regclass('public.$t') IS NOT NULL")" = t ]; then n="$(pq_app "SELECT count(*) FROM public.$t")"; fi
		printf '  %-16s %s
' "$t" "$n"
	done
}

# pq_app SQL: one statement on the app database, unaligned output.
pq_app() {
	dc exec -T postgres psql -U "$(envget POSTGRES_USER)" -d "$(envget POSTGRES_DB)" -v ON_ERROR_STOP=1 -Atc "$1"
}

backups_configured() { [ -n "$(envget BACKUP_S3_ACCESS_KEY_ID)" ] && [ -n "$(envget BACKUP_S3_BUCKET)" ]; }

# wait_healthy SECONDS: wait for the api container's healthcheck to pass.
wait_healthy() {
	local deadline=$((SECONDS + $1)) id status
	while [ $SECONDS -lt $deadline ]; do
		id="$(dc ps -q api)"
		status="$(docker inspect -f '{{.State.Health.Status}}' "$id" 2>/dev/null || echo missing)"
		case "$status" in
		healthy) return 0 ;;
		unhealthy) break ;;
		esac
		sleep 2
	done
	echo "api health: ${status:-unknown}" >&2
	return 1
}

# roll_to TAG: recreate only the api container on TAG and wait for health.
# Callers run it inside `if`/`||`, where set -e is off: check every step.
roll_to() {
	local want running
	want="$(api_image):$1"
	say "starting api $want"
	API_TAG="$1" dc up -d --no-deps api || {
		echo "docker compose up failed for $want" >&2
		return 1
	}
	API_TAG="$1" wait_healthy 120 || return 1
	running="$(docker inspect -f '{{.Config.Image}}' "$(API_TAG="$1" dc ps -q api)" 2>/dev/null)" || running=unknown
	[ "$running" = "$want" ] || {
		echo "api runs $running, not $want" >&2
		return 1
	}
}

pull_or_check() {
	local tag="$1" image
	image="$(api_image)"
	if [[ "$image" == */* ]]; then
		say "pulling $image:$tag"
		API_TAG="$tag" dc pull api
	else
		docker image inspect "$image:$tag" >/dev/null 2>&1 || die "local image $image:$tag not found (deploy.sh build $tag)"
	fi
}

cmd="${1:-help}"
shift || true
case "$cmd" in
up)
	# The API needs its tables (River's included) before it starts.
	ensure_web_root
	dc up -d postgres
	dc run --rm migrate up
	dc up -d
	sync_caddy
	wait_healthy 120 && say "api healthy"
	;;
down) dc down ;;
deploy)
	tag="${1:-}"
	[ -n "$tag" ] || die "usage: deploy.sh deploy TAG"
	old="$(current_tag)"
	pull_or_check "$tag"
	ensure_web_root
	dc up -d postgres
	if backups_configured && [ "${SKIP_BACKUP:-}" != 1 ]; then
		say "pre-deploy backup"
		BACKUP_TAG=predeploy dc run --rm backup backup.sh
	else
		say "skipping pre-deploy backup (backups not configured or SKIP_BACKUP=1)"
	fi
	say "migrating with $tag"
	API_TAG="$tag" dc run --rm migrate up
	if roll_to "$tag"; then
		if [ -f "$RELEASE" ] && [ "$old" != "$tag" ]; then echo "$old" >"$PREVIOUS"; fi
		echo "$tag" >"$RELEASE"
		say "deployed $tag (previous: $old)"
		sync_caddy
	else
		say "api $tag did not come up healthy; rolling back to $old"
		dc logs --tail 50 api >&2 || true
		roll_to "$old" || die "rollback to $old is unhealthy too: check deploy.sh logs"
		die "deploy of $tag failed; still on $old. Migrations it applied stay applied."
	fi
	;;
rollback)
	[ -f "$PREVIOUS" ] || die "no previous release recorded"
	prev="$(cat "$PREVIOUS")"
	cur="$(current_tag)"
	pull_or_check "$prev"
	roll_to "$prev" || die "api $prev is unhealthy"
	echo "$prev" >"$RELEASE"
	echo "$cur" >"$PREVIOUS"
	say "rolled back to $prev (deploy.sh rollback again returns to $cur)"
	;;
status)
	echo "release: $(api_image):$(current_tag) (previous: $(cat "$PREVIOUS" 2>/dev/null || echo none))"
	dc ps
	port="$(envget API_HOST_PORT)"
	echo -n "healthz: "
	curl -fsS "http://127.0.0.1:${port:-8080}/healthz" || echo "DOWN"
	echo
	dc run --rm migrate status | tail -n 5 || true
	dc exec -T postgres psql -U "$(envget POSTGRES_USER)" -d "$(envget POSTGRES_DB)" -c \
		"SELECT mode, gameweek, last_good_at, next_poll_at, consecutive_failures, left(last_error, 80) AS last_error FROM poll_state" || true
	;;
logs) dc logs -f --tail 200 "${1:-api}" ;;
migrate) dc run --rm migrate "${@:-up}" ;;
admin)
	# `admin session` mints a signed-in session for any email: on production
	# it is the commissioner's first sign-in without email, nothing else.
	if [ "${1:-}" = session ]; then
		args=() iknow=no
		for a in "$@"; do
			if [ "$a" = --i-know ]; then iknow=yes; else args+=("$a"); fi
		done
		app_env="$(envget APP_ENV)"
		if [ "${app_env:-production}" = production ] && [ "$iknow" != yes ]; then
			die "admin session prints a live sign-in for any email; on production pass --i-know (docs/DEPLOY.md, first commissioner login)"
		fi
		# On production never create an account for a mistyped email.
		[ "${app_env:-production}" != production ] || args+=(-existing)
		set -- "${args[@]}"
	fi
	dc run --rm admin "$@"
	;;
web)
	src="${1:-}"
	[ -n "$src" ] && [ -f "$src/index.html" ] && [ -d "$src/_expo" ] ||
		die "usage: deploy.sh web DIST (an \`npm run build:web\` output: index.html and _expo/)"
	root="$(web_root)"
	app_url="$(envget APP_URL)"
	# A build for another origin would call the wrong API (EXPO_PUBLIC_API_BASE_URL).
	if [ -n "$app_url" ] && ! grep -rqF "$app_url" "$src/_expo"; then
		[ "${FORCE:-}" = 1 ] || die "$src does not mention $app_url: build it with EXPO_PUBLIC_API_BASE_URL=$app_url (FORCE=1 skips this check)"
	fi
	rel="$(date -u +%Y%m%dT%H%M%SZ)"
	ensure_web_root
	say "copying $src to $root/releases/$rel"
	cp -R "$src" "$root/releases/$rel.tmp"
	mv "$root/releases/$rel.tmp" "$root/releases/$rel"
	chmod -R a+rX "$root/releases/$rel"
	# Hashed files of every release go to shared/ (Caddy serves them from there),
	# so a tab on an older bundle still finds its files after a switch.
	mkdir -p "$root/shared"
	for d in _expo/static assets; do
		if [ -d "$root/releases/$rel/$d" ]; then
			mkdir -p "$root/shared/$d"
			cp -Rn "$root/releases/$rel/$d/." "$root/shared/$d/" 2>/dev/null || true
			find "$root/releases/$rel/$d" -type f | while read -r f; do touch "$root/shared/${f#"$root/releases/$rel/"}" 2>/dev/null || true; done
		fi
	done
	chmod -R a+rX "$root/shared"
	web_lock "$root"
	old="$(readlink "$root/current" 2>/dev/null || true)"
	[ -z "$old" ] || switch_link "$root" previous "$old"
	switch_link "$root" current "releases/$rel"
	say "web: current -> releases/$rel (previous: ${old:-none})"
	# Keep the newest 5 releases and whatever current and previous point at;
	# hashed files not seen in a publish for 60 days leave shared/.
	keep="$(readlink "$root/previous" 2>/dev/null || true)"
	find "$root/releases" -mindepth 1 -maxdepth 1 -type d -name '20*' | sort -r | tail -n +6 | while read -r d; do
		[ "releases/$(basename "$d")" = "$keep" ] || rm -rf "$d"
	done
	find "$root/shared" -type f -mtime +60 -delete 2>/dev/null || true
	;;
web-rollback)
	root="$(web_root)"
	ensure_web_root
	web_lock "$root"
	prev="$(readlink "$root/previous" 2>/dev/null)" || die "no previous web release recorded"
	[ -d "$root/$prev" ] || die "$root/$prev is gone"
	cur="$(readlink "$root/current")"
	switch_link "$root" current "$prev"
	switch_link "$root" previous "$cur"
	say "web: current -> $prev (web-rollback again returns to $cur)"
	;;
migrate-from-dump)
	file="" replace=no
	for a in "$@"; do
		case "$a" in
		--replace) replace=yes ;;
		*) file="$a" ;;
		esac
	done
	[ -n "$file" ] && [ -f "$file" ] || die "usage: deploy.sh migrate-from-dump FILE [--replace] (pg_dump -Fc output, or plain SQL)"
	# Its migrations run from the API image in service: deploy first.
	[ -s "$RELEASE" ] || die "no API release yet ($RELEASE is missing): run deploy.sh deploy TAG first, then this (docs/DEPLOY.md step 8)"
	dc up -d postgres
	for _ in $(seq 1 30); do pq_app "SELECT 1" >/dev/null 2>&1 && break || sleep 2; done
	tables="$(pq_app "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")"
	say "before:"
	row_counts >&2 || true
	if [ "$tables" != 0 ]; then
		[ "$replace" = yes ] || die "the database already has $tables tables; --replace drops them all (take deploy.sh backup first)"
		if backups_configured && [ "${SKIP_BACKUP:-}" != 1 ]; then
			say "backup before replacing"
			BACKUP_TAG=predump dc run --rm backup backup.sh
		fi
		say "stopping api; dropping schema public"
		dc stop api || true
		pq_app "DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION CURRENT_USER;" >/dev/null
	fi
	user="$(envget POSTGRES_USER)" db="$(envget POSTGRES_DB)"
	if [ "$(head -c 5 "$file")" = PGDMP ]; then
		say "pg_restore (custom format) from $file"
		dc exec -T postgres pg_restore --no-owner --no-acl --exit-on-error -U "$user" -d "$db" <"$file"
	else
		say "psql (plain SQL) from $file"
		dc exec -T postgres psql -q -v ON_ERROR_STOP=1 -U "$user" -d "$db" <"$file" >/dev/null
	fi
	say "migrating to this release's schema ($(current_tag))"
	dc run --rm migrate up
	say "after:"
	row_counts >&2
	say "loaded $file. Start the API again with deploy.sh up (it was stopped if --replace was used)"
	;;
seed) die "seed loads the fictional demo world; never on the production database" ;;
backup) dc run --rm backup backup.sh ;;
restore)
	if [[ " $* " == *" --replace "* ]]; then
		say "stopping api for the restore"
		dc stop api
		RESTORE_CONFIRM=yes dc run --rm backup restore.sh "$@" || {
			# Report what is there rather than assume. restore.sh swaps in one
			# transaction, but a kill mid-swap can leave $db closed to sessions.
			db="$(envget POSTGRES_DB)"
			db="${db:-myfantasyprem}"
			pq() { dc exec -T postgres psql -U "$(envget POSTGRES_USER)" -d postgres -v ON_ERROR_STOP=1 -Atc "$1"; }
			if [ "$(pq "SELECT count(*) FROM pg_database WHERE datname = '$db'")" = 1 ]; then
				pq "ALTER DATABASE \"$db\" WITH ALLOW_CONNECTIONS true" >/dev/null
				dc start api
				die "restore failed; the live database $db is unchanged (see the log above)"
			fi
			pq "SELECT datname FROM pg_database WHERE datname LIKE '$db%' ORDER BY 1" >&2 || true
			die "restore failed and there is NO database $db; api left stopped. Rename the newest ${db}_pre_restore_* back to $db"
		}
		dc start api
		wait_healthy 120 && say "api healthy on the restored database"
	else
		dc run --rm backup restore.sh "$@"
	fi
	;;
psql) dc exec postgres psql -U "$(envget POSTGRES_USER)" -d "$(envget POSTGRES_DB)" ;;
build)
	tag="${1:-$(git -C "$REPO" describe --tags --always --dirty)}"
	say "building $(api_image):$tag"
	docker build --build-arg VERSION="$tag" -t "$(api_image):$tag" "$REPO/server"
	echo "$tag"
	;;
push)
	tag="${1:-}"
	[ -n "$tag" ] || die "usage: deploy.sh push TAG"
	docker buildx build --platform linux/arm64,linux/amd64 --build-arg VERSION="$tag" \
		-t "$(api_image):$tag" --push "$REPO/server"
	;;
install-timer)
	unit=/etc/systemd/system/myfantasyprem-backup
	sudo tee "$unit.service" >/dev/null <<EOF
[Unit]
Description=MyFantasyPrem nightly Postgres backup to R2
Wants=docker.service
After=docker.service

[Service]
Type=oneshot
User=$(id -un)
ExecStart=$DIR/deploy.sh backup
EOF
	sudo tee "$unit.timer" >/dev/null <<EOF
[Unit]
Description=Run the MyFantasyPrem backup nightly

[Timer]
OnCalendar=*-*-* 03:15:00 UTC
RandomizedDelaySec=10m
Persistent=true

[Install]
WantedBy=timers.target
EOF
	sudo systemctl daemon-reload
	sudo systemctl enable --now myfantasyprem-backup.timer
	systemctl list-timers myfantasyprem-backup.timer --no-pager
	;;
help | -h | --help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//' ;;
*) die "unknown command $cmd (deploy.sh help)" ;;
esac
