#!/bin/sh
#
# Installs gotiler from its GitHub releases on Linux (x86_64 or ARM64) and on
# macOS (Apple Silicon):
#
#   curl -fsSL https://github.com/mfbonfigli/gotiler/releases/latest/download/install.sh | sh
#
# It downloads the release archive for this machine, checks it against the
# release's SHA256SUMS, unpacks it into the install folder and links the
# gotiler executable from the bin folder. Running it again replaces the
# installed version but keeps the files you added to its share/ folder, such
# as PROJ grids. Downloads made this way carry no macOS quarantine flag.
#
# It needs only tools that macOS and Linux distributions ship: a POSIX sh,
# curl or wget, tar and gzip, awk, and sha256sum, shasum or openssl.
#
# Settings, as environment variables (curl ... | GOTILER_VERSION=v3.1.0 sh):
#   GOTILER_VERSION       release to install; default: the latest one
#   GOTILER_INSTALL_DIR   install folder; default: ~/.local/share/gotiler
#   GOTILER_BIN_DIR       folder of the gotiler link; default: ~/.local/bin
#   GOTILER_RELEASES_URL  releases page, for tests;
#                         default: https://github.com/mfbonfigli/gotiler/releases
#
# Everything runs from main, called on the last line, so a partly downloaded
# script runs nothing.

set -eu

# the file that marks a folder as installed by this script, which it may
# replace
install_marker=".gotiler-install"

info() {
    printf '%s\n' "$1"
}

fail() {
    printf 'gotiler install: %s\n' "$1" >&2
    exit 1
}

has() {
    command -v "$1" >/dev/null 2>&1
}

# detect_target sets target to the release platform of this machine
detect_target() {
    os="$(uname -s)"
    arch="$(uname -m)"
    case "$os" in
        Linux) os="linux" ;;
        Darwin) os="darwin" ;;
        MINGW* | MSYS* | CYGWIN*) fail "on Windows, download the .zip archive from $releases_url" ;;
        *) fail "gotiler has no release for $os" ;;
    esac
    case "$arch" in
        x86_64 | amd64) arch="amd64" ;;
        aarch64 | arm64) arch="arm64" ;;
        *) fail "gotiler has no release for $os on $arch" ;;
    esac
    if [ "$os" = "darwin" ] && [ "$arch" = "amd64" ]; then
        # a shell running under Rosetta reports x86_64 on Apple Silicon
        if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = "1" ]; then
            arch="arm64"
        else
            fail "gotiler runs on Apple Silicon Macs only"
        fi
    fi
    target="$os-$arch"
}

check_tools() {
    has curl || has wget || fail "downloading needs curl or wget"
    has tar || fail "unpacking needs tar"
    has gzip || fail "unpacking needs gzip"
    has awk || fail "checking the download needs awk"
    has sha256sum || has shasum || has openssl ||
        fail "checking the download needs sha256sum, shasum or openssl"
}

# check_destination stops before any download if installing would overwrite
# files that this script did not install
check_destination() {
    if [ -e "$install_dir" ] && [ -n "$(ls -A "$install_dir")" ] && [ ! -f "$install_dir/$install_marker" ]; then
        fail "$install_dir holds files this script did not install: set GOTILER_INSTALL_DIR to another folder"
    fi
    if [ -e "$bin_dir/gotiler" ] && [ ! -L "$bin_dir/gotiler" ]; then
        fail "$bin_dir/gotiler exists and is not a link: remove it or set GOTILER_BIN_DIR to another folder"
    fi
}

# download URL FILE
download() {
    if has curl; then
        # shellcheck disable=SC2086 # curl_proto is empty or two arguments
        curl -fsSL $curl_proto -o "$2" "$1"
    else
        wget -q -O "$2" "$1"
    fi
}

# latest_tag prints the tag that the releases/latest page redirects to
latest_tag() {
    if has curl; then
        # shellcheck disable=SC2086
        url="$(curl -fsSLI $curl_proto -o /dev/null -w '%{url_effective}' "$releases_url/latest")"
    else
        url="$(wget -S --spider "$releases_url/latest" 2>&1 | tr -d '\r' |
            sed -n 's/^ *[Ll]ocation: *\([^ ]*\).*/\1/p' | tail -n 1)"
    fi
    printf '%s\n' "${url##*/}"
}

# sha256_of FILE prints the SHA-256 checksum of FILE
sha256_of() {
    if has sha256sum; then
        sha256sum "$1" | awk '{ print $1 }'
    elif has shasum; then
        shasum -a 256 "$1" | awk '{ print $1 }'
    else
        openssl dgst -sha256 "$1" | awk '{ print $NF }'
    fi
}

# keep_added_files OLD NEW copies into the folder NEW the entries of the
# folder OLD that NEW lacks
keep_added_files() {
    for entry in "$1"/*; do
        [ -e "$entry" ] || continue
        [ -e "$2/${entry##*/}" ] || cp -R "$entry" "$2/"
    done
}

# install_files SOURCE moves the unpacked release folder SOURCE into the
# install folder, replacing a previous install, and links the executable
install_files() {
    mkdir -p "$(dirname "$install_dir")" "$bin_dir"
    staged="$install_dir.new"
    rm -rf "$staged"
    mv "$1" "$staged"
    printf 'version=%s\n' "$tag" >"$staged/$install_marker"
    if [ -d "$install_dir/share" ]; then
        keep_added_files "$install_dir/share" "$staged/share"
    fi
    if [ -e "$install_dir" ]; then
        rm -rf "$install_dir.old"
        mv "$install_dir" "$install_dir.old"
    fi
    mv "$staged" "$install_dir"
    rm -rf "$install_dir.old"
    ln -sf "$install_dir/gotiler" "$bin_dir/gotiler"
}

# print_path_hint explains how to put the bin folder on PATH when it is not
print_path_hint() {
    case ":${PATH:-}:" in
        *":$bin_dir:"*) return ;;
    esac
    case "$bin_dir" in
        "$HOME"/*) shown_dir="\$HOME${bin_dir#"$HOME"}" ;;
        *) shown_dir="$bin_dir" ;;
    esac
    case "${SHELL:-}" in
        */zsh) profile="~/.zshrc" ;;
        */bash) if [ "$os" = "darwin" ]; then profile="~/.bash_profile"; else profile="~/.bashrc"; fi ;;
        *) profile="~/.profile" ;;
    esac
    info ""
    info "$bin_dir is not on your PATH. To run gotiler by name, add this line to $profile:"
    info "  export PATH=\"$shown_dir:\$PATH\""
}

main() {
    releases_url="${GOTILER_RELEASES_URL:-https://github.com/mfbonfigli/gotiler/releases}"
    install_dir="${GOTILER_INSTALL_DIR:-${HOME:?HOME is not set}/.local/share/gotiler}"
    bin_dir="${GOTILER_BIN_DIR:-$HOME/.local/bin}"
    # the link to the executable must not depend on the current folder
    case "$install_dir" in
        /*) ;;
        *) install_dir="$PWD/$install_dir" ;;
    esac
    case "$releases_url" in
        https://*) curl_proto="--proto =https" ;;
        *) curl_proto="" ;;
    esac

    detect_target
    check_tools
    check_destination

    if [ -n "${GOTILER_VERSION:-}" ]; then
        tag="$GOTILER_VERSION"
        case "$tag" in
            v*) ;;
            *) tag="v$tag" ;;
        esac
    else
        tag="$(latest_tag)" || fail "could not reach $releases_url/latest"
        case "$tag" in
            v[0-9]*) ;;
            *) fail "found no release at $releases_url/latest" ;;
        esac
    fi

    archive="gotiler-$target-$tag.tar.gz"
    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' EXIT
    trap 'exit 1' INT TERM

    info "Downloading gotiler $tag for $target..."
    download "$releases_url/download/$tag/$archive" "$tmp_dir/$archive" ||
        fail "could not download $archive from release $tag (this script installs releases that ship .tar.gz archives)"
    download "$releases_url/download/$tag/SHA256SUMS" "$tmp_dir/SHA256SUMS" ||
        fail "could not download the SHA256SUMS of release $tag"

    expected="$(awk -v name="$archive" '$2 == name || $2 == "*" name { print $1 }' "$tmp_dir/SHA256SUMS")"
    [ -n "$expected" ] || fail "the SHA256SUMS of release $tag does not list $archive"
    actual="$(sha256_of "$tmp_dir/$archive")"
    [ "$actual" = "$expected" ] ||
        fail "$archive does not match the release's SHA256SUMS (got $actual, expected $expected)"
    info "Checksum verified."

    mkdir "$tmp_dir/unpacked"
    tar -xzf "$tmp_dir/$archive" -C "$tmp_dir/unpacked"
    [ -f "$tmp_dir/unpacked/gotiler-$target-$tag/gotiler" ] ||
        fail "$archive does not hold gotiler-$target-$tag/gotiler"

    install_files "$tmp_dir/unpacked/gotiler-$target-$tag"
    version="$("$install_dir/gotiler" version 2>/dev/null)" ||
        fail "the installed $install_dir/gotiler does not run"
    info "Installed gotiler $version in $install_dir, linked from $bin_dir/gotiler."
    print_path_hint
}

main "$@"
