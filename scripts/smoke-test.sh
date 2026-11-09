#!/usr/bin/env bash

# Runs a gotiler build folder, the executable next to its share/ PROJ data as
# the release archives ship it, on a machine without the build's libraries or
# PROJ: prints the version, then tiles a small LAZ file with an EPSG input
# CRS, which needs the PROJ database from share/. The tiling goes through a
# symbolic link to the executable, the way package managers install it.
#
# Written for bash 3.2, the version macOS ships.
#
# Usage: scripts/smoke-test.sh BUILD_DIR
#   e.g. scripts/smoke-test.sh build/darwin-arm64

set -o errexit
set -o nounset
set -o pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 BUILD_DIR" >&2
    exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exe="$(cd "$1" && pwd)/gotiler"
input="$repo_root/internal/pc/testdata/laz14/las14_pf7_v3_multichannel_1000pts.laz"

# only the PROJ data next to the executable may be used
unset PROJ_DATA PROJ_LIB

# downloaded workflow artifacts lose the executable bit
chmod +x "$exe"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "==> gotiler version"
"$exe" version

mkdir "$work/bin"
ln -s "$exe" "$work/bin/gotiler"

echo "==> Tiling $(basename "$input") through a link to the executable"
"$work/bin/gotiler" --plain -o "$work/out" -c EPSG:32633 "$input"

tileset="$(find "$work/out" -name tileset.json | head -n 1)"
if [[ -z "$tileset" ]]; then
    echo "No tileset.json written to $work/out" >&2
    exit 1
fi
echo "Smoke test passed: wrote ${tileset#"$work"/}"
