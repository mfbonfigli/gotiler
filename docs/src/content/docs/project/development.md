---
title: Development
description: Build gotiler from source with Docker or a local toolchain, run its tests, regenerate generated data, and work on this site.
sidebar:
  order: 2
---

The repository holds the whole project as a single Go module, `github.com/mfbonfigli/gotiler/v3`:

| Path                              | Content                                                                                                        |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `cmd/`, `cli/`                    | The `gotiler` command line application                                                                         |
| `tiler/`, `internal/`, `version/` | The tiling engine and its public API, see the [Go library](/gotiler/library/overview/)                         |
| `plugins/`                        | Optional engine features: compression, E57 and PLY input, GeoTIFF colorization, color ramps, 3TZ, S3, subsampling |
| `scripts/`                        | Build, test, benchmark and license generation scripts                                                         |
| `docs/`                           | This documentation site                                                                                        |

## Reproducible Docker builds

Docker is the recommended way to build release artifacts because it builds PROJ and the C/C++ dependencies in a known environment.

```bash
bash scripts/build.sh linux-amd64
bash scripts/build.sh linux-arm64
bash scripts/build.sh windows-amd64
bash scripts/build.sh all
```

Windows PowerShell:

```powershell
.\scripts\build.ps1 -Target windows-amd64
```

Each target folder under `build/` contains the executable, the PROJ data (`share/`) and the `THIRD-PARTY-LICENSES.md` generated for that platform. The build also compiles one test binary per package under `build/tests/<target>/`, mirroring the package layout, and runs them when the host can execute the target. `scripts/run-tests.sh` runs each binary from its package directory, like `go test` does, so tests find their `testdata/`, with `PROJ_DATA` pointing at the build's `share/`:

```bash
bash scripts/run-tests.sh build/tests/linux-arm64 build/linux-arm64/share
```

The GitHub workflows use the same script to run the cross-compiled test binaries on native linux-arm64 and windows-amd64 runners.

## macOS builds

Docker cannot build macOS executables, so the `darwin-arm64` target builds natively on an Apple Silicon Mac. `scripts/build-darwin.sh` follows the Dockerfile builders: it builds the vcpkg libraries and PROJ statically under `build/deps/darwin-arm64` (reused by later builds), then the executable, the test binaries, the PROJ data and the third-party licenses. It needs the Xcode Command Line Tools, Go, CMake, pkg-config and bash 4 or later, since macOS ships bash 3.2:

```bash
xcode-select --install
brew install bash cmake pkgconf go
bash scripts/build.sh darwin-arm64
```

The executable links to macOS system libraries only, and needs macOS 12 or later. It carries the ad-hoc signature that the linker gives every Apple Silicon executable, with no Apple Developer ID signature or notarization: see [Installation](/gotiler/getting-started/installation/#manual-download) for running a downloaded copy. The GitHub workflows build it on a macOS 15 runner, which also runs the tests, then run it on a clean macOS 26 runner with `scripts/smoke-test.sh`, which tiles a small file through a link to the executable.

## Local Windows development

Local builds require Go, CGO, MinGW, pkg-config, vcpkg dependencies, and a static PROJ build.

Install MSYS2 packages:

```bash
pacman -S --noconfirm mingw-w64-x86_64-pkgconf
pacman -S --noconfirm mingw-w64-x86_64-gcc
pacman -S --noconfirm mingw-w64-x86_64-cmake
pacman -S --noconfirm mingw-w64-x86_64-sqlite3
```

Install vcpkg dependencies:

```powershell
git clone https://github.com/Microsoft/vcpkg.git C:\vcpkg
cd C:\vcpkg
.\bootstrap-vcpkg.bat -disableMetrics
.\vcpkg.exe install sqlite3[core,tool] tiff zlib --triplet=x64-mingw-static
```

Build PROJ statically, following the Dockerfile for the canonical version and flags. The shape is:

```bash
cmake -DCMAKE_TOOLCHAIN_FILE=C:/vcpkg/scripts/buildsystems/vcpkg.cmake \
  -DVCPKG_TARGET_TRIPLET=x64-mingw-static \
  -DCMAKE_C_COMPILER=x86_64-w64-mingw32-gcc \
  -DCMAKE_CXX_COMPILER=x86_64-w64-mingw32-g++ \
  -DCMAKE_INSTALL_PREFIX=/usr/local/ \
  -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_APPS=OFF \
  -DBUILD_SHARED_LIBS=OFF \
  -DENABLE_CURL=OFF \
  -DENABLE_TIFF=ON \
  -DEMBED_PROJ_DATA_PATH=OFF \
  -DBUILD_TESTING=OFF ..
cmake --build . --config Release -j 8
cmake --build . --target install -j 8
```

Then build:

```powershell
$env:PKG_CONFIG_PATH="C:\usr\local\lib\pkgconfig;C:\vcpkg\installed\x64-mingw-static\lib\pkgconfig"
$env:CC="x86_64-w64-mingw32-gcc"
$env:CGO_ENABLED="1"
$env:CGO_LDFLAGS="-LC:/vcpkg/installed/x64-mingw-static/lib -g -O2 -static -lstdc++ -lsqlite3 -ltiff -lz -ljpeg -llzma -lm"
$env:PROJ_DATA="C:\usr\local\share\proj"

go build -o ./bin/gotiler.exe ./cmd/main.go
go test ./...
```

## Local Linux development

For Ubuntu-like environments, follow the Dockerfile steps:

1. Install build tools, CMake, pkg-config, SQLite, TIFF, and GCC.
2. Bootstrap vcpkg.
3. Install `sqlite3[core,tool]` and `tiff`.
4. Build PROJ statically with the same flags used by the Dockerfile.
5. Export `PKG_CONFIG_PATH`, `CGO_ENABLED`, `CGO_LDFLAGS`, and `PROJ_DATA`.
6. Run `go test ./...`.

## Third-party licenses

`scripts/3p-license-gen.sh` generates `THIRD-PARTY-LICENSES.md` from the actual build inputs: the Go toolchain, every Go module linked into `./cmd` for the target `GOOS`/`GOARCH` (read from the module cache, nested license files of vendored code included), the native libraries passed with `--notice`, and the hand-maintained attributions for embedded data and ported code in `scripts/third-party-notices.md`.

The Docker build runs it for every target, passing the licenses of PROJ, the vcpkg libraries and the C runtime, and the release workflow ships each platform's file in its archive. When adding embedded data or code ported from another project, add its attribution to `scripts/third-party-notices.md`. The copy committed at the repository root is the linux-amd64 one: refresh it after dependency changes with

```bash
bash scripts/build.sh linux-amd64
cp build/linux-amd64/THIRD-PARTY-LICENSES.md THIRD-PARTY-LICENSES.md
```

## Generated data

### Color ramps

`plugins/ramps/data.go` is generated from the upstream colormap sources by `plugins/ramps/gen_ramps.py` (requires `numpy` and `matplotlib`):

```bash
python plugins/ramps/gen_ramps.py
```

The previews on the [Color ramps](/gotiler/reference/color-ramps/) page are sampled from the same tables. After changing a ramp, regenerate them from the repository root, or a Go test fails:

```bash
go run ./docs/scripts/ramp-swatches > docs/src/data/ramp-swatches.json
```

### Meshopt reference vectors

The meshopt encoders in `plugins/compression` are tested against streams produced by the [meshoptimizer](https://github.com/zeux/meshoptimizer) reference encoder, stored in `plugins/compression/testdata/meshopt`. To regenerate them, run the generator from a folder outside the repository where meshoptimizer is installed:

```bash
npm install meshoptimizer@1.3.0
cp <repo>/plugins/compression/testdata/meshopt/gen_vectors.mjs .
node gen_vectors.mjs <repo>/plugins/compression/testdata/meshopt
```

## Updating dependencies

Dependencies are regular Go modules:

```bash
go get github.com/some/module@version
go mod tidy
```

## Releases

Pushing a `v3.*` tag runs the draft-release workflow, which builds and tests all targets and attaches one archive per platform (executable, `share/`, README, license and third-party licenses) to a draft GitHub release, together with a `SHA256SUMS` file listing their checksums. `scripts/package.sh` builds the archives: a `.tar.gz` holding a `gotiler-<target>-<tag>/` folder for Linux and macOS, and a flat `.zip` for Windows. The release also carries `scripts/install.sh` and `scripts/install.ps1`, which the [one-line installs](/gotiler/getting-started/installation/#install-script) download from the latest release. Each archive and script gets a signed build provenance attestation, which anyone can check with `gh attestation verify <file> --repo mfbonfigli/gotiler`.

## Install scripts

`scripts/install.sh` is plain POSIX `sh`, so it runs under dash, busybox and macOS's `/bin/sh`, with only the tools those systems ship. `scripts/install.ps1` runs in Windows PowerShell 5.1 and PowerShell 7, through `irm ... | iex`: it runs in a single script block and throws instead of calling `exit`, which would close the user's window. It installs in `%LOCALAPPDATA%\Programs\gotiler` and adds that folder to the user `PATH` in the registry, keeping the value's `%VARIABLES%` unexpanded.

The build workflow tests both on every pull request. It packages this run's builds and the two scripts into a test release, and serves it with `scripts/test-release-server.py`, which stands in for the GitHub releases page and its `releases/latest` redirects. `scripts/test-install.sh` then runs on Ubuntu (x86_64 and ARM64), Alpine, Fedora, Debian with GNU wget, and macOS, and `scripts/test-install.ps1` runs the exact `irm ... | iex` one-liner in Windows PowerShell 5.1 and PowerShell 7. The tests install, reinstall a pinned version keeping a file added to `share/`, reject a release with wrong checksums, and refuse to replace a folder the script did not install. To run the `sh` tests locally:

```bash
python3 scripts/test-release-server.py <folder> v0.0.0-ci 8000 &
sh scripts/test-install.sh http://127.0.0.1:8000 v0.0.0-ci
```

where `<folder>/good/download/v0.0.0-ci/` holds archives built with `scripts/package.sh`, the install scripts and their `SHA256SUMS`, and `<folder>/bad/download/v0.0.0-ci/` the same files with wrong checksums. `scripts/test-install.ps1 -BaseUrl http://127.0.0.1:8000 -Tag v0.0.0-ci` runs the PowerShell tests the same way, but it edits your user `PATH` while it runs (restoring it at the end), so it is meant for CI runners.

## Documentation site

This site lives in `docs/` and is built with [Starlight](https://starlight.astro.build/). Pages are Markdown or MDX files in `docs/src/content/docs/`. With Node.js 22.23 or later, from the `docs/` folder:

```bash
npm ci          # install the exact dependencies from package-lock.json
npm run dev     # preview the site at http://localhost:4321/gotiler/
npm run build   # build the site into dist/, failing on broken internal links
```

Pushes to `v3` that change `docs/` publish the site through `.github/workflows/docs.yml`. Pull requests build it without publishing.
