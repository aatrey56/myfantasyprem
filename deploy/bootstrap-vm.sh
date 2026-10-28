#!/usr/bin/env bash
# Prepare a fresh Ubuntu 24.04 VM (Oracle A1 or any VPS) for the stack.
# Idempotent: safe to run again. Run as the login user with sudo:
#
#   curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/deploy/bootstrap-vm.sh -o bootstrap-vm.sh
#   bash bootstrap-vm.sh
#
# It installs Docker (official apt repo) with log rotation, closes every
# inbound port but SSH, 80 and 443 (Caddy; 443/udp for HTTP/3), hardens sshd
# (keys only, no root; exits 1 if it cannot), turns on fail2ban and
# unattended security upgrades, adds swap, caps the journal and checks the
# clock is synchronised (TLS and token expiry need it).
#
# On Oracle Cloud also add ingress rules for TCP 80 and 443 (and UDP 443) to
# the subnet's security list (Networking > VCN > Security Lists): the VM
# firewall alone is not enough.
set -euo pipefail

SWAP_SIZE="${SWAP_SIZE:-2G}"
REBOOT_TIME="${REBOOT_TIME:-04:30}"
USER_NAME="$(id -un)"

say() { echo "==> $*"; }
[ "$(id -u)" -ne 0 ] || {
	echo "Run as your login user (with sudo rights), not root." >&2
	exit 1
}
. /etc/os-release
[ "${ID:-}" = ubuntu ] || echo "warning: tested on Ubuntu 24.04; this is ${PRETTY_NAME:-unknown}" >&2

export DEBIAN_FRONTEND=noninteractive
say "packages"
sudo apt-get update -qq
sudo apt-get install -y -qq ca-certificates curl gnupg git fail2ban unattended-upgrades apt-listchanges jq rsync >/dev/null

# --- Docker ---------------------------------------------------------------------
if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
	say "docker (official repository)"
	sudo install -m 0755 -d /etc/apt/keyrings
	sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
	sudo chmod a+r /etc/apt/keyrings/docker.asc
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" |
		sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
	sudo apt-get update -qq
	sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null
else
	say "docker already installed: $(docker --version)"
fi
say "docker daemon: log rotation, live-restore"
sudo mkdir -p /etc/docker
want='{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"5"},"live-restore":true}'
if [ "$(sudo cat /etc/docker/daemon.json 2>/dev/null | jq -c . 2>/dev/null)" != "$(echo "$want" | jq -c .)" ]; then
	echo "$want" | jq . | sudo tee /etc/docker/daemon.json >/dev/null
	sudo systemctl restart docker
fi
sudo systemctl enable --now docker >/dev/null
if ! id -nG "$USER_NAME" | grep -qw docker; then
	sudo usermod -aG docker "$USER_NAME"
	say "added $USER_NAME to the docker group: log out and back in before using docker"
fi

# --- Firewall: SSH, HTTP, HTTPS ----------------------------------------------------
# Oracle's Ubuntu images ship an iptables ruleset (/etc/iptables/rules.v4)
# that allows SSH and ends INPUT with a REJECT, and Oracle advises against
# ufw on them: insert ACCEPTs for 80 and 443 before that REJECT and persist
# them with netfilter-persistent. Elsewhere use ufw. These rules are not what
# keeps Postgres and the API private: Docker's published ports bypass them.
# That is the OCI security list (22, 80, 443 only) plus compose.prod.yml,
# where only Caddy publishes publicly and the API and metrics bind 127.0.0.1.
if [ -f /etc/iptables/rules.v4 ] && grep -q 'InstanceServices' /etc/iptables/rules.v4 2>/dev/null; then
	say "firewall: Oracle iptables image; allowing 80/tcp, 443/tcp and 443/udp before its REJECT"
	sudo apt-get install -y -qq iptables-persistent netfilter-persistent >/dev/null
	for rule in "-p tcp --dport 80" "-p tcp --dport 443" "-p udp --dport 443"; do
		# shellcheck disable=SC2086 # $rule is several words on purpose
		if ! sudo iptables -C INPUT -m state --state NEW $rule -j ACCEPT 2>/dev/null; then
			reject="$(sudo iptables -L INPUT --line-numbers -n | awk '$2 == "REJECT" {print $1; exit}')"
			# shellcheck disable=SC2086
			sudo iptables -I INPUT "${reject:-1}" -m state --state NEW $rule -j ACCEPT
		fi
	done
	sudo netfilter-persistent save >/dev/null
	sudo iptables -L INPUT -n --line-numbers | sed -n '1,12p'
	if sudo ufw status 2>/dev/null | grep -q 'Status: active'; then
		echo "warning: ufw is active on an Oracle image; Oracle advises against it" >&2
	fi
else
	say "firewall: ufw deny incoming, allow SSH, 80, 443"
	sudo apt-get install -y -qq ufw >/dev/null
	sudo ufw default deny incoming >/dev/null
	sudo ufw default allow outgoing >/dev/null
	sudo ufw allow OpenSSH >/dev/null
	sudo ufw allow 80/tcp >/dev/null
	sudo ufw allow 443/tcp >/dev/null
	sudo ufw allow 443/udp >/dev/null
	sudo ufw --force enable >/dev/null
fi

# --- SSH hardening ---------------------------------------------------------------
say "sshd: keys only, no root login"
if [ ! -s "$HOME/.ssh/authorized_keys" ]; then
	echo "FAILED: $HOME/.ssh/authorized_keys is empty, so password login cannot be turned off." >&2
	echo "Add your public key there (or create the VM with one), then run this script again." >&2
	exit 1
fi
sudo tee /etc/ssh/sshd_config.d/10-myfantasyprem.conf >/dev/null <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
MaxAuthTries 3
EOF
if ! sudo sshd -t; then
	sudo rm -f /etc/ssh/sshd_config.d/10-myfantasyprem.conf
	echo "FAILED: sshd -t rejects the configuration (removed again); SSH is not hardened" >&2
	exit 1
fi
sudo systemctl reload ssh

say "fail2ban: sshd jail"
sudo tee /etc/fail2ban/jail.d/sshd.local >/dev/null <<'EOF'
[sshd]
enabled = true
backend = systemd
maxretry = 5
findtime = 10m
bantime = 1h
EOF
sudo systemctl enable --now fail2ban >/dev/null
sudo systemctl restart fail2ban

# --- Automatic security updates -------------------------------------------------
say "unattended-upgrades (security), reboot at $REBOOT_TIME if needed"
sudo tee /etc/apt/apt.conf.d/20auto-upgrades >/dev/null <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
sudo tee /etc/apt/apt.conf.d/52myfantasyprem-upgrades >/dev/null <<EOF
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "$REBOOT_TIME";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
EOF
sudo systemctl enable --now unattended-upgrades >/dev/null

# --- Swap -------------------------------------------------------------------------
if ! swapon --show=NAME --noheadings | grep -q .; then
	say "swap: $SWAP_SIZE at /swapfile"
	sudo fallocate -l "$SWAP_SIZE" /swapfile
	sudo chmod 600 /swapfile
	sudo mkswap /swapfile >/dev/null
	sudo swapon /swapfile
	grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
	echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/90-swappiness.conf >/dev/null
	sudo sysctl -q --system
else
	say "swap already on"
fi

# --- Logs -------------------------------------------------------------------------
say "journald: cap at 500 MB"
sudo mkdir -p /etc/systemd/journald.conf.d
sudo tee /etc/systemd/journald.conf.d/size.conf >/dev/null <<'EOF'
[Journal]
SystemMaxUse=500M
EOF
sudo systemctl restart systemd-journald

# --- Time -------------------------------------------------------------------------
sudo timedatectl set-timezone UTC
sudo timedatectl set-ntp true
synced=no
for _ in $(seq 1 30); do
	if [ "$(timedatectl show -p NTPSynchronized --value)" = yes ]; then
		synced=yes
		break
	fi
	sleep 2
done
if [ "$synced" = yes ]; then
	say "clock synchronised: $(date -u +%FT%TZ)"
else
	timedatectl >&2 || true
	echo "FAILED: the clock is not NTP-synchronised after 60s (systemctl status systemd-timesyncd)" >&2
	exit 1
fi

say "done. Next: log out and in (docker group), clone the repo, fill deploy/.env (docs/DEPLOY.md)."
say "Oracle: the subnet's security list also needs ingress TCP 80, TCP 443 and UDP 443 from 0.0.0.0/0."
