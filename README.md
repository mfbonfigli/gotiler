# GoTiler CLI

<p align="center">
  <img src="gotiler-cli-banner.png" alt="Gotiler Repository Banner" width="100%">
</p>

**GoTiler CLI** (formerly *gocesiumtiler*) converts **LAS, LAZ and E57** point clouds into streaming-ready **OGC 3D Tiles 1.0 and 1.1**, ready for **CesiumJS** and other 3D Tiles viewers.

Its out-of-core high-performance engine can tile over **4 million points per second** on modern hardware with NVMe storage while using little memory, so clouds of hundreds of millions of points take seconds rather than minutes.

## ✨ Features

* **Fast and out-of-core:** uses every CPU core and fast NVMe drives, and tiles billion-point clouds without running out of RAM.
* **Compressed tiles by default:** 3D Tiles 1.1 output uses `EXT_meshopt_compression` and `KHR_mesh_quantization` (or the newer `KHR_meshopt_compression`), cutting tileset size by 70% or more.
* **LAS, LAZ and E57 input:** every LAS/LAZ version (1.0–1.4) and point format, plus E57 scans (experimental).
* **Automatic reprojection:** reads the CRS from LAS GeoTIFF or WKT metadata and transforms coordinates with the embedded PROJ library. Ungeoreferenced clouds can be placed on the globe by hand.
* **3D Tiles 1.0 and 1.1:** `.pnts` or glTF (`.glb`) tiles, optionally packed into a single `.3tz` archive.
* **Uniform tiles, one setting:** set `--points-per-tile` and the cloud is split into tiles of about that size. Geometric error and `ADD`/`REPLACE` refinement can be tuned.
* **Per-point attributes:** export intensity, classification, GPS time, LAS extra bytes or any other attribute of the input.
* **Colorization:** color points by any attribute or coordinate with 36 color ramps and gradient controls, or directly from an RGB GeoTIFF orthophoto.
* **Subsampling and merging:** thin out huge clouds while tiling, or merge a folder of files into one tileset.
* **Self-contained:** one executable plus its bundled PROJ data, with live progress bars. No runtime, shared library, Docker or other tools to install.

## ❤️ Support the Project

GoTiler is an AGPLv3 open-source project maintained with love. If it saves you time or resources, consider **[making a donation](https://ko-fi.com/mfbonfigli)** to support ongoing maintenance, or starring the repository.

## 📸 Demo

![GoTiler CLI tiling 15.9 million points in 3.7 seconds](static/gotiler-cli-demo.webp)

*15.9M points from a thinned [Helsinki dataset](https://doi.org/10.5281/zenodo.5578198) tiled in 3.7 seconds on an Intel Core i5-13600K with a Samsung 980 PRO NVMe SSD. This is just a quick preview: the tool scales to billions of points.*

Browse tilesets generated with GoTiler on the [preview website](https://d39maarsub1d2t.cloudfront.net/index.html).

## 🏁 Benchmarks

Time and peak memory to tile the [Helsinki point cloud](https://zenodo.org/records/5578198) (313M points, 13 GB LAS file), measured on the same AWS EC2 `i4i.2xlarge` instance running Ubuntu 24.04. [scripts/benchmark.sh](scripts/benchmark.sh) reproduces the runs.

| **tool** | **execution time** | **max memory** |
|------|---------------|-----------|
| **gotiler CLI v3.0** | **1m 50s** | **605 MB** |
| [**py3dtiles 12.1.1**](https://gitlab.com/py3dtiles/py3dtiles) | 7m 11s | 2.56 GB |
| [**mago 3d tiler 1.15.4**](https://github.com/Gaia3D/mago-3d-tiler) | 14m 17s | 16.81 GB |

## 📣 Installation

Each [GitHub release](https://github.com/mfbonfigli/gotiler/releases) has prebuilt archives for Windows x86_64, Linux x86_64 and Linux ARM64. Unzip one anywhere: the executable must stay next to its `share` folder, which holds the PROJ data. A symbolic link to the executable, for example from a folder on your `PATH`, works too.

For high-precision datum conversions, including vertical ones, download the grids you need from the PROJ CDN into `share/`.

While processing, gotiler keeps temporary working files in a `tmp` folder inside the output folder, so the output drive needs extra free disk space. The space is released at the end of processing.

## ⚡ Quick Start

Tile a LAS, LAZ or E57 file into `./out`:

```bash
gotiler -o ./out ./input.las
```

Tile every point cloud file in a folder into separate tilesets, or join them into one tileset packaged as a `.3tz` archive:

```bash
gotiler -o ./out ./las_folder
gotiler -o ./out --join --3tz ./las_folder
```

Folders are not scanned recursively: files in subfolders are ignored. `gotiler version` prints the application version and `gotiler --help` lists every flag.

## 🛠️ Usage

### CLI Flags

| Flag | Default | Description |
|---|---:|---|
| `--out`, `-o` | *required* | Output folder for the generated tilesets. |
| `--crs`, `-c` | autodetect | Input CRS: an EPSG code such as `EPSG:4326` (bare numbers work too), a compound code with a vertical datum such as `EPSG:32633+3855`, a Proj4 string or WKT. Autodetected from LAS/LAZ metadata when omitted; required for E57. `local` accepts ungeoreferenced input, see [Placing Ungeoreferenced Point Clouds](#placing-ungeoreferenced-point-clouds). |
| `--z-offset`, `-z` | `0` | Vertical offset in meters. |
| `--points-per-tile`, `-p` | `50000` | Target points per tile, from 5,000 to 5,000,000. |
| `--refine-mode`, `-r` | `add` | Tile refinement: `add` or `replace`, see [Refine Mode](#refine-mode). |
| `--8-bit` | `false` | Treat LAS/LAZ colors as 8-bit instead of 16-bit. |
| `--version`, `-v` | `1.1` | 3D Tiles version: `1.0` (`.pnts`) or `1.1` (`.glb`). |
| `--initial-geometric-error` | `0` | Root geometric error target in meters; `0` derives it from the dataset. |
| `--ge-correction` | `1.0` | Multiplier applied to all output geometric errors. |
| `--attributes` | `intensity,classification` | Per-point attributes to export, or `none`, see [Per-Point Attributes](#per-point-attributes). |
| `--include-withheld` | `false` | Keep points flagged as withheld, which are dropped by default. |
| `--colorize` | | Color points by attribute or coordinate, e.g. `z:viridis`, see [Colorizing Points](#colorizing-points). |
| `--geotiff-colorize` | | Color points from an RGB/RGBA GeoTIFF orthophoto, see [GeoTIFF Colorization](#geotiff-colorization). |
| `--compression` | `meshopt` | `meshopt` or `none`, see [Tile Compression](#tile-compression). |
| `--meshopt-khr` | `false` | Use `KHR_meshopt_compression` instead of `EXT_meshopt_compression`, see [Tile Compression](#tile-compression). |
| `--subsample` | `100` | Percentage of points to keep, in (0, 100]. |
| `--3tz` | `false` | Write each tileset as a single `.3tz` archive, see [3TZ Archives](#3tz-archives). |
| `--join`, `-j` | `false` | Merge all files of an input folder into one tileset. |
| `--plain` | `false` | Print plain progress messages instead of progress bars. |
| `--longitude`, `--latitude`, `--height`, `--heading`, `--pitch`, `--roll`, `--scale`/`-s`, `--input-up-axis` | | Only with `--crs local`: place the model on the globe, see [Placing Ungeoreferenced Point Clouds](#placing-ungeoreferenced-point-clouds). |
| `--help`, `-h` | | Show help. |

### Refine Mode

- `add` (default): each point is stored in exactly one level of detail, so the output is smaller and no point is downloaded twice. To show a tile, viewers must also load every coarser level above it.
- `replace`: each tile repeats the points of the coarser levels and stands on its own, so viewers can skip those levels and make fewer requests, at the cost of larger output. In CesiumJS this needs the `skipLevelOfDetail` tileset option, which is off by default.

### Tile Compression

3D Tiles 1.1 (`.glb`) output is compressed by default with `EXT_meshopt_compression` and `KHR_mesh_quantization`, typically shrinking tilesets by 70% or more with no visible quality loss. CesiumJS supports both extensions; if your viewer doesn't, pass `--compression none`. 3D Tiles 1.0 (`.pnts`) output is never compressed.

`--meshopt-khr` uses `KHR_meshopt_compression` instead, the Khronos successor of the EXT extension, whose improved codec produces smaller tiles at the same quality. Viewer support is still limited (CesiumJS added it in version 1.143), so EXT remains the default.

```bash
gotiler -o ./out --compression none ./input.las
gotiler -o ./out --meshopt-khr ./input.las
```

### E57 Input

`.e57` scans from terrestrial laser scanners are read natively (experimental). gotiler doesn't read a CRS from E57 files, so pass one with `--crs`, or use `--crs local` and the [placement flags](#placing-ungeoreferenced-point-clouds) for scans in a local frame:

```bash
gotiler -o ./out --crs EPSG:32633 ./scan.e57
```

Standard E57 fields (intensity, timestamp, normals, invalid flags, row/column indices, …) and any extension field declared in the scan prototypes can be exported with `--attributes`.

### Subsampling

`--subsample` keeps a random percentage of the points, thinning huge datasets while tiling:

```bash
gotiler -o ./out --subsample 25 ./input.las
```

### 3TZ Archives

`--3tz` writes each tileset as a single OGC 3D Tiles Archive, `<out>/tileset.3tz`, instead of loose tile files. Folder inputs without `--join` get one archive per file, in `<out>/<name>/tileset.3tz`.

```bash
gotiler -o ./out --3tz ./input.las
```

### Per-Point Attributes

Per-point attributes are scalar values stored with each point next to its position and color. By default only `intensity` and `classification` are exported. `--attributes` takes a comma-separated list of any attributes the input files expose, matched case-insensitively, or `none`:

```bash
gotiler -o ./out --attributes intensity,classification,gps_time,my_custom_field ./input.las
gotiler -o ./out --attributes none ./input.las
```

Commonly available attributes for LAS/LAZ inputs:

| Attribute name | Type | Description | Included by default |
|---|---|---|---|
| `intensity` | `uint16` | Raw laser return intensity (0–65535) | Yes |
| `classification` | `uint8` | LAS point classification code | Yes |
| `return_number` | `uint8` | Return number within the pulse (1-indexed; 0 = unset) | No |
| `number_of_returns` | `uint8` | Total number of returns for the pulse | No |
| `gps_time` | `float64` | GPS timestamp of the point | No |
| `scan_angle` | `float64` | Scan angle in degrees | No |
| `point_source_id` | `uint16` | File source ID the point originated from | No |
| `user_data` | `uint8` | User data byte | No |
| `classification_flags`, `synthetic`, `key_point`, `withheld`, `overlap` | `uint8` / `bool` | Classification flag bits | No |
| `scan_direction_flag`, `edge_of_flight_line`, `scanner_channel`, `nir`, … | various | Other standard LAS point record fields | No |

Any **extra-byte attribute** declared in a LAS file can also be requested by name; scaled extra bytes are exported as `float64` physical values (`raw*scale+offset`). Common vendor spellings are matched automatically: `incidence_angle` also finds OPALS `_IncidenceAngle` and GeoCue/LP360 `True View Incidence Angle`, and `pulse_width`/`echo_width` find the ASPRS, RIEGL, OPALS (`EchoWidth`) and Terrasolid (`Echo length`) fields. RIEGL, OPALS and Terrasolid `Amplitude` and `Reflectance` match by name.

Notes:

- Requested attributes missing from the source, or from some points, are skipped silently.
- Types a tileset version can't store are dropped from that output: neither format stores 64-bit integers, and 3D Tiles 1.1 also drops 32-bit integers and stores `float64` values (e.g. `gps_time`) as `float32`. 3D Tiles 1.0 keeps `float64` exact.
- Attribute names are uppercased in the tiles (`INTENSITY`, `GPS_TIME`).

### Tileset Attribute Ranges

The dataset-wide minimum and maximum of every exported attribute are written to `tileset.json`, so viewers can normalize values for shaders and color ramps without reading any tile. 3D Tiles 1.1 uses the tileset metadata mechanism, with `MIN_`/`MAX_`-prefixed properties; 3D Tiles 1.0 uses the top-level `properties` dictionary.

<details>
<summary>Examples and details</summary>

**3D Tiles 1.1**: a metadata `schema` plus a tileset `metadata` entity, with one pair of properties per exported attribute:

```json
{
  "asset": {"version": "1.1"},
  "schema": {
    "id": "gotiler_dataset",
    "classes": {
      "dataset": {
        "properties": {
          "MIN_INTENSITY": {"type": "SCALAR", "componentType": "UINT16"},
          "MAX_INTENSITY": {"type": "SCALAR", "componentType": "UINT16"}
        }
      }
    }
  },
  "metadata": {
    "class": "dataset",
    "properties": {"MIN_INTENSITY": 12, "MAX_INTENSITY": 833}
  }
}
```

**3D Tiles 1.0**: the top-level `properties` dictionary, keyed by the per-point property name:

```json
{
  "asset": {"version": "1.0"},
  "properties": {
    "INTENSITY": {"minimum": 12, "maximum": 833}
  }
}
```

CesiumJS exposes it as `tileset.properties` and uses it to resolve `${MINIMUM}`-style bounds in declarative styling; in JavaScript, read `tileset.properties.INTENSITY.minimum` and `.maximum`.

- Ranges are written for every attribute selected with `--attributes` and found in the data, including attributes whose per-point values the tile format can't carry (e.g. 64-bit integers): the metadata is then their only trace.
- 3D Tiles 1.1 stores per-point `float64` values as `float32`, while the metadata keeps full `float64` precision: clamp when normalizing, as rounded values can fall just outside the range.

</details>

### Colorizing Points

`--colorize attribute:gradient[:modifier...]` replaces point colors using a numeric attribute or a local coordinate (`x`, `y` or `z`):

```bash
gotiler -o ./out --colorize z:viridis ./input.las
gotiler -o ./out --colorize classification:las-classification ./input.las
```

No bounds are needed: the gradient spans the 2nd to 98th percentile of the values found in the data, so outliers and skewed distributions (typical for intensity or amplitude) don't wash it out. Gradients with an absolute scale, like `las-classification`, are applied as is.

Modifiers can be combined:

| Modifier | Effect |
|----------|--------|
| `reverse` | Reverses the gradient color order. |
| `steps=N` | Quantizes the gradient into N discrete color bands (contour-band look). |
| `stretch=pLow,pHigh` | Sets the percentile stretch (default `2,98`). `stretch=minmax` scales over the full value range. |
| `blend=A` | Blends the gradient with the original point color (`1` = gradient only, `0.5` = even mix). |

```bash
gotiler -o ./out --colorize intensity:turbo:reverse:steps=8:stretch=5,95 ./input.las
```

### Color Ramps

36 ramps from freely licensed sources (matplotlib, seaborn, cmocean, Fabio Crameri's Scientific Colour Maps, ColorBrewer; see [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) for attributions). Good starting points: `viridis` for elevation, `turbo` for intensity, `rdbu` with `stretch=minmax` for change detection, `las-classification` for class codes.

<details>
<summary>All ramps</summary>

**Perceptually uniform** (best default choices; embedded as canonical 256-entry lookup tables):

| Ramp | Best for |
|---|---|
| `viridis` | General-purpose elevation or continuous density; the scientific standard. |
| `viridis-pastel` | A soft, pastel take on viridis that keeps its perceptual ordering. |
| `magma`, `inferno`, `plasma` | Heat-like sequential data with a dark-to-bright look. |
| `cividis` | Optimized for color-vision deficiency. |
| `turbo` | Vibrant rainbow alternative to "jet" with tuned lightness; great for intensity and edge spotting. |
| `batlow` | Colorblind-safe multi-hue rainbow alternative (Crameri); ideal for canopy-height models. |

**Terrain & elevation**:

| Ramp | Best for |
|---|---|
| `terrain` | Classic land elevation: blue lowlands through green, yellow and brown to white peaks. |
| `gist-earth` | Like terrain with deeper earth tones; pairs well with hillshading. |
| `topo` | Combined bathymetry + land elevation (cmocean); dark depths to bright uplands. |
| `oleron` | Perceptually uniform olive-to-brown ramp for bare-earth DEMs and dryland topography. |
| `nuuk` | Deep indigo to bright mint; crisp accents for urban building heights. |

**Sequential intensity & density** (single direction, dark-to-bright):

| Ramp | Best for |
|---|---|
| `grayscale`, `heat` | Simple built-in defaults. |
| `cubehelix` | Monotonically increasing brightness; stays readable in black-and-white prints. |
| `mako` | Dark navy to light turquoise; deep-water and coastal bathymetric LiDAR. |
| `rocket` | Blackish-purple through reds to pale cream; point-density heat maps. |
| `haline` | Deep blue to bright green (cmocean); brackish water and estuaries. |
| `amp` | Light-to-deep red (cmocean); laser/radar intensity over dark basemaps. |
| `ylgnbu` | ColorBrewer Yellow-Green-Blue; density distributions and drainage. |
| `blues` | ColorBrewer single-hue blue; water depth, subtle underlays. |

**Diverging change detection** (pair with `stretch=minmax` or symmetric data so the midpoint lands on your baseline):

| Ramp | Best for |
|---|---|
| `rdbu` | The gold standard for change: red = loss/erosion, blue = gain/accumulation. |
| `brbg` | Brown-to-blue-green; bare soil versus vegetation/water shifts. |
| `coolwarm` | Balance-adjusted blue-to-red that tolerates hillshading and shadows. |
| `spectral` | Full-spectrum diverging ramp for deviations from a baseline. |
| `balance` | Perceptually uniform blue-white-red (cmocean) with a truly neutral center. |
| `seismic` | High-contrast deep-blue/white/deep-red; subsurface and elevation differences. |
| `roma` | Crameri diverging scheme tailored to topographic anomalies. |
| `berlin` | Dark-centered diverging map for dark-mode viewers. |
| `piyg` | Pink-to-yellow-green; NDVI-style vegetation anomalies. |

**Categorical classification**:

| Ramp | Best for |
|---|---|
| `las-classification` | ASPRS LAS class codes with their conventional colors (fixed 0–255 scale). |
| `dark2` | Distinct muted dark tones; urban feature separation. |
| `paired` | Light/dark hue pairs; nested classes like low vs. high vegetation. |
| `set2` | Pastel palette that keeps overlaid labels legible. |
| `accent` | Makes selected categories pop against neutral basemaps. |

Categorical ramps map equal-width value bands to discrete colors: combine them with `stretch=minmax` (or data with a known range) so category codes land on stable bands. `las-classification` needs nothing extra, as it is pinned to the absolute 0–255 class range.

</details>

### GeoTIFF Colorization

`--geotiff-colorize` colors points from an RGB/RGBA GeoTIFF orthophoto, sampled at each point's location and reprojected on the fly when the CRSs differ. Points outside the orthophoto keep their original colors.

```bash
gotiler -o ./out --geotiff-colorize ./ortho.tif ./input.las
```

### Placing Ungeoreferenced Point Clouds

Input files without a CRS normally fail. With `--crs local`, their coordinates are treated as a local Z-up cartesian system in meters and placed on the WGS84 ellipsoid, by default with the origin at longitude 0, latitude 0, height 0 and the axes aligned east-north-up. These flags, accepted only with `--crs local`, move and orient the model:

| Flag | Default | Effect |
|---|---:|---|
| `--longitude`, `--latitude` | `0` | Position of the model origin, in EPSG:4326 degrees. |
| `--height` | `0` | Height of the origin in meters above the WGS84 ellipsoid. |
| `--heading` | `0` | Rotation in degrees from local north, positive eastward. |
| `--pitch` | `0` | Rotation in degrees from the local east-north plane, positive up. |
| `--roll` | `0` | Rotation in degrees about the local east axis. |
| `--scale`, `-s` | `1` | Uniform scale; input units are assumed to be meters. |
| `--input-up-axis` | `z` | Axis treated as up: `x`, `y` or `z`. `y` is common for glTF/CAD-derived data. |

```bash
gotiler -o ./out --crs local \
  --longitude 12.492 --latitude 41.890 --height 76 \
  --heading 45 --input-up-axis y ./scan.las
```

Heading, pitch and roll follow the CesiumJS convention. The tileset root transform carries the placement, so viewers need nothing special. `--geotiff-colorize` works too, as long as the placement puts the cloud at its true geographic location.

### More Examples

```bash
# Folder with a compound CRS (vertical datum included), 3D Tiles 1.0 output
gotiler -o ./out -c EPSG:32633+3855 --version 1.0 ./las_folder

# Folder merged into one tileset, REPLACE refinement, 8-bit colors
gotiler -o ./out -c EPSG:28355 --join --refine-mode replace --8-bit ./las_folder

# Folder merged and colorized from an orthophoto, uncompressed output
gotiler -o ./out --join --geotiff-colorize ./ortho.tif --compression none ./las_folder

# Change detection: a difference attribute on a diverging ramp over its full range
gotiler -o ./out --colorize dz:rdbu:stretch=minmax ./diff.las
```

## 📚 Using GoTiler as a Go library

The tiling engine, readers, encoders and plugins are importable Go packages of the `github.com/mfbonfigli/gotiler/v3` module. See [LIBRARY.md](LIBRARY.md) for the API and examples, and [DEVELOPMENT.md](DEVELOPMENT.md) for building from source.

## License

GoTiler is distributed under the GNU AGPLv3, see [LICENSE.md](LICENSE.md). The executables include third-party code and data: see [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) for their licenses. Each release archive ships the copy generated for its platform.

## 💼 Commercial Licensing

If the AGPLv3 terms don't fit your use case, for example to embed the engine in proprietary software or services, commercial licenses are available: please get in touch with the maintainer through [GitHub](https://github.com/mfbonfigli).

## Acknowledgments

- [Photorealistic and classified urban point cloud dataset (Helsinki)](https://doi.org/10.5281/zenodo.5578198) by Julin, A., & Rantanen, T. (2021), used for the benchmarks and, thinned, for the demo animation. Licensed under [CC-BY-4.0](https://creativecommons.org).
- The Go gopher mascot was originally designed by Renee French and is licensed under the Creative Commons 4.0 Attribution License.

## Disclaimer

This project is an independent utility and is not affiliated with, sponsored by, or endorsed by Cesium GS, Inc. "Cesium" is a registered trademark of Cesium GS, Inc.
