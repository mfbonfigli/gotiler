#!/usr/bin/env bash

# Builds the darwin-arm64 (Apple Silicon) target. Docker cannot produce macOS
# executables, so this runs natively on an Apple Silicon Mac and follows the
# Dockerfile builders: static vcpkg libraries, a static PROJ build, the gotiler
# executable, one test binary per package, the PROJ data and the third-party
# licenses. The PROJ version and the default version string are read from the
# Dockerfile. The dependencies are built once under build/deps/darwin-arm64
# and reused by later builds: delete that folder to rebuild them.
#
# The executable links to macOS system libraries only, and carries the ad-hoc
# signature that the linker gives every arm64 executable: it is not signed with
# an Apple Developer ID, nor notarized.
#
# Usage: scripts/build-darwin.sh [VERSION]
# scripts/build.sh darwin-arm64 calls it, then runs the tests.
#
# Requirements: Xcode Command Line Tools, Go, CMake, pkg-config, and bash 4 or
# later for the license and test scripts (macOS ships bash 3.2):
#   brew install bash cmake pkgconf

set -o errexit
set -o nounset
set -o pipefail

if [[ "$(uname -s)/$(uname -m)" != "Darwin/arm64" ]]; then
    echo "darwin-arm64 builds run natively on an Apple Silicon Mac" >&2
    exit 1
fi
if (( BASH_VERSINFO[0] < 4 )); then
    echo "The build needs bash 4 or later, while macOS ships bash 3.2: brew install bash" >&2
    exit 1
fi

# Go 1.26 runs on macOS 12 Monterey or later
macos_min="12.0"
export MACOSX_DEPLOYMENT_TARGET="$macos_min"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# dockerfile_arg NAME prints the default value of the Dockerfile's ARG NAME
dockerfile_arg() {
    awk -F'"' -v prefix="ARG $1=" 'index($0, prefix) == 1 { print $2; exit }' Dockerfile
}

target="darwin-arm64"
version="${1:-$(dockerfile_arg VERSION)}"
git_commit="$(git rev-list -1 HEAD 2>/dev/null || echo unknown)"
proj_version="$(dockerfile_arg PROJ_VERSION)"
jobs="$(sysctl -n hw.ncpu)"

out="build/$target"
tests_out="build/tests/$target"
deps="$repo_root/build/deps/$target"
vcpkg_root="$deps/vcpkg"
vcpkg_installed="$vcpkg_root/installed/arm64-osx"
proj_src="$deps/$proj_version"
proj_prefix="$deps/$proj_version-install"

mkdir -p "$deps"

# -------------------------------------------------------------------
# Native libraries: vcpkg's arm64-osx triplet, built static, plus the
# minimum macOS version
# -------------------------------------------------------------------
if [[ ! -x "$vcpkg_root/vcpkg" ]]; then
    rm -rf "$vcpkg_root"
    git clone https://github.com/Microsoft/vcpkg.git "$vcpkg_root"
    "$vcpkg_root/bootstrap-vcpkg.sh" -disableMetrics
fi

mkdir -p "$deps/triplets"
cat > "$deps/triplets/arm64-osx.cmake" <<EOF
set(VCPKG_TARGET_ARCHITECTURE arm64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_CMAKE_SYSTEM_NAME Darwin)
set(VCPKG_OSX_ARCHITECTURES arm64)
set(VCPKG_OSX_DEPLOYMENT_TARGET $macos_min)
EOF

"$vcpkg_root/vcpkg" install "sqlite3[core,tool]" tiff \
    --triplet=arm64-osx --overlay-triplets="$deps/triplets"

# -------------------------------------------------------------------
# PROJ, static, with the flags of the Dockerfile builders
# -------------------------------------------------------------------
if [[ ! -f "$proj_prefix/lib/libproj.a" ]]; then
    rm -rf "$proj_src" "$proj_prefix"
    curl -fsSL "https://download.osgeo.org/proj/$proj_version.tar.gz" | tar -xz -C "$deps"
    cmake -S "$proj_src" -B "$proj_src/build" \
        -DCMAKE_TOOLCHAIN_FILE="$vcpkg_root/scripts/buildsystems/vcpkg.cmake" \
        -DVCPKG_TARGET_TRIPLET=arm64-osx \
        -DCMAKE_OSX_ARCHITECTURES=arm64 \
        -DCMAKE_OSX_DEPLOYMENT_TARGET="$macos_min" \
        -DCMAKE_INSTALL_PREFIX="$proj_prefix" \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_APPS=OFF \
        -DBUILD_SHARED_LIBS=OFF \
        -DENABLE_CURL=OFF \
        -DENABLE_TIFF=ON \
        -DBUILD_TESTING=OFF \
        -DEMBED_PROJ_DATA_PATH=OFF
    cmake --build "$proj_src/build" --config Release -j "$jobs"
    cmake --install "$proj_src/build"
fi

# -------------------------------------------------------------------
# gotiler
# -------------------------------------------------------------------
export PKG_CONFIG_PATH="$proj_prefix/lib/pkgconfig:$vcpkg_installed/lib/pkgconfig" \
    CGO_ENABLED="1" \
    CGO_LDFLAGS="-L$vcpkg_installed/lib -lsqlite3 -ltiff -lz -ljpeg -llzma -lc++ -lm" \
    GOOS="darwin" \
    GOARCH="arm64"

rm -rf "$out" "$tests_out"
mkdir -p "$out/share" "$tests_out"

go build -o "$out/gotiler" -ldflags "-X main.GitCommit=$git_commit -X main.Version=$version" ./cmd/main.go

# the executable must run on Macs without vcpkg or PROJ: it can only link to
# the libraries macOS ships
otool -L "$out/gotiler"
foreign_libs="$(otool -L "$out/gotiler" | tail -n +2 | awk '{ print $1 }' | grep -vE '^(/usr/lib/|/System/Library/)' || true)"
if [[ -n "$foreign_libs" ]]; then
    echo "gotiler links to libraries that macOS does not ship:" >&2
    echo "$foreign_libs" >&2
    exit 1
fi

minos="$(vtool -show-build "$out/gotiler" | awk '$1 == "minos" { print $2 }')"
if [[ "$minos" != "$macos_min" ]]; then
    echo "gotiler requires macOS $minos instead of $macos_min" >&2
    exit 1
fi
echo "gotiler runs on macOS $minos or later"

# arm64 code only runs when signed: check the linker's ad-hoc signature
codesign --verify --verbose=2 "$out/gotiler"
codesign --display --verbose=2 "$out/gotiler"

# test binaries mirror the package layout: scripts/run-tests.sh runs each one
# from its package directory, where tests find their testdata
test_dirs="$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' ./...)"
while IFS= read -r dir; do
    [[ -n "$dir" ]] || continue
    rel="${dir#"$repo_root"/}"
    go test -c -o "$tests_out/$rel/pkg.test" "./$rel"
done <<< "$test_dirs"

cp -R "$proj_prefix/share/proj/." "$out/share/"

# libSystem and libc++ are linked dynamically from macOS itself, so unlike the
# Linux and Windows builds no C runtime is embedded
bash scripts/3p-license-gen.sh --out "$out/THIRD-PARTY-LICENSES.md" \
    --notice "PROJ=$proj_src/COPYING" \
    --notice "SQLite=$vcpkg_installed/share/sqlite3/copyright" \
    --notice "libtiff=$vcpkg_installed/share/tiff/copyright" \
    --notice "libjpeg-turbo=$vcpkg_installed/share/libjpeg-turbo/copyright" \
    --notice "liblzma (XZ Utils)=$vcpkg_installed/share/liblzma/copyright" \
    --notice "zlib=$vcpkg_installed/share/zlib/copyright"
