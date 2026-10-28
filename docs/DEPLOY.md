# Deploy

Production for about a dozen players. **Current:** one Oracle Always Free
VM with Caddy and DuckDNS, $0 (the next section). The older setup (Hetzner
VPS, Cloudflare Tunnel and Pages) follows it. Follow the steps in order;
each ends with a check. Day-to-day operations are in [RUNBOOK.md](RUNBOOK.md).

> **Email sign-in** is optional. The API rate limits `POST /v1/auth/email`
> per IP and per address, so `EMAIL_PROVIDER=resend` is allowed in
> production once Resend is set up (step 6 of the older setup);
> `EMAIL_PROVIDER=none` runs without it.

## Oracle + Caddy + DuckDNS (current)

The production setup for the owner's league (about 12 players, $0): one
Oracle Always Free Ampere A1 VM, Docker Compose, Caddy with automatic HTTPS
on a DuckDNS name, everything on **one origin**.

```
 browser ── https://harvestfc.duckdns.org ──> Oracle A1 VM (Ubuntu 24.04 arm64, 2 OCPU / 12 GB)
                                                caddy :80/:443 (Let's Encrypt, security headers, CSP)
                                                  /                 -> web/current (static Expo web build)
                                                  /v1/* (wss too), /updates/*, /healthz -> api:8080
                                                api (Go) ──> postgres 17 (volume pgdata)
                                                duckdns: refreshes the A record every 5 minutes
                                                nightly timer: deploy.sh backup ──> R2 (off-site)
```

Settings: `APP_ENV=production`, `EMAIL_PROVIDER=none` (sign-in through
claim links and, for the commissioner, `admin session`), `BRANDED_ASSETS=true`
(private league), web build with `EXPO_PUBLIC_API_BASE_URL=https://<DOMAIN>`.
The commands below use `harvestfc.duckdns.org`; substitute yours.

### 1. DuckDNS name

1. Sign in at duckdns.org (GitHub or Google login). Add the subdomain
   `harvestfc`. Copy the **token** at the top of the page.
2. Leave the IP for now; the `duckdns` service sets it from the VM.

### 2. Create the VM

1. Oracle Cloud sign-up and home region: see "Alternative: Oracle Cloud
   Always Free" below, steps 1-2 (shape **VM.Standard.A1.Flex, 2 OCPU,
   12 GB**, image **Canonical Ubuntu 24.04**, public IPv4, your SSH key,
   100 GB boot volume).
2. **Networking > Virtual cloud networks > your VCN > Security Lists >
   Default > Add Ingress Rules**, source `0.0.0.0/0`:
   TCP `80`, TCP `443`, UDP `443`. (Port 22 is already there.)
3. Check: `ssh ubuntu@<public ip>` logs you in.

### 3. Bootstrap

```bash
ssh ubuntu@<public ip>
sudo apt-get update && sudo apt-get install -y git   # the bootstrap script comes from the repo
git clone https://github.com/aatrey56/myfantasyprem.git ~/myfantasyprem && cd ~/myfantasyprem
bash deploy/bootstrap-vm.sh
exit   # log out and back in for the docker group
```

The clone is anonymous, so the repo must be public (it will be; nothing
secret lives in it: `deploy/.env` is git-ignored). For a private repo use a
read-only deploy key instead.

It installs Docker, opens 80/443 in the VM firewall (on Oracle's image it
inserts ACCEPT rules before the REJECT in `/etc/iptables/rules.v4` and saves
them with `netfilter-persistent`), turns off SSH passwords and root login,
enables fail2ban and security updates, adds swap and checks NTP. It **exits
1** if `~/.ssh/authorized_keys` is empty, `sshd -t` fails, or the clock is
not synchronised: fix and run it again (it is idempotent).

What actually keeps the rest of the stack private is not the host firewall
(Docker publishes ports through its own iptables chains, which bypass ufw and
the INPUT rules) but two things: the **OCI security list** from step 2, which
admits only 22, 80 and 443, and the compose file, where only Caddy publishes
on all interfaces. Postgres publishes nothing; the API and metrics bind
`127.0.0.1` only. Never add a `ports:` line without the `127.0.0.1:` prefix.

Check: `sudo iptables -L INPUT -n --line-numbers` lists the 80/443 ACCEPTs
above the REJECT; `timedatectl` says `System clock synchronized: yes`.

### 4. Pre-flight: can this VM reach FPL?

Some cloud IP ranges are blocked upstream. From the VM (by hand, once):

```bash
curl -sS -o /dev/null -w '%{http_code}\n' -A 'MyFantasyPrem-stats/1.0 (+mailto:you@example.com)' \
  https://draft.premierleague.com/api/bootstrap-static
```

`200` is good. `403` or a timeout: the poller and imports will fail from
this VM; stop and raise it before going further (RUNBOOK "FPL API down or
blocked").

### 5. `deploy/.env`

```bash
cd ~/myfantasyprem
cp deploy/.env.example deploy/.env && chmod 600 deploy/.env
openssl rand -hex 24   # POSTGRES_PASSWORD
nano deploy/.env
```

Fill in: `DOMAIN`, `DUCKDNS_DOMAIN` (`harvestfc`), `DUCKDNS_TOKEN`,
`POSTGRES_PASSWORD`, `APP_URL` / `CORS_ALLOWED_ORIGINS` (`https://DOMAIN`),
`REALTIME_URL` (`wss://DOMAIN/v1/realtime`), `UPDATES_PUBLIC_URL`
(`https://DOMAIN`), `FPL_USER_AGENT` (a real contact), the `BACKUP_S3_*`
settings (R2, step 5 of the older guide below; optional on day one but do
it the same week), `API_IMAGE`. Keep `COMPOSE_PROFILES=duckdns`,
`APP_ENV=production`, `EMAIL_PROVIDER=none`, `BRANDED_ASSETS=true`,
`PG_MAJOR=17`, and the network values (`MFP_SUBNET`, `MFP_IP_RANGE`,
`CADDY_IP`): the API trusts `X-Forwarded-For` only from `CADDY_IP`, so
per-IP rate limits see real clients. The compose network is pinned to
`172.30.0.0/24` (Caddy at `172.30.0.10`, dynamic addresses from
`172.30.0.128/25`). If that collides with your VCN or a VPN (`docker network
create` fails with "Pool overlaps", or the VM cannot reach a 172.30.x host),
pick another private /24 and change all three together, e.g.
`MFP_SUBNET=10.77.0.0/24`, `MFP_IP_RANGE=10.77.0.128/25`, `CADDY_IP=10.77.0.10`
(`CADDY_IP` inside the subnet, outside the range), then `deploy.sh down` and
`deploy.sh up`. `FPL_RESYNC_DISABLED_LEAGUES` stays
empty unless the owner wants "Re-sync from FPL" off for a league.

Check, from `~/myfantasyprem`:
`docker compose -f deploy/compose.prod.yml --env-file deploy/.env config >/dev/null`
prints nothing.

### 6. API image

Either build on the VM (arm64, about 3 minutes; no registry):

```bash
cd ~/myfantasyprem
# deploy/.env: API_IMAGE=myfantasyprem-api
deploy/deploy.sh build v1
```

or push from the laptop (`deploy.sh push v1`, see "7. API image" in the older setup below).

### 7. First deploy

```bash
cd ~/myfantasyprem
deploy/deploy.sh deploy v1     # backup (if R2 is set), migrate, start api; rolls back if unhealthy
deploy/deploy.sh up            # also starts caddy and duckdns
deploy/deploy.sh status
deploy/deploy.sh logs duckdns  # "duckdns: OK" within a minute
deploy/deploy.sh logs caddy    # "certificate obtained successfully" for DOMAIN
```

`deploy.sh` creates `deploy/web/releases/` as your user before starting
anything: if Docker ever makes `deploy/web` first it is owned by root and
`deploy.sh web` fails with "permission denied"; fix with `sudo chown -R
$USER: deploy/web`.

Check: `dig +short harvestfc.duckdns.org` is the VM's IP;
`curl -sS https://harvestfc.duckdns.org/healthz` answers
`{"status":"ok",...}` (it pings Postgres, at most once every 5 seconds;
`503 unavailable` means the database does not answer). Caddy needs port 80
reachable for the certificate: if it keeps failing, re-check step 2.2 and
the iptables rules.

### 8. Data: the owner's league from the Mac

Skip for a fresh league. Needs step 7 first: the load runs this release's
migrations from the API image in service, and `migrate-from-dump` refuses
without a recorded release. **Freeze writes first**: stop using the league
on the Mac (and close its API) before the dump, since anything changed after
it is lost. To move the current database (Postgres on the owner's Mac):

```bash
# Mac: custom format, without owners or grants (roles differ on the VM).
# pg_dump must not be newer than the VM's Postgres 17 (pg_dump --version).
pg_dump -Fc --no-owner --no-acl -d myfantasyprem -f mfp.dump
scp mfp.dump ubuntu@<public ip>:~/
# VM:
cd ~/myfantasyprem
deploy/deploy.sh migrate-from-dump ~/mfp.dump --replace
deploy/deploy.sh up            # the load stopped the API; start it again
```

Step 7 already created the (empty) tables, so `--replace` is needed: it
backs up first when R2 is set, stops the API, drops the schema, restores,
runs this release's migrations and prints the row counts of the key tables
(users, leagues, teams, rosters, matchups, transactions, ...) before and
after. Compare "after" with the same counts on the Mac:
`psql -d myfantasyprem -c 'SELECT count(*) FROM users'` and so on.
Delete `~/mfp.dump` from the VM afterwards.

### 9. Web app

On the laptop, from the repo root (`MyFantasyPrem/`):

```bash
EXPO_PUBLIC_API_BASE_URL=https://harvestfc.duckdns.org EXPO_PUBLIC_BRANDED_ASSETS=true npm run build:web
npm run verify:prod-bundle
rsync -a --delete apps/app/dist/ ubuntu@<public ip>:~/web-upload/
ssh ubuntu@<public ip> 'cd ~/myfantasyprem && deploy/deploy.sh web ~/web-upload'
```

`deploy.sh web` copies the build into `deploy/web/releases/<UTC time>/`,
adds its hashed files (`_expo/static`, `assets`) to `deploy/web/shared/`,
flips the `current` symlink under a lock (atomic; Caddy serves it at once),
keeps the previous one for `deploy.sh web-rollback`, keeps 5 releases, and
refuses a build that does not contain `APP_URL` (a wrong
`EXPO_PUBLIC_API_BASE_URL`). Caddy serves hashed files from `shared/`, so a
browser tab still on the old bundle keeps working after a switch; a hashed
URL that does not exist is a 404, never `index.html`. Files in `shared/`
not seen in a publish for 60 days are deleted.

Check: open `https://harvestfc.duckdns.org`; devtools show no CSP errors
(the CSP is in `deploy/caddy/Caddyfile`), and the realtime socket
(`wss://.../v1/realtime`) connects once signed in.

### 10. First commissioner login (no email)

With `EMAIL_PROVIDER=none` there are no magic links. Mint a session for the
commissioner's account on the VM (`--i-know`: it prints a live sign-in for
any email, so production refuses it otherwise). **Check the commissioner's
exact email first** (`deploy/deploy.sh psql`, then `SELECT email FROM users;`):
in production `deploy.sh` adds `-existing`, so an unknown or mistyped email
is refused rather than silently creating a new account (and a session for it).

```bash
cd ~/myfantasyprem
deploy/deploy.sh admin session -email owner@example.com --i-know
```

It prints one JSON line (the app's stored-session shape). In the browser,
open `https://harvestfc.duckdns.org`, then the devtools console:

```js
localStorage.setItem('mfp.session', '<the JSON line>');
location.reload();
```

The web app keeps its session in `localStorage` under `mfp.session`
(`apps/app/src/auth/session-store.web.ts`, key in `session.ts`). Clear the
terminal scrollback afterwards. Everyone else joins through their seat's claim
link from the commissioner, or a sign-in link the commissioner issues for
a claimed team.

### 11. Backups off-site and the restore drill

```bash
cd ~/myfantasyprem
deploy/deploy.sh backup            # once by hand: uploads to R2
deploy/deploy.sh install-timer     # nightly at 03:15 UTC
deploy/deploy.sh restore latest    # drill: restores into myfantasyprem_restore, prints row counts
deploy/deploy.sh psql              # DROP DATABASE myfantasyprem_restore;
```

Do the drill the first week and monthly after ("14. Backups and the restore
drill" below has the details and the real `--replace` restore).

### 12. Uptime monitors (UptimeRobot, free)

1. HTTP(s) `https://harvestfc.duckdns.org/healthz` (API and database).
2. Keyword `https://harvestfc.duckdns.org/v1/gameweeks/current`, alert when
   `"stale":true` **exists** (poller failing).
3. HTTP(s) `https://harvestfc.duckdns.org/` (Caddy and the web app).
4. Healthchecks.io: daily check, 2 h grace, its URL in
   `BACKUP_HEARTBEAT_URL`.

### 13. Updating and rollback

```bash
cd ~/myfantasyprem
git pull && deploy/deploy.sh build v2 && deploy/deploy.sh deploy v2   # API (auto-rollback if unhealthy)
deploy/deploy.sh rollback                                               # API back to the previous tag
deploy/deploy.sh web ~/web-upload                                       # a new web build
deploy/deploy.sh web-rollback                                           # web back to the previous build
```

Migrations are not rolled back (`deploy.sh rollback` keeps the schema);
restore a backup for a bad migration. Caddy config changes: the Caddyfile
is a bind-mounted file, so editing it does **not** recreate the container.
`deploy.sh up` and `deploy.sh deploy` restart caddy when the Caddyfile's hash
differs from the last run; by hand:
`docker compose -f deploy/compose.prod.yml --env-file deploy/.env restart caddy`
(from `~/myfantasyprem`). Keep the `caddy_data` volume: it holds
the certificates, and Let's Encrypt rate-limits re-issuing.

---

The rest of this guide is the older setup (Hetzner VPS, Cloudflare Tunnel
for the API, Cloudflare Pages for the web app). The `cloudflared` service
is still in `compose.prod.yml` behind the `tunnel` profile.

## Older setup: Hetzner + Cloudflare Tunnel + Pages

```
 browser ── https://app.example.com ──> Cloudflare Pages (static SPA, _headers CSP)
    │
    └──── https/wss://api.example.com ──> Cloudflare edge ──(Tunnel, outbound only)──┐
                                                                                     │
  Hetzner CAX11 VPS (Ubuntu 24.04, ARM, Docker Compose; Hetzner firewall: SSH only) │
    cloudflared ────────────────────────────────────────────────────────────────────┘
    api (Go: HTTP, realtime, River jobs, stats poller) ──> postgres 17 (volume pgdata)
    nightly timer: deploy.sh backup ──> pg_dump ──> Cloudflare R2 (30 days)
  Email: Resend (magic links)       Monitoring: UptimeRobot + Healthchecks.io
```

Replace `example.com` with your domain throughout. Files: `deploy/`
(compose, scripts, config), `server/Dockerfile`,
`scripts/pages-files.mjs`.

## What costs money

| Item                                    | Cost          | Card needed?                                       |
| --------------------------------------- | ------------- | -------------------------------------------------- |
| Domain on Cloudflare Registrar          | **~$10/year** | yes                                                |
| Hetzner CAX11 + IPv4 + Hetzner backups  | **~€7/month** | yes                                                |
| _or_ Oracle Cloud Always Free VM        | $0            | yes (identity check; see "Oracle and idle VMs")    |
| Cloudflare DNS, Tunnel, Pages           | $0            | no                                                 |
| Cloudflare R2 (10 GB-month free)        | $0            | yes, R2 asks for a payment method before enabling  |
| Resend (3,000 emails/month, 100/day)    | $0            | no                                                 |
| GitHub Actions + GHCR                   | $0            | no (public repos free; private within the minutes) |
| UptimeRobot, Healthchecks.io free plans | $0            | no                                                 |

The Hetzner figure is the owner's October 2026 estimate for a CAX11 in
Germany with its primary IPv4 and backups (20% of the server price);
Hetzner shows the exact monthly total before you create the server.
Free-tier limits checked in October 2026: R2 10 GB-month storage, 1M Class A and
10M Class B operations, free egress ([R2 pricing][r2-pricing]); Resend
3,000 emails a month, 100 a day, 3 domains ([Resend pricing][resend]);
UptimeRobot 50 monitors at 5-minute intervals, no commercial use
([UptimeRobot][uptimerobot]); Healthchecks.io 20 checks ([pricing][hc]).

## 0. Before you start

On your laptop: `git`, `ssh`, Node 24 (`.nvmrc`) and this repo. Docker is
only needed to build images locally. Make an SSH key if you have none:
`ssh-keygen -t ed25519` (it writes `~/.ssh/id_ed25519.pub`, the public half
you upload below).

Pick the names now:

| Name           | Example                                   |
| -------------- | ----------------------------------------- |
| Web app        | `app.example.com`                         |
| API            | `api.example.com`                         |
| Sender address | `login@example.com`                       |
| R2 bucket      | `myfantasyprem-backups`                   |
| Pages project  | `myfantasyprem`                           |
| API image      | `ghcr.io/<github user>/myfantasyprem-api` |

## 1. Domain

1. Create a Cloudflare account (free). **Domain Registration > Register
   Domains**, buy the domain (**~$10/year**). It lands on Cloudflare DNS
   automatically.
2. Check: **Websites** lists the domain as Active.

## 2. Hetzner Cloud server

A CAX11 is 2 Ampere (ARM) vCPUs, 4 GB RAM and 40 GB disk: enough for
Postgres, the API and cloudflared at this size. `deploy/postgres/postgresql.conf`
(1 GB shared_buffers, 3 GB effective_cache_size) is PGTune's web profile for
4 GB of RAM, and `bootstrap-vm.sh` adds 2 GB of swap. The compose kit is the
same on any host.

1. Sign up at hetzner.com/cloud (identity check and a payment method), then
   **Cloud Console > + New project** `myfantasyprem`.
2. **SSH key:** project > **Security > SSH keys > Add SSH key**: paste
   `~/.ssh/id_ed25519.pub`, name it after your laptop, tick **Set as
   default key**.
3. **Firewall:** project > **Firewalls > Create Firewall**, name
   `ssh-only`:
   - Inbound: keep only **TCP 22** (source "Any IPv4" and "Any IPv6", or
     your own address if it is fixed). Delete the ICMP rule if you like; add
     nothing else. The tunnel dials out, so no web ports.
   - Outbound: leave empty (Hetzner then allows all outbound).
   - **Apply to**: you can pick the server after step 4, or select it in the
     create dialog. It filters before traffic reaches the VM, so nothing
     inside (Docker's iptables rules included) can open a port by mistake.
4. **Server:** project > **Servers > Add Server**:
   - Location: **Falkenstein** or **Nuremberg** (Germany).
   - Image: **Ubuntu 24.04**.
   - Type: **Shared vCPU > Arm64 (Ampere) > CAX11**.
   - Networking: **Public IPv4** on (GHCR and Docker Hub pulls need it) and
     IPv6.
   - SSH keys: the key you added above. Firewalls: `ssh-only`.
   - **Backups**: tick it (or step 5). Name: `myfantasyprem`. **Create & Buy
     now**; the summary shows the monthly price.
5. **Backups:** server > **Backups > Enable backups** if not ticked. Hetzner
   keeps 7 daily whole-disk images (+20% of the server price). They are
   crash-consistent images in the same account, so they complement the
   nightly `pg_dump` to R2 (step 14) but do not replace it: restore Postgres
   from R2, and use a Hetzner backup to roll back the whole machine.
6. **Login user.** Hetzner images log in as `root`, and `bootstrap-vm.sh`
   turns root SSH off, so make a sudo user first:

   ```bash
   ssh root@<server ip>
   adduser deploy                      # pick a password: sudo asks for it
   usermod -aG sudo deploy
   install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
   install -m 600 -o deploy -g deploy /root/.ssh/authorized_keys /home/deploy/.ssh/authorized_keys
   exit
   ```

7. Check: `ssh deploy@<server ip>` logs you in and `sudo true` works;
   Firewalls > `ssh-only` lists the server under **Applied to**.

## 3. Bootstrap the VM

On the VM, as `deploy` (Hetzner) or `ubuntu` (Oracle):

```bash
sudo apt-get update && sudo apt-get install -y git
git clone https://github.com/<you>/<repo>.git ~/myfantasyprem   # private repo: use a read-only deploy key
cd ~/myfantasyprem
bash deploy/bootstrap-vm.sh
exit   # log out and back in so the docker group applies
```

It is idempotent (run it again any time). It installs Docker from Docker's
repository with log rotation (10 MB x 5 per container), enables ufw with
only SSH (a second layer behind the Hetzner firewall; on Oracle images it
keeps Oracle's SSH-only iptables rules instead, as Oracle advises against
ufw there), makes sshd key-only with no root login,
enables fail2ban for SSH, unattended security upgrades (reboot at 04:30 UTC
when needed), a 2 GB swap file and a 500 MB journal cap.

Check: `docker run --rm hello-world` works without sudo; `sudo fail2ban-client
status sshd` shows the jail.

## 4. Cloudflare Tunnel (API)

1. Cloudflare dashboard > **Zero Trust** (free plan; it asks for a team name)
   > **Networks > Tunnels > Create a tunnel > Cloudflared**. Name it
   > `myfantasyprem`.
2. On the install step, copy the token (the long string after `--token` in
   the Docker command). Do not run their command; compose runs cloudflared.
   The token goes into `TUNNEL_TOKEN` (step 8).
3. **Public hostname**: subdomain `api`, domain `example.com`, service type
   `HTTP`, URL `api:8080`. Save. Cloudflare creates the DNS record.
4. WebSockets are on by default (domain > **Network > WebSockets**); keep
   them on: the app's realtime socket is `wss://api.example.com/v1/realtime`.

## 5. Cloudflare R2 (backups)

1. Dashboard > **R2 Object Storage**. Enabling R2 asks for a payment method;
   usage stays within the free tier (a year of 30 nightly dumps of a small
   database is well under 1 GB).
2. **Create bucket** `myfantasyprem-backups` (location: automatic).
3. Bucket > **Settings > Object lifecycle rules > Add rule**: delete objects
   after 35 days (a backstop; `backup.sh` already prunes at 30).
4. **R2 > Manage API tokens > Create API token**: permission **Object Read &
   Write**, "Apply to specific buckets only" > the bucket. Copy the **Access
   Key ID**, **Secret Access Key** and the **S3 endpoint**
   (`https://<account id>.r2.cloudflarestorage.com`) into `BACKUP_S3_*`.

## 6. Resend (sign-in email)

1. Sign up at resend.com (free). **Domains > Add domain**: `example.com`
   (or a subdomain such as `mail.example.com`; then the sender is
   `login@mail.example.com`).
2. Resend shows the DNS records to add. Use its **Cloudflare auto-configure**
   button, or add them by hand in Cloudflare DNS, values copied exactly
   from Resend. They look like:

   | Type | Name                | Value (from Resend)                                 | Notes                 |
   | ---- | ------------------- | --------------------------------------------------- | --------------------- |
   | TXT  | `resend._domainkey` | `p=MIGfMA0...` (DKIM key)                           | DNS only (grey cloud) |
   | MX   | `send`              | `feedback-smtp.<region>.amazonses.com`, priority 10 | bounce handling       |
   | TXT  | `send`              | `v=spf1 include:amazonses.com ~all`                 | SPF                   |
   | TXT  | `_dmarc` (optional) | `v=DMARC1; p=none;`                                 | recommended           |

3. Wait for **Verified** (minutes, sometimes an hour).
4. **API Keys > Create API key**: permission **Sending access**, domain
   `example.com`. Copy it into `RESEND_API_KEY`.
5. Free plan: 100 emails a day, so at most 100 sign-ins a day. Ample here.
   The API's per-IP and per-address limits on `POST /v1/auth/email` keep
   strangers from spending that quota.

## 7. API image (GitHub Container Registry)

The image is built and pushed from a laptop with `deploy/deploy.sh push`
(arm64 and amd64); there is no GitHub Actions deploy workflow. The Oracle VM
setup at the top of this file is the current path.

Let the VM pull it, either:

- make the package public (GitHub > your profile > Packages >
  `myfantasyprem-api` > Package settings > Change visibility; it holds
  binaries only, no secrets), or
- on the VM: `docker login ghcr.io -u <you>` with a classic personal
  access token that has only `read:packages`.

No registry? Build on the VM (native ARM, about 2 minutes): set
`API_IMAGE=myfantasyprem-api` in `deploy/.env`, then
`deploy/deploy.sh build v0.1.0` before `deploy.sh deploy v0.1.0`.

## 8. Configure `deploy/.env`

On the VM:

```bash
cd ~/myfantasyprem
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
openssl rand -hex 24          # paste as POSTGRES_PASSWORD
nano deploy/.env
```

Every variable is documented in [`deploy/.env.example`](../deploy/.env.example).
The ones you must set:

| Variable                                                   | Value                                                           |
| ---------------------------------------------------------- | --------------------------------------------------------------- |
| `API_IMAGE`                                                | `ghcr.io/<you>/myfantasyprem-api` (lowercase)                   |
| `POSTGRES_PASSWORD`                                        | the `openssl rand -hex 24` output                               |
| `APP_URL`, `CORS_ALLOWED_ORIGINS`                          | `https://app.example.com` (required in production)              |
| `REALTIME_URL`                                             | `wss://api.example.com/v1/realtime`                             |
| `TUNNEL_TOKEN`                                             | step 4                                                          |
| `FPL_USER_AGENT`                                           | keep a contact address in it                                    |
| `RESEND_API_KEY`, `EMAIL_FROM`                             | step 6; `"MyFantasyPrem <login@example.com>"`                   |
| `EMAIL_PROVIDER`                                           | `resend` once step 6 is done, else `none`                       |
| `BACKUP_S3_ENDPOINT`, `BACKUP_S3_ACCESS_KEY_ID`, `..._KEY` | step 5                                                          |
| `BACKUP_HEARTBEAT_URL`                                     | optional, step 12                                               |
| `APPLE_CLIENT_IDS`, `GOOGLE_CLIENT_IDS`                    | optional; empty turns those sign-ins off (503)                  |
| `APP_ENV`                                                  | `production` (leave)                                            |
| `BRANDED_ASSETS`                                           | `true` private build; `false` for any public build              |
| `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT`   | Web Push; `npm run admin -- vapid-keys`, docs/notifications.md  |
| `UPDATES_PUBLIC_URL`                                       | `https://api.example.com` when `UPDATES_DIR` serves OTA updates |

Behind the tunnel the API sees plain http and, in production, ignores
forwarded headers, so without `UPDATES_PUBLIC_URL` the update manifest's
asset urls would start `http://`. Set it to the API's public base URL
whenever OTA updates are served; unset keeps the request's own host (the
tailnet setup in docs/IOS.md).

`AUTH_DEV_MAGIC_LINKS` is forced off by compose; never use
`EMAIL_PROVIDER=log` in production.

## 9. First deploy

```bash
cd ~/myfantasyprem
deploy/deploy.sh deploy v0.1.0     # pull, (backup), migrate, start the API, wait for healthy
deploy/deploy.sh up                # also starts cloudflared (COMPOSE_PROFILES=tunnel)
deploy/deploy.sh install-timer     # nightly backup at 03:15 UTC
deploy/deploy.sh backup            # one now, to prove R2 works
deploy/deploy.sh status
```

Checks:

- `curl https://api.example.com/healthz` from your laptop answers
  `{"status":"ok","version":"v0.1.0"}`.
- Zero Trust > Tunnels shows the tunnel **Healthy**.
- `deploy.sh restore --list` lists the dump you just made.

## 10. Real player data

The poller (`POLLER_ENABLED=true`, `POLLER_SOURCE=fpl`) runs inside the API
and fills clubs, players, fixtures and live stats from FPL Draft's public
API on its first poll, then keeps polling (faster during matches). There is
no seed step: **never run `seed` on production** (it loads the fictional
demo world; `deploy.sh seed` refuses).

Check within a minute or two: `deploy.sh status` shows a `poll_state` row
with `last_good_at` set and `consecutive_failures` 0; `deploy.sh logs | grep
poll` shows `poll ok`. Problems: RUNBOOK "FPL API down or blocked".

## 11. Web app on Cloudflare Pages

Publish it by hand from the laptop (no workflow does it):

```bash
EXPO_PUBLIC_API_BASE_URL=https://api.example.com npm run build:web:pages
npx wrangler pages deploy apps/app/dist --project-name myfantasyprem --branch main
```

`build:web:pages` builds the production SPA (no mocks, verified by
`verify:prod-bundle`), checks that the bundle carries the API URL, and adds
`_headers` (CSP allowing only the API origin over https/wss, HSTS,
`Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, nosniff, long cache
for hashed assets) and `_redirects` (SPA fallback). The first `wrangler`
run opens a browser to log in and creates the project.

Then Workers & Pages > `myfantasyprem` > **Custom domains > Set up a
domain**: `app.example.com`.

Check: open `https://app.example.com`, request a sign-in link with your
email, and the mail arrives from `login@example.com`; the link signs you
in. Browser devtools show no CSP errors.

## 12. Monitoring ($0)

- **Restarts**: every service has `restart: unless-stopped` and a
  healthcheck (API: `/app/healthcheck` against `/healthz`; Postgres:
  `pg_isready`; cloudflared: `tunnel ready`). Docker survives reboots.
- **Uptime** (UptimeRobot, free): three monitors, alerts by email.
  1. HTTP(s) `https://api.example.com/healthz`.
  2. Keyword `https://api.example.com/v1/gameweeks/current`, alert when the
     keyword `"stale":true` **exists**: FPL data is stale (poller failing).
  3. HTTP(s) `https://app.example.com`.
- **Backups** (Healthchecks.io, free): a check with period 1 day, grace 2
  hours; put its ping URL in `BACKUP_HEARTBEAT_URL`. `backup.sh` pings it
  after each good backup and `<url>/fail` on failure, so a silent failure
  also alerts.
- **Logs**: JSON, rotated (10 MB x 5 per container). `deploy.sh logs api`;
  poller only: `deploy.sh logs api | grep '"component":"poller"'`.
- **Poller staleness**: `curl -s 127.0.0.1:9090/debug/vars | jq .poller` on
  the VM (`stale`, `consecutive_failures`, `last_good_unix`), the
  `poll_state` row in `deploy.sh status`, or `meta.stale` on any stats
  response. Details: RUNBOOK "How to tell data is stale".

## 13. Your league

1. Sign in on `https://app.example.com`, create the league, set the draft.
2. **Invite**: share the invite link (or the code) from the league's
   invite screen. Check: the link starts with your `APP_URL`
   (`https://app.example.com/join/...`) and, opened in a private window,
   shows the league before sign-in.

## 14. Backups and the restore drill

- Nightly at 03:15 UTC (`systemctl list-timers myfantasyprem-backup.timer`)
  plus one before every `deploy` (named `<db>-<time>-predeploy.dump`):
  `pg_dump` custom format, compressed, checked with `pg_restore --list`,
  uploaded with a SHA-256 file, remote size verified, older than 30 days
  pruned. The last 3 days also stay on the VM (volume `backups`).
- A database with no tables (a new VM before its restore) is never dumped,
  so an empty dump cannot become the newest; the nightly run then skips the
  heartbeat, which alerts. `restore latest` also skips any dump without
  table data.
- On Hetzner, the whole-disk backups (step 2) are a second, coarser layer.
- **Monthly drill** (5 minutes, does not touch live data):

  ```bash
  deploy/deploy.sh restore --list
  deploy/deploy.sh restore latest     # into myfantasyprem_restore; prints every table's row count
  deploy/deploy.sh psql               # optional: look around: \c myfantasyprem_restore
  # then: DROP DATABASE myfantasyprem_restore;
  ```

- **Real restore** (data loss, bad migration):

  ```bash
  deploy/deploy.sh restore latest --replace          # or a file name from --list
  ```

  It stops the API, restores into a scratch database, checks it, closes
  both databases to new sessions, then renames the live database to
  `myfantasyprem_pre_restore_<time>` (kept) and the restored one to the
  live name in **one transaction**: both renames happen or neither. It
  starts the API. If it fails, `deploy.sh` checks and says whether the live
  database is in place. Drop the old copy once happy.

## 15. Updating

```bash
# laptop
deploy/deploy.sh push v0.2.0                   # builds and pushes the API image (see 7)
# then publish the web app as in 11
# VM
cd ~/myfantasyprem && git pull                 # picks up compose/script changes
deploy/deploy.sh deploy v0.2.0
```

`deploy` pulls the image, takes a pre-deploy backup, runs migrations, then
recreates only the API container and waits for its healthcheck. Measured
locally: about one second of failed requests; the app retries, and realtime
clients reconnect (falling back to polling meanwhile). If the new API is not
healthy within two minutes it rolls back to the previous tag by itself.

Run one API only: the poller must run in exactly one process.

## 16. Rollback

- API: `deploy/deploy.sh rollback` (previous tag; run again to go forward).
  Migrations are not undone: keep each migration compatible with the
  previous release (add columns and tables; drop them a release later). If
  a migration itself is the problem, restore the pre-deploy backup
  (`restore <name>-predeploy.dump --replace`).
- Web: Workers & Pages > project > Deployments > an earlier one >
  **Rollback to this deployment**.

## Rebuild on a new VM (about 30 minutes)

1. New Ubuntu 24.04 VM anywhere (ARM or x86: the image is multi-arch); SSH
   key in. On Hetzner: step 2 (or rebuild the server from a Hetzner backup
   and skip to step 3 below).
2. Steps 3 and 8 (clone, `bootstrap-vm.sh`, copy `deploy/.env` from your
   password manager).
3. `deploy/deploy.sh deploy <tag>` then `deploy/deploy.sh restore latest
--replace`, then `deploy/deploy.sh up` and `install-timer`. The deploy's
   pre-deploy backup is skipped on the empty database, so `latest` is still
   the old VM's last good dump; check the name it prints.
4. The tunnel follows the token: the old VM's cloudflared (if alive) must
   be stopped, or both serve. DNS and Pages do not change.

Keep `deploy/.env` in a password manager: it is the only state not in git
or R2.

## Alternative: Oracle Cloud Always Free ($0)

Everything else in this guide is the same; only the VM differs. Trade-offs:
$0 and more memory (2 OCPU / 12 GB), but A1 capacity can be scarce at
signup, Oracle may reclaim idle Always Free VMs, and the account can be
suspended. Free-tier limits checked in October 2026: A1 1,500 OCPU-hours
and 9,000 GB-hours a month (2 OCPU / 12 GB running all month) and 200 GB
of block storage ([Oracle docs][oci-free]). Log in as `ubuntu` (no extra
user needed) and skip the Hetzner steps.

1. Sign up at oracle.com/cloud/free. Pick the **home region** carefully: it
   cannot change, and Always Free A1 VMs live only there. A region with
   several availability domains gives more chances of A1 capacity.
2. **Compute > Instances > Create instance**:
   - Image: **Canonical Ubuntu 24.04** (the aarch64 build is picked with the
     shape).
   - Shape: **Ampere > VM.Standard.A1.Flex**, **2 OCPU, 12 GB** (the whole
     Always Free allowance; smaller is fine too).
   - Networking: create a VCN with a public subnet; assign a public IPv4.
   - SSH keys: paste your `~/.ssh/id_ed25519.pub`.
   - Boot volume: 100 GB (free up to 200 GB in total).
   - "Out of capacity": retry later or in another availability domain.
3. Leave the subnet's security list as created: it allows only TCP 22 in.
   Nothing else needs opening; the tunnel dials out.
4. Check: `ssh ubuntu@<public ip>` logs you in.

### Oracle and idle VMs

Oracle may reclaim idle **Always Free** instances: over 7 days, 95th
percentile CPU under 20%, network under 20% and, for A1, memory under 20%
([Oracle docs][oci-free]). A 12-player league is that idle. Do **not** run
CPU-burning scripts to look busy: that games the policy and can still get
the account closed. Instead:

1. **Upgrade the tenancy to Pay As You Go** (Billing > Upgrade and manage
   payment). Oracle's docs state that "Oracle doesn't charge for Always Free
   resources after you upgrade, and will only charge you for resource usage
   above the Always Free limits" ([Always Free resources][oci-free]). The
   reclamation rule is written for Always Free instances; that upgraded
   tenancies are exempt is widely reported but **not stated in the docs**,
   so keep steps 2 and 3 regardless.
   - Then set a budget alert (Billing > Budgets, $1, email) so any
     accidental paid resource shows up within a day. Use only shapes and
     sizes marked "Always Free-eligible".
2. **Backups off the box**: nightly to R2 (step 9). Losing the VM loses at
   most a day.
3. **A 30-minute rebuild path** onto any Ubuntu 24.04 VM (a Hetzner
   server as above, another Oracle VM, a home server): the same compose file, `bootstrap-vm.sh`,
   `deploy.sh deploy <tag>`, `deploy.sh restore latest --replace`. See "Rebuild on a
   new VM". The tunnel and DNS do not change: only the token moves.

## Local rehearsal

The same stack runs on a laptop without the tunnel: copy `.env.example` to
a file with `COMPOSE_PROFILES=` (empty), `API_IMAGE=myfantasyprem-api`,
`POLLER_SOURCE=demo`, `EMAIL_PROVIDER=none` and an S3-compatible test
bucket, then `ENV_FILE=<that file> deploy/deploy.sh build v0 && ...
deploy v0`. Pick unused host ports (`API_HOST_PORT`, `METRICS_HOST_PORT`).

## Risks and limits

- **Invite links** are `APP_URL/join/<code>`: a wrong `APP_URL` breaks
  every invite link (and sign-in link) already sent. The web app serves
  `/join/<code>` from the SPA fallback in `_redirects`.
- **Single VM, single Postgres**: a VM loss costs the time to rebuild and
  up to a day of data (nightly backups plus pre-deploy ones).
- **Client IPs**: behind Caddy the API trusts `X-Forwarded-For` from
  `CADDY_IP` only (`TRUSTED_PROXIES`), so per-IP limits see real clients.
  Behind the tunnel (older setup) the API sees cloudflared's address and
  every client shares one per-IP bucket.
- **Postgres 17** in production, development and CI (`PG_MAJOR=17`). A
  database created on 16 by the older setup needs a dump and restore
  (`deploy.sh migrate-from-dump`), not just a new image.
- **One provider for the VM**: a Hetzner account or data-centre problem
  takes the VM and its Hetzner backups together. The R2 dumps and the
  rebuild path are the answer (any Ubuntu VPS, or Oracle's free VM).
- **Oracle** (if used): capacity for A1 can be scarce at signup; the
  account and VM can be suspended by Oracle. Backups and the rebuild path
  are the answer.
- **No email sign-in** with `EMAIL_PROVIDER=none`: a lost session means
  a new claim or sign-in link from the commissioner, or `deploy.sh admin
session ... --i-know` on the VM.

[oci-free]: https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
[r2-pricing]: https://developers.cloudflare.com/r2/pricing/
[resend]: https://resend.com/pricing
[uptimerobot]: https://uptimerobot.com/pricing/
[hc]: https://healthchecks.io/pricing/
