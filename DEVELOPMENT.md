# gotiler Development

This repository holds the whole project as a single Go module, `github.com/mfbonfigli/gotiler/v3`:

| Path | Content |
|------|---------|
| `cmd/`, `cli/` | The `gotiler` command line application |
| `tiler/`, `internal/`, `version/` | The tiling engine and its public API, see [LIBRARY.md](LIBRARY.md) |
| `plugins/` | Optional engine features: compression, E57, GeoTIFF colorization, color ramps, 3TZ, S3, subsampling |
| `scripts/` | Build, test, benchmark and license generation scripts |

## Reproducible Docker Builds

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

Each target folder under `build/` contains the executable, the PROJ data (`share/`) and the
`THIRD-PARTY-LICENSES.md` generated for that platform. The build also compiles one test binary per
package under `build/tests/<target>/`, mirroring the package layout, and runs them when the host
can execute the target. `scripts/run-tests.sh` runs each binary from its package directory, like
`go test` does, so tests find their `testdata/`, with `PROJ_DATA` pointing at the build's `share/`:

```bash
bash scripts/run-tests.sh build/tests/linux-arm64 build/linux-arm64/share
```

The GitHub workflows use the same script to run the cross-compiled test binaries on native
linux-arm64 and windows-amd64 runners.

## Local Windows Development

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

## Local Linux Development

For Ubuntu-like environments, follow the Dockerfile steps:

1. Install build tools, CMake, pkg-config, SQLite, TIFF, and GCC.
2. Bootstrap vcpkg.
3. Install `sqlite3[core,tool]` and `tiff`.
4. Build PROJ statically with the same flags used by the Dockerfile.
5. Export `PKG_CONFIG_PATH`, `CGO_ENABLED`, `CGO_LDFLAGS`, and `PROJ_DATA`.
6. Run `go test ./...`.

## Third-Party Licenses

`scripts/3p-license-gen.sh` generates `THIRD-PARTY-LICENSES.md` from the actual build inputs:
the Go toolchain, every Go module linked into `./cmd` for the target `GOOS`/`GOARCH` (read from
the module cache, nested license files of vendored code included), the native libraries passed
with `--notice`, and the hand-maintained attributions for embedded data and ported code in
`scripts/third-party-notices.md`.

The Docker build runs it for every target, passing the licenses of PROJ, the vcpkg libraries and
the C runtime, and the release workflow ships each platform's file in its archive. When adding
embedded data or code ported from another project, add its attribution to
`scripts/third-party-notices.md`. The copy committed at the repository root is the linux-amd64
one: refresh it after dependency changes with

```bash
bash scripts/build.sh linux-amd64
cp build/linux-amd64/THIRD-PARTY-LICENSES.md THIRD-PARTY-LICENSES.md
```

## Color Ramp Data

`plugins/ramps/data.go` is generated from the upstream colormap sources by
`plugins/ramps/gen_ramps.py` (requires `numpy` and `matplotlib`):

```bash
python plugins/ramps/gen_ramps.py
```

## Meshopt Reference Vectors

The meshopt encoders in `plugins/compression` are tested against streams produced by the
[meshoptimizer](https://github.com/zeux/meshoptimizer) reference encoder, stored in
`plugins/compression/testdata/meshopt`. To regenerate them, run the generator from a folder
outside the repository where meshoptimizer is installed:

```bash
npm install meshoptimizer@1.3.0
cp <repo>/plugins/compression/testdata/meshopt/gen_vectors.mjs .
node gen_vectors.mjs <repo>/plugins/compression/testdata/meshopt
```

## Updating Dependencies

Dependencies are regular Go modules:

```bash
go get github.com/some/module@version
go mod tidy
```

## Releases

Pushing a `v3.*` tag runs the draft-release workflow, which builds and tests all targets and
attaches one zip per platform (executable, `share/`, README, license and third-party licenses) to
a draft GitHub release, together with a `SHA256SUMS` file listing their checksums. Each zip also
gets a signed build provenance attestation, which anyone can check with
`gh attestation verify <zip> --repo mfbonfigli/gotiler`.
