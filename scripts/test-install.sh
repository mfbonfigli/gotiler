#!/bin/sh
#
# Tests scripts/install.sh against scripts/test-release-server.py, which
# serves BASE_URL/good, a release holding the archives built by this run,
# and BASE_URL/bad, the same release with wrong checksums. Each run installs
# into a new temporary HOME. Written for any POSIX sh.
#
# Usage: scripts/test-install.sh BASE_URL TAG
#   e.g. scripts/test-install.sh http://127.0.0.1:8000 v0.0.0-ci

set -eu

if [ $# -ne 2 ]; then
    echo "Usage: $0 BASE_URL TAG" >&2
    exit 1
fi
base_url="$1"
tag="$2"
installer="$(cd "$(dirname "$0")" && pwd)/install.sh"

HOME="$(mktemp -d)"
export HOME
unset GOTILER_VERSION GOTILER_INSTALL_DIR GOTILER_BIN_DIR
install_dir="$HOME/.local/share/gotiler"
gotiler="$HOME/.local/bin/gotiler"
log="$HOME/install.log"

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

# run_installer RELEASES_URL [NAME=VALUE...] runs the installer with the
# given environment variables, keeping its output in $log
run_installer() {
    releases_url="$1"
    shift
    if env GOTILER_RELEASES_URL="$releases_url" "$@" sh "$installer" >"$log" 2>&1; then
        cat "$log"
        return 0
    fi
    cat "$log"
    return 1
}

echo "==> Installing the latest release"
run_installer "$base_url/good" || fail "the install failed"
[ -L "$gotiler" ] || fail "$gotiler is not a link"
"$gotiler" version || fail "the installed gotiler does not run"
grep -q 'export PATH=' "$log" || fail "no hint to put the bin folder on PATH"

echo "==> Reinstalling a pinned release keeps the files added to share/"
echo "grid" >"$install_dir/share/test_grid.tif"
run_installer "$base_url/good" GOTILER_VERSION="$tag" || fail "the reinstall failed"
[ -f "$install_dir/share/test_grid.tif" ] || fail "the reinstall dropped share/test_grid.tif"
[ ! -e "$install_dir.new" ] && [ ! -e "$install_dir.old" ] || fail "the reinstall left temporary folders"
"$gotiler" version || fail "the reinstalled gotiler does not run"

echo "==> A wrong checksum leaves the install untouched"
if run_installer "$base_url/bad"; then
    fail "the installer accepted a wrong checksum"
fi
grep -q 'does not match' "$log" || fail "no checksum error"
[ -f "$install_dir/share/test_grid.tif" ] || fail "the failed install changed $install_dir"
"$gotiler" version || fail "the failed install broke the previous one"

echo "==> Folders the installer did not create are left alone"
mkdir "$HOME/other"
echo "keep" >"$HOME/other/notes.txt"
if run_installer "$base_url/good" GOTILER_INSTALL_DIR="$HOME/other"; then
    fail "the installer replaced a folder it did not create"
fi
[ -f "$HOME/other/notes.txt" ] || fail "the installer changed $HOME/other"

echo "All install tests passed."
