---
title: Tiles and output
description: Choose the 3D Tiles version, tile size, level-of-detail behavior, compression and packaging of gotiler's output.
sidebar:
  order: 6
---

gotiler writes a `tileset.json` and its tiles into the output folder given with `-o`. These options shape what it writes.

## 3D Tiles version

`--version` (or `-v`) selects the output format: `1.1` (default) writes glTF (`.glb`) tiles, `1.0` writes `.pnts` tiles for viewers that only read 3D Tiles 1.0.

```bash
gotiler pcloud -o ./out --version 1.0 ./input.las
```

## Tile size

`--points-per-tile` (or `-p`) sets the approximate number of points a tile should contain, from 5,000 to 5,000,000 (default 50,000). It is a target that regulates the average tile size: tiles with more or fewer points are still possible.

## Levels of detail

Viewers pick which tiles to show from their geometric error, which gotiler derives from the dataset:

- `--initial-geometric-error` sets the minimum target geometric error, in meters, of the root tile. Higher values produce a coarser root tile that is visible from farther away. `0` (default) derives it from the dataset size.
- `--ge-correction` multiplies every geometric error in `tileset.json` (default `1.0`). It controls at what distance viewers switch between levels: values above 1 make tiles appear from farther away, values below 1 only up close. It does not change how many levels are built or which points belong to each.

## Large tilesets

Viewers read the whole `tileset.json` before showing anything, and a cloud of billions of points has tens of thousands of tiles. A tileset of more than `--max-tiles-per-tileset` tiles (default 2,000) is split: `tileset.json` keeps the top levels of the tree, and each subtree below them goes into an external tileset in the `data` folder, such as `data/0101d.json`. The cloud then shows sooner, as viewers fetch each subtree only once the camera gets close to it.

Smaller tilesets keep a single `tileset.json`, and `--max-tiles-per-tileset 0` always writes one. External tilesets are part of the 3D Tiles standard, so CesiumJS and other 3D Tiles viewers read them.

## Refine mode

`--refine-mode` (or `-r`) chooses how levels of detail relate:

- `add` (default): each point is stored in exactly one level of detail, so the output is smaller and no point is downloaded twice. To show a tile, viewers must also load every coarser level above it.
- `replace`: each tile repeats the points of the coarser levels and stands on its own, so viewers can skip those levels and make fewer requests, at the cost of larger output. In CesiumJS this needs the `skipLevelOfDetail` tileset option, which is off by default.

## Compression

3D Tiles 1.1 (`.glb`) output is compressed by default with `EXT_meshopt_compression` and `KHR_mesh_quantization`, typically shrinking tilesets by 70% or more with no visible quality loss. CesiumJS supports both extensions; if your viewer doesn't, pass `--compression none`. 3D Tiles 1.0 (`.pnts`) output is never compressed.

`--meshopt-khr` uses `KHR_meshopt_compression` instead, the Khronos successor of the EXT extension, whose improved codec produces smaller tiles at the same quality. Viewer support is still limited (CesiumJS added it in version 1.143), so EXT remains the default.

```bash
gotiler pcloud -o ./out --compression none ./input.las
gotiler pcloud -o ./out --meshopt-khr ./input.las
```

## 3TZ archives

`--3tz` writes each tileset as a single OGC 3D Tiles Archive, `<out>/tileset.3tz`, instead of loose tile files. Folder inputs without `--join` get one archive per file, in `<out>/<name>/tileset.3tz`.

```bash
gotiler pcloud -o ./out --3tz ./input.las
```

## Subsampling

`--subsample` keeps the given percentage of the points, picked at random, to thin out huge datasets.

```bash
gotiler pcloud -o ./out --subsample 25 ./input.las
```

## Progress output

gotiler shows live progress bars while it works. `--plain` prints only milestone updates as plain text instead, which reads better in logs and CI jobs.
