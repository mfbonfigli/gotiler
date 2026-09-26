#!/usr/bin/env bash

# Runs the test binaries produced by the Docker build. Each binary is executed
# from its package directory, like `go test` does, so tests reading files under
# testdata/ work, and PROJ_DATA points the coordinate conversion tests at the
# PROJ database shipped with the build.
#
# Usage: scripts/run-tests.sh TESTS_DIR PROJ_DATA_DIR
#   e.g. scripts/run-tests.sh build/tests/linux-amd64 build/linux-amd64/share

set -o errexit
set -o nounset
set -o pipefail

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 TESTS_DIR PROJ_DATA_DIR" >&2
    exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tests_dir="$(cd "$1" && pwd)"
proj_data="$(cd "$2" && pwd)"

# Windows test binaries need a native path
if command -v cygpath >/dev/null 2>&1; then
    proj_data="$(cygpath -w "$proj_data")"
fi
export PROJ_DATA="$proj_data"

mapfile -t binaries < <(find "$tests_dir" -type f \( -name '*.test' -o -name '*.test.exe' \) | sort)
if [[ ${#binaries[@]} -eq 0 ]]; then
    echo "No test binaries found in $tests_dir" >&2
    exit 1
fi

failed=()
for bin in "${binaries[@]}"; do
    pkg_dir="$(dirname "${bin#"$tests_dir"/}")"
    echo "==> Testing ./$pkg_dir"
    chmod +x "$bin"
    if ! (cd "$repo_root/$pkg_dir" && "$bin" -test.v); then
        failed+=("$pkg_dir")
    fi
done

if [[ ${#failed[@]} -gt 0 ]]; then
    echo "Test failures in: ${failed[*]}" >&2
    exit 1
fi
echo "All ${#binaries[@]} test binaries passed."
