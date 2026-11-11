#!/usr/bin/env bash
# Installs Ubuntu packages on a GitHub runner. The runner's Azure mirror
# sometimes stops answering and apt then waits 20 s per package for half an
# hour, so give it a short time limit and switch to the main Ubuntu archive
# when it fails.
set -uo pipefail

opts=(-o Acquire::http::Timeout=15 -o Acquire::Retries=2 -o DPkg::Lock::Timeout=120)

install() {
  sudo timeout 300 apt-get "${opts[@]}" update -qq &&
    sudo DEBIAN_FRONTEND=noninteractive timeout 900 apt-get "${opts[@]}" install -y -qq "$@"
}

install "$@" && exit 0

echo "::warning::Ubuntu mirror did not answer, retrying with archive.ubuntu.com" >&2
sudo sed -i 's|http://azure.archive.ubuntu.com/ubuntu|http://archive.ubuntu.com/ubuntu|g' \
  /etc/apt/sources.list /etc/apt/sources.list.d/* 2>/dev/null || true
install "$@"
