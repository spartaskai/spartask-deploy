#!/bin/sh
# Installs SparTask on a Linux server with one command:
#
#   curl -fsSL https://get.spartask.ai | sudo sh -s -- -activation <code> -license-server <url>
#
# It installs Docker when it is missing, downloads the spartask-deploy bundle of the latest
# release (SPARTASK_DEPLOY_VERSION=v1.2.0 pins one), verifies it against the release's
# SHA256SUMS, unpacks it into SPARTASK_DIR (/opt/spartask) and runs `spartaskctl install` there
# with the given arguments. Everything after `--` goes to spartaskctl install unchanged.
#
# Run the same command again after a failure: an unfinished installation in SPARTASK_DIR is
# continued with the bundle it started with.
set -eu

repo="https://github.com/spartaskai/spartask-deploy/releases"
dir="${SPARTASK_DIR:-/opt/spartask}"

fail() {
	echo "spartask: $*" >&2
	exit 1
}

[ "$(id -u)" -eq 0 ] || fail "run as root: curl -fsSL https://get.spartask.ai | sudo sh -s -- <install options>"
[ "$(uname -s)" = Linux ] || fail "SparTask installs on Linux servers (Windows Server: see the README)"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
*) fail "SparTask images are built for x86_64 (amd64) servers; this server is $(uname -m)" ;;
esac
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required"

# The questions are read from the terminal; the script itself arrives on standard input.
run_install() {
	cd "$dir"
	if [ ! -t 0 ] && (exec </dev/tty) 2>/dev/null; then
		exec ./spartaskctl install "$@" </dev/tty
	fi
	exec ./spartaskctl install "$@"
}

if [ -e "$dir/.env" ]; then
	[ -e "$dir/install-state.json" ] || fail "$dir is already installed; manage it there with ./spartaskctl (update, status, ...)"
	echo "spartask: continuing the unfinished installation in $dir"
	run_install "$@"
fi

if ! command -v docker >/dev/null 2>&1; then
	echo "spartask: installing Docker (https://get.docker.com)"
	curl -fsSL https://get.docker.com | sh
fi

if [ -n "${SPARTASK_DEPLOY_VERSION:-}" ]; then
	base="$repo/download/$SPARTASK_DEPLOY_VERSION"
else
	base="$repo/latest/download"
fi
bundle="spartask-deploy-linux-$arch.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "spartask: downloading $bundle"
curl -fsSL -o "$tmp/$bundle" "$base/$bundle"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
(cd "$tmp" && grep " $bundle\$" SHA256SUMS | sha256sum -c -) >/dev/null || fail "$bundle does not match the release's SHA256SUMS"

mkdir -p "$dir"
tar -xzf "$tmp/$bundle" -C "$dir" --strip-components=1
rm -rf "$tmp"
echo "spartask: installing in $dir"
run_install "$@"
