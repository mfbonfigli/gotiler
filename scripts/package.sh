#!/usr/bin/env bash

# Packages a build folder into its release archive: a .tar.gz holding a
# gotiler-<target>-<version>/ folder for Linux and macOS, or a flat .zip for
# Windows. Each archive holds the executable, the PROJ data (share/), the
# platform's third-party licenses, the README and the license.
#
# Usage: scripts/package.sh TARGET BUILD_DIR VERSION OUT_DIR
#   e.g. scripts/package.sh linux-amd64 build/linux-amd64 v3.1.0 release-assets
#
# Tarballs need GNU tar: their files are stored as owned by root, so
# extracting one as root does not hand the files to the CI runner's user id.

set -o errexit
set -o nounset
set -o pipefail

if [[ $# -ne 4 ]]; then
    echo "Usage: $0 TARGET BUILD_DIR VERSION OUT_DIR" >&2
    exit 1
fi

target="$1"
build_dir="$2"
version="$3"
out_dir="$4"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
name="gotiler-$target-$version"

case "$target" in
    linux-amd64|linux-arm64|darwin-arm64)
        exe="gotiler"
        archive="$name.tar.gz"
        ;;
    windows-amd64)
        exe="gotiler.exe"
        archive="$name.zip"
        ;;
    *)
        echo "Unknown target: $target" >&2
        exit 1
        ;;
esac

mkdir -p "$out_dir"
out_dir="$(cd "$out_dir" && pwd)"

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
pkg="$staging/$name"
mkdir "$pkg"

cp "$build_dir/$exe" "$pkg/"
# workflow artifacts lose the executable bit
chmod 755 "$pkg/$exe"
cp -R "$build_dir/share" "$pkg/"
cp "$build_dir/THIRD-PARTY-LICENSES.md" "$pkg/"
cp "$repo_root/README.md" "$repo_root/LICENSE.md" "$pkg/"

case "$archive" in
    *.tar.gz)
        tar_version="$(tar --version 2>/dev/null || true)"
        if [[ "$tar_version" != *"GNU tar"* ]]; then
            echo "Packaging $target needs GNU tar" >&2
            exit 1
        fi
        tar -czf "$out_dir/$archive" --owner=0 --group=0 --numeric-owner -C "$staging" "$name"
        ;;
    *.zip)
        (cd "$pkg" && zip -qr "$out_dir/$archive" .)
        ;;
esac

echo "Wrote $out_dir/$archive"
