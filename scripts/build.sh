#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

if [[ "${TRACE-0}" == "1" ]]; then
    set -o xtrace
fi

VALID_TARGETS=("linux-amd64" "linux-arm64" "windows-amd64" "all")

usage() {
    echo -e '
\033[1;32mGotiler build script\033[0m
Usage: ./build.sh [TARGET]

Arguments:
  TARGET           Build target (default: all)
                   Valid values: linux-amd64, linux-arm64, windows-amd64, all
  --version VALUE  Version string embedded in the binary

Output:
  build/
    linux-amd64/
      share/
      gotiler
      THIRD-PARTY-LICENSES.md
    linux-arm64/
      share/
      gotiler
      THIRD-PARTY-LICENSES.md
    windows-amd64/
      share/
      gotiler.exe
      THIRD-PARTY-LICENSES.md
    tests/
      linux-amd64/     test binaries, one per package directory
      linux-arm64/
      windows-amd64/
'
    exit
}

main() {
    BoldGreen='\033[1;32m'
    Blue='\033[34m'
    Cyan='\033[36m'
    Red='\033[31m'
    Reset='\033[0m'

    TARGET="all"
    VERSION_ARGS=()
    i=1

    while [[ $i -le $# ]]; do
        arg="${!i}"
        case "$arg" in
            -h|--help|help)
                usage
                ;;
            --version|-V)
                next=$((i + 1))
                if [[ $next -le $# ]]; then
                    VERSION_ARGS=(--build-arg "VERSION=${!next}")
                    i=$next
                else
                    echo -e "${Red}Missing value for $arg${Reset}"
                    exit 1
                fi
                ;;
            linux-amd64|linux-arm64|windows-amd64|all)
                TARGET="$arg"
                ;;
            *)
                echo -e "${Red}Unknown argument: $arg${Reset}"
                usage
                ;;
        esac
        i=$((i + 1))
    done

    valid=0
    for t in "${VALID_TARGETS[@]}"; do
        [[ "$TARGET" == "$t" ]] && valid=1 && break
    done
    if [[ "$valid" == "0" ]]; then
        echo -e "${Red}Invalid target: $TARGET${Reset}"
        usage
    fi

    echo -e "${BoldGreen}Gotiler build script${Reset}"
    echo -e "${Blue} => Target: ${Cyan}$TARGET${Reset}"

    export DOCKER_BUILDKIT=1
    GIT_COMMIT="$(git rev-list -1 HEAD 2>/dev/null || echo unknown)"

    DOCKER_BUILD_ARGS=(
        --build-arg "GIT_COMMIT=${GIT_COMMIT}"
    )

    case "$TARGET" in
        all)
            DOCKER_TARGET="final"
            echo -ne "${Blue} => Removing old build artifacts... ${Reset}"
            rm -rf "./build"
            echo -e "${Blue}done${Reset}"
            ;;
        linux-amd64)
            DOCKER_TARGET="linux-amd64-builder"
            rm -rf "./build/linux-amd64" "./build/tests/linux-amd64"
            ;;
        linux-arm64)
            DOCKER_TARGET="linux-arm64-builder"
            rm -rf "./build/linux-arm64" "./build/tests/linux-arm64"
            ;;
        windows-amd64)
            DOCKER_TARGET="windows-amd64-builder"
            rm -rf "./build/windows-amd64" "./build/tests/windows-amd64"
            ;;
    esac

    echo -e "${Blue} => Building...${Reset}"

    if [[ "$DOCKER_TARGET" == "final" ]]; then
        docker build "${VERSION_ARGS[@]}" "${DOCKER_BUILD_ARGS[@]}" -t gotiler:build --target=final --output ./build .
    else
        docker build "${VERSION_ARGS[@]}" "${DOCKER_BUILD_ARGS[@]}" -t "gotiler:build-$TARGET" --target="$DOCKER_TARGET" .

        tmp_extract="$(mktemp -d)"
        container_id=$(docker create "gotiler:build-$TARGET")
        docker cp "$container_id:/artifacts/." "$tmp_extract/"
        docker rm "$container_id" > /dev/null

        mkdir -p "./build/$TARGET" "./build/tests/$TARGET"
        cp -r "$tmp_extract/$TARGET/"* "./build/$TARGET/"
        cp -r "$tmp_extract/tests/$TARGET/"* "./build/tests/$TARGET/"
        rm -rf "$tmp_extract"
    fi

    # Post-build: Run tests
    run_tests() {
        local target_arch="$1"
        local test_dir="./build/tests/$target_arch"

        if [[ ! -d "$test_dir" ]]; then
            echo -e "${Blue}    No tests found for ${target_arch}${Reset}"
            return
        fi

        local host_os
        host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
        [[ "$host_os" =~ mingw|msys|cygwin ]] && host_os="windows"
        [[ "$host_os" =~ linux ]]             && host_os="linux"

        local host_arch
        host_arch="$(uname -m)"
        case "$host_arch" in
            x86_64|amd64|AMD64) host_arch="amd64" ;;
            aarch64|arm64)      host_arch="arm64" ;;
        esac

        local can_run=false
        case "$target_arch" in
            windows-amd64) [[ "$host_os" == "windows" && "$host_arch" == "amd64" ]] && can_run=true ;;
            linux-amd64)   [[ "$host_os" == "linux"   && "$host_arch" == "amd64" ]] && can_run=true ;;
            linux-arm64)   [[ "$host_os" == "linux"   && "$host_arch" == "arm64" ]] && can_run=true ;;
        esac

        if [[ "$can_run" != true ]]; then
            echo -e "${Blue}    Skipping ${target_arch} tests (host=${host_os}/${host_arch})${Reset}"
            return
        fi

        if ! bash "$(dirname "${BASH_SOURCE[0]}")/run-tests.sh" "$test_dir" "./build/$target_arch/share"; then
            echo -e "${Red}    Tests failed for ${target_arch}${Reset}"
            exit 1
        fi
        echo -e "${BoldGreen}    PASS: ${target_arch}${Reset}"
    }

    echo -e "${Blue} => Running tests...${Reset}"
    if [[ "$TARGET" == "all" ]]; then
        for t in "linux-amd64" "linux-arm64" "windows-amd64"; do
            run_tests "$t"
        done
    else
        run_tests "$TARGET"
    fi

    if [[ -d "./build/tests" ]] && [[ "${CI:-}" != "true" ]]; then
        echo -e "${Blue} => Cleaning test artifacts...${Reset}"
        rm -rf "./build/tests"
    fi

    echo -e "${BoldGreen}=> Build and test complete.${Reset}"
}

main "$@"
