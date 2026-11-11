---
title: Examples
description: Ready-to-run gotiler pcloud commands for common point cloud tiling jobs.
sidebar:
  order: 9
---

Ready-to-run commands for common jobs. Each one links the options it combines to the guide that explains them.

## Single files

```bash
# Tile a LAS, LAZ, E57 or PLY file into ./out
gotiler pcloud -o ./out ./input.las

# Set the CRS of an E57 scan
gotiler pcloud -o ./out --crs EPSG:32633 ./scan.e57

# Keep a quarter of the points, uncompressed output
gotiler pcloud -o ./out --subsample 25 --compression none ./input.las
```

See [Input formats](/gotiler/guides/input-formats/), [Coordinate systems](/gotiler/guides/coordinate-systems/) and [Tiles and output](/gotiler/guides/output/).

## Folders

```bash
# Folder with a compound CRS (vertical datum included), 3D Tiles 1.0 output
gotiler pcloud -o ./out -c EPSG:32633+3855 --version 1.0 ./las_folder

# Folder merged into one tileset, REPLACE refinement, 8-bit colors
gotiler pcloud -o ./out -c EPSG:28355 --join --refine-mode replace --8-bit ./las_folder

# Folder merged into one tileset, packed as a .3tz archive
gotiler pcloud -o ./out --join --3tz ./las_folder
```

## Colors and attributes

```bash
# Color points by elevation
gotiler pcloud -o ./out --colorize z:viridis ./input.las

# Folder merged and colorized from an orthophoto, uncompressed output
gotiler pcloud -o ./out --join --geotiff-colorize ./ortho.tif --compression none ./las_folder

# Change detection: a difference attribute on a diverging ramp over its full range
gotiler pcloud -o ./out --colorize dz:rdbu:stretch=minmax ./diff.las

# Export GPS time next to the default attributes
gotiler pcloud -o ./out --attributes intensity,classification,gps_time ./input.las
```

See [Colorizing points](/gotiler/guides/colorizing/) and [Attributes](/gotiler/guides/attributes/).

## Ungeoreferenced models

```bash
# Place a Y-up model with known coordinates
gotiler pcloud -o ./out --crs local \
  --longitude 12.492 --latitude 41.890 --height 76 \
  --heading 45 --input-up-axis y ./scan.las

# Tile a model, then place it in the browser
gotiler pcloud -o ./out --crs local --input-up-axis y --preview ./model.ply
```

See [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/).
