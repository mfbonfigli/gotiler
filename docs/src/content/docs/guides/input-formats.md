---
title: Input formats
description: The point cloud formats gotiler reads, LAS, LAZ, E57 and PLY, and what it takes from each.
sidebar:
  order: 1
---

gotiler reads LAS, LAZ, E57 and PLY point clouds, from a single file or from every file of a folder.

## LAS and LAZ

Every LAS/LAZ version (1.0–1.4) and point format is supported. gotiler reads the coordinate system from the file's GeoTIFF or WKT metadata, so `--crs` is only needed when the file has none or declares the wrong one.

- Colors are read as 16-bit values. Pass `--8-bit` when a file stores 8-bit colors instead.
- Points flagged as withheld are dropped. Pass `--include-withheld` to keep them.
- Intensity and classification are exported by default, and any other attribute, including LAS extra bytes, on request: see [Attributes](/gotiler/guides/attributes/).

## E57

`.e57` scans from terrestrial laser scanners are read natively (experimental). gotiler doesn't read a CRS from E57 files, so pass one with `--crs`, or use `--crs local` and the [placement flags](/gotiler/guides/placing-ungeoreferenced-clouds/#placement-flags) for scans in a local frame:

```bash
gotiler pcloud -o ./out --crs EPSG:32633 ./scan.e57
```

Standard E57 fields (intensity, timestamp, normals, invalid flags, row/column indices, …) and any extension field declared in the scan prototypes can be exported with `--attributes`.

## PLY

`.ply` point clouds are read in ASCII and binary (little- and big-endian) encoding. gotiler tiles the vertices and ignores faces and other elements, so a mesh is tiled as its vertices. PLY files carry no CRS: pass one with `--crs`, or use `--crs local` and the [placement flags](/gotiler/guides/placing-ungeoreferenced-clouds/#placement-flags) for clouds in a local frame. Photogrammetry and graphics tools often write Y-up clouds, which `--input-up-axis y` turns upright:

```bash
gotiler pcloud -o ./out --crs EPSG:32633 ./cloud.ply
gotiler pcloud -o ./out --crs local --latitude 41.89 --longitude 12.49 --input-up-axis y ./model.ply
```

Colors come from the `red`/`green`/`blue` properties, or `diffuse_red`/`diffuse_green`/`diffuse_blue`, scaled to 8 bits according to their type. Any other vertex property can be exported with `--attributes`, keeping its PLY type. CloudCompare's `scalar_` prefix is optional (`intensity` finds `scalar_Intensity`), and normals can be requested as `nx` or `normal_x`. Vertices with non-finite coordinates are skipped.

:::note
Gaussian splat PLY files are not point clouds: tile them with `gotiler splats`, see [Gaussian splats](/gotiler/guides/gaussian-splats/input-and-placement/).
:::

## Folders

Pass a folder instead of a file to tile every point cloud file in it. Each file becomes its own tileset, in a subfolder of the output folder named after the file. With `--join`, all the files are merged into one tileset instead: they must then share the same properties, such as their CRS.

```bash
gotiler pcloud -o ./out ./las_folder
gotiler pcloud -o ./out --join ./las_folder
```

Folders are not scanned recursively: files in subfolders are ignored.
