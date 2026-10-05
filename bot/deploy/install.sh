#!/usr/bin/env bash
# Installs or updates otakase-bot on a Debian/Ubuntu server.
# Run it again to update to the latest main (or BRANCH).
#
#   curl -fsSL https://raw.githubusercontent.com/TheXykril/otakase/main/bot/deploy/install.sh | sudo bash
set -euo pipefail

GO_VERSION=1.26.0
SRC=/opt/otakase-src
BRANCH=${BRANCH:-main}

[ "$(id -u)" = 0 ] || { echo "run with sudo" >&2; exit 1; }

apt-get update -qq
apt-get install -y -qq git curl ca-certificates >/dev/null

# A 1 GB swap file keeps the build from running out of memory on small VMs.
if ! swapon --show | grep -q /swapfile; then
  fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap -q /swapfile && swapon /swapfile
  grep -q /swapfile /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_VERSION"; then
  arch=$(dpkg --print-architecture)
  curl -fsSL "https://go.dev/dl/go$GO_VERSION.linux-$arch.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
fi

if [ -d "$SRC/.git" ]; then
  git -C "$SRC" fetch -q --depth 1 origin "$BRANCH" && git -C "$SRC" reset -q --hard FETCH_HEAD
else
  git clone -q --depth 1 --branch "$BRANCH" https://github.com/TheXykril/otakase.git "$SRC"
fi

(cd "$SRC/bot" && GOFLAGS=-trimpath /usr/local/go/bin/go build -o /usr/local/bin/otakase-bot .)

# The latest otakase release, for /provider-status.
case "$(dpkg --print-architecture)" in arm64) cli=arm64 ;; *) cli=x86_64 ;; esac
install -d /usr/local/libexec
curl -fsSL "https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-$cli" -o /usr/local/libexec/otakase
chmod 755 /usr/local/libexec/otakase

[ -f /etc/otakase-bot.env ] || install -m 600 "$SRC/bot/deploy/otakase-bot.env" /etc/otakase-bot.env
install -m 644 "$SRC/bot/deploy/otakase-bot.service" /etc/systemd/system/otakase-bot.service
systemctl daemon-reload
systemctl enable -q otakase-bot

if grep -q '^DISCORD_BOT_TOKEN=.\+' /etc/otakase-bot.env; then
  systemctl restart otakase-bot
  echo "otakase-bot is running. Logs: journalctl -u otakase-bot -f"
else
  echo "Installed. Put the token in /etc/otakase-bot.env (sudo nano /etc/otakase-bot.env),"
  echo "then: sudo systemctl restart otakase-bot"
fi
