# gotiler

<p align="center">
  <img src="gotiler-cli-banner.png" alt="gotiler repository banner" width="100%">
</p>

**gotiler** (formerly *gocesiumtiler*) converts **LAS, LAZ, E57 and PLY** point clouds into streaming-ready **OGC 3D Tiles 1.0 and 1.1**, ready for **CesiumJS** and other 3D Tiles viewers. Its out-of-core engine tiles over **4 million points per second** on modern hardware with NVMe storage while using little memory, so clouds of hundreds of millions of points take seconds rather than minutes.

<p align="center">
  <a href="https://mfbonfigli.github.io/gotiler/"><b>📖 Documentation</b></a> ·
  <a href="https://mfbonfigli.github.io/gotiler/getting-started/installation/">Installation</a> ·
  <a href="https://mfbonfigli.github.io/gotiler/getting-started/quick-start/">Quick start</a> ·
  <a href="https://mfbonfigli.github.io/gotiler/reference/cli-flags/">CLI flags</a> ·
  <a href="https://pkg.go.dev/github.com/mfbonfigli/gotiler/v3">Go API</a>
</p>

## ✨ Features

* **Fast and out-of-core:** uses every CPU core and fast NVMe drives, and tiles billion-point clouds without running out of RAM.
* **Compressed by default:** meshopt compression and quantization cut 3D Tiles 1.1 tilesets by 70% or more.
* **LAS, LAZ, E57 and PLY input:** every LAS/LAZ version and point format, E57 scans (experimental) and ASCII or binary PLY.
* **Automatic reprojection:** reads the CRS from LAS metadata and transforms coordinates with the embedded PROJ library.
* **3D Tiles 1.0 and 1.1:** `.pnts` or glTF tiles, optionally packed into a single `.3tz` archive.
* **Gaussian splats:** tiles 3D Gaussian splat PLY scenes into 3D Tiles 1.1 with SPZ compression, with levels of detail that CesiumJS streams.
* **Per-point attributes:** intensity, classification, GPS time, LAS extra bytes or any other attribute of the input.
* **Colorization:** 36 color ramps for any attribute or coordinate, or colors from an RGB GeoTIFF orthophoto.
* **Built-in viewer:** shows tilesets on a local CesiumJS globe, and places ungeoreferenced models by hand.
* **Self-contained:** one executable plus its bundled PROJ data, for Linux, macOS and Windows.

## ❤️ Support the Project

**gotiler** is an AGPLv3 open-source project maintained with love. If it saves you time or resources, consider **[making a donation](https://ko-fi.com/mfbonfigli)** to support ongoing maintenance, or starring the repository.

## 📸 Demo

![gotiler tiling 15.9 million points in 3.7 seconds](static/gotiler-cli-demo.webp)

*15.9M points from a thinned [Helsinki dataset](https://doi.org/10.5281/zenodo.5578198) tiled in 3.7 seconds on an Intel Core i5-13600K with a Samsung 980 PRO NVMe SSD. This is just a quick preview: the tool scales to billions of points.*

Browse tilesets generated with **gotiler** on the [preview website](https://d39maarsub1d2t.cloudfront.net/index.html).

## 🏁 Benchmarks

Time and peak memory to tile the [Helsinki point cloud](https://zenodo.org/records/5578198) (313M points, 13 GB LAS file), measured on the same AWS EC2 `i4i.2xlarge` instance running Ubuntu 24.04. [scripts/benchmark.sh](scripts/benchmark.sh) reproduces the runs.

| **tool** | **execution time** | **max memory** |
|------|---------------|-----------|
| **gotiler v3.0** | **1m 50s** | **605 MB** |
| [**py3dtiles 12.1.1**](https://gitlab.com/py3dtiles/py3dtiles) | 7m 11s | 2.56 GB |
| [**mago 3d tiler 1.15.4**](https://github.com/Gaia3D/mago-3d-tiler) | 14m 17s | 16.81 GB |

## 📣 Installation

On Linux (x86_64 or ARM64) and macOS (Apple Silicon, macOS 12 or later):

```bash
curl -fsSL https://github.com/mfbonfigli/gotiler/releases/latest/download/install.sh | sh
```

On 64-bit Windows, in PowerShell:

```powershell
irm https://github.com/mfbonfigli/gotiler/releases/latest/download/install.ps1 | iex
```

Both scripts check the download against the release's checksums and need no administrator rights. To update, uninstall, choose the install folder or a release, or download an archive by hand, see [Installation](https://mfbonfigli.github.io/gotiler/getting-started/installation/).

## ⚡ Quick Start

```bash
# Tile a LAS, LAZ, E57 or PLY file into ./out
gotiler pcloud -o ./out ./input.las

# Merge a folder into one tileset, packed as a .3tz archive
gotiler pcloud -o ./out --join --3tz ./las_folder

# Set the input CRS, color points by elevation and open the result on a globe
gotiler pcloud -o ./out -c EPSG:32633 --colorize z:viridis --preview ./input.las

# Tile a Gaussian splat scene, then place it on the globe
gotiler splat -o ./out --input-up-axis -y ./scene.ply
```

`gotiler help` lists the commands, `gotiler help pcloud` the flags of one, and the [Quick start](https://mfbonfigli.github.io/gotiler/getting-started/quick-start/) walks through a first tileset.

## 📖 Documentation

The full documentation lives at **[mfbonfigli.github.io/gotiler](https://mfbonfigli.github.io/gotiler/)**:

* **Getting started:** [Installation](https://mfbonfigli.github.io/gotiler/getting-started/installation/), [Quick start](https://mfbonfigli.github.io/gotiler/getting-started/quick-start/)
* **Point clouds:** [Input formats](https://mfbonfigli.github.io/gotiler/guides/input-formats/), [Coordinate systems](https://mfbonfigli.github.io/gotiler/guides/coordinate-systems/), [Attributes](https://mfbonfigli.github.io/gotiler/guides/attributes/), [Colorizing points](https://mfbonfigli.github.io/gotiler/guides/colorizing/), [Tiles and output](https://mfbonfigli.github.io/gotiler/guides/output/), [Examples](https://mfbonfigli.github.io/gotiler/guides/examples/)
* **Gaussian splats:** [Input formats and placement](https://mfbonfigli.github.io/gotiler/guides/gaussian-splats/input-and-placement/), [Size and detail](https://mfbonfigli.github.io/gotiler/guides/gaussian-splats/size-and-detail/), [Output](https://mfbonfigli.github.io/gotiler/guides/gaussian-splats/output/), [Examples](https://mfbonfigli.github.io/gotiler/guides/gaussian-splats/examples/)
* **Reference:** [CLI flags](https://mfbonfigli.github.io/gotiler/reference/cli-flags/), [Color ramps](https://mfbonfigli.github.io/gotiler/reference/color-ramps/), [Viewing and positioning](https://mfbonfigli.github.io/gotiler/guides/viewer/), [Placing ungeoreferenced assets](https://mfbonfigli.github.io/gotiler/guides/placing-ungeoreferenced-clouds/)
* **Go library:** [embed the tiling engine](https://mfbonfigli.github.io/gotiler/library/overview/) with the `github.com/mfbonfigli/gotiler/v3` module, and its [API reference](https://pkg.go.dev/github.com/mfbonfigli/gotiler/v3)
* **Project:** [Development](https://mfbonfigli.github.io/gotiler/project/development/), to build **gotiler** from source

## License

**gotiler** is distributed under the GNU AGPLv3, see [LICENSE.md](LICENSE.md). The executables include third-party code and data: see [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) for their licenses. Each release archive ships the copy generated for its platform.

## 💼 Commercial Licensing

If the AGPLv3 terms don't fit your use case, for example to embed the engine in proprietary software or services, commercial licenses are available: please get in touch with the maintainer through [GitHub](https://github.com/mfbonfigli).

## Acknowledgments

- [Photorealistic and classified urban point cloud dataset (Helsinki)](https://doi.org/10.5281/zenodo.5578198) by Julin, A., & Rantanen, T. (2021), used for the benchmarks and, thinned, for the demo animation. Licensed under [CC-BY-4.0](https://creativecommons.org).
- The Go gopher mascot was originally designed by Renee French and is licensed under the Creative Commons 4.0 Attribution License.

## Disclaimer

This project is an independent utility and is not affiliated with, sponsored by, or endorsed by Cesium GS, Inc. "Cesium" is a registered trademark of Cesium GS, Inc.
