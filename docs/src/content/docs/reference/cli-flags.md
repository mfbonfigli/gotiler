---
title: CLI flags
description: Every command, flag and environment variable of the gotiler CLI.
sidebar:
  order: 1
---

## Commands

| Command                             | Description                                                          |
| ----------------------------------- | -------------------------------------------------------------------- |
| `gotiler pcloud [flags] <input>`    | Tile a point cloud file, or every file of a folder.                  |
| `gotiler splats [flags] <splats.ply>` | Tile a Gaussian splat PLY file, see [Gaussian splats](/gotiler/guides/gaussian-splats/input-and-placement/). |
| `gotiler view [flags] <tileset>`    | Show a tileset on a 3D globe in the browser and move it into place.  |
| `gotiler version`                   | Print the gotiler version.                                           |
| `gotiler help [command]`            | List the commands, or the flags of one command.                      |

`gotiler [flags] <input>`, without a command, runs `gotiler pcloud` and takes its flags, as gotiler did before it had commands.

## Point cloud flags

The flags of `gotiler pcloud`.

| Flag                        | Default                    | Description |
| --------------------------- | -------------------------- | ----------- |
| `--out`, `-o`               | _required_                 | Output folder for the generated tilesets. |
| `--crs`, `-c`               | autodetect                 | Input CRS: an EPSG code such as `EPSG:4326` (bare numbers work too), a compound code with a vertical datum such as `EPSG:32633+3855`, a Proj4 string or WKT. Autodetected from LAS/LAZ metadata when omitted; required for E57 and PLY. `local` accepts ungeoreferenced input, see [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/). |
| `--z-offset`, `-z`          | `0`                        | Vertical offset in meters, see [Coordinate systems](/gotiler/guides/coordinate-systems/#heights). |
| `--points-per-tile`, `-p`   | `50000`                    | Target points per tile, from 5,000 to 5,000,000. |
| `--refine-mode`, `-r`       | `add`                      | Tile refinement: `add` or `replace`, see [Refine mode](/gotiler/guides/output/#refine-mode). |
| `--8-bit`                   | `false`                    | Treat LAS/LAZ colors as 8-bit instead of 16-bit. |
| `--version`, `-v`           | `1.1`                      | 3D Tiles version: `1.0` (`.pnts`) or `1.1` (`.glb`). |
| `--initial-geometric-error` | `0`                        | Root geometric error target in meters; `0` derives it from the dataset, see [Levels of detail](/gotiler/guides/output/#levels-of-detail). |
| `--ge-correction`           | `1.0`                      | Multiplier applied to all output geometric errors. |
| `--max-tiles-per-tileset`   | `2000`                     | Most tiles a tileset JSON file holds; larger tilesets move subtrees into external tilesets, see [Large tilesets](/gotiler/guides/output/#large-tilesets). `0` writes a single `tileset.json`. |
| `--attributes`              | `intensity,classification` | Per-point attributes to export, or `none`, see [Attributes](/gotiler/guides/attributes/). |
| `--include-withheld`        | `false`                    | Keep points flagged as withheld, which are dropped by default. |
| `--colorize`                |                            | Color points by attribute or coordinate, e.g. `z:viridis`, see [Colorizing points](/gotiler/guides/colorizing/). |
| `--geotiff-colorize`        |                            | Color points from an RGB/RGBA GeoTIFF orthophoto, see [Colorizing points](/gotiler/guides/colorizing/#from-a-geotiff-orthophoto). |
| `--compression`             | `meshopt`                  | `meshopt` or `none`, see [Compression](/gotiler/guides/output/#compression). |
| `--meshopt-khr`             | `false`                    | Use `KHR_meshopt_compression` instead of `EXT_meshopt_compression`. |
| `--subsample`               | `100`                      | Percentage of points to keep, in (0, 100]. |
| `--3tz`                     | `false`                    | Write each tileset as a single `.3tz` archive, see [3TZ archives](/gotiler/guides/output/#3tz-archives). |
| `--join`, `-j`              | `false`                    | Merge all files of an input folder into one tileset. |
| `--plain`                   | `false`                    | Print plain progress messages instead of progress bars. |
| `--preview`                 | `false`                    | After tiling, open the tileset in the browser viewer, see [Viewing and positioning](/gotiler/guides/viewer/). Needs a single tileset: a file input, or a folder with `--join`, without `--3tz`. |
| `--port`                    | `0`                        | With `--preview`: local port of the viewer; `0` picks a free one. |
| `--help`, `-h`              |                            | Show help. |

### Placement flags

Only with `--crs local`, see [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/#placement-flags):

| Flag                        | Default | Description                                                         |
| --------------------------- | ------: | ------------------------------------------------------------------- |
| `--longitude`, `--latitude` |     `0` | Position of the model origin, in EPSG:4326 degrees.                 |
| `--height`                  |     `0` | Height of the origin in meters above the WGS84 ellipsoid.           |
| `--heading`                 |     `0` | Rotation in degrees from local north, positive eastward.            |
| `--pitch`                   |     `0` | Rotation in degrees from the local east-north plane, positive up.   |
| `--roll`                    |     `0` | Rotation in degrees about the local east axis.                      |
| `--scale`, `-s`             |     `1` | Uniform scale; input units are assumed to be meters.                |
| `--input-up-axis`           |     `z` | Input axis treated as up: `x`, `y` or `z`.                          |

## Splat flags

The flags of `gotiler splats`, see [Gaussian splats](/gotiler/guides/gaussian-splats/input-and-placement/).

| Flag                        | Default    | Description |
| --------------------------- | ---------- | ----------- |
| `--out`, `-o`               | _required_ | Output folder of the tileset. |
| `--splats-per-tile`         | `40000`    | Most splats a tile holds, from 1,000 to 1,000,000. |
| `--max-sh-degree`           | `0`        | Highest spherical-harmonic degree to keep, from 0 to 3. Higher degrees keep the colors that change with the view direction, but take much more memory in viewers; `3` keeps the input's. |
| `--min-opacity`             | `0.05`     | Drop the splats of lower opacity, from 0 to 1; `0` keeps them all. |
| `--ge-correction`           | `1`        | Multiplier applied to all output geometric errors. |
| `--max-tiles-per-tileset`   | `2000`     | Most tiles a tileset JSON file holds; larger tilesets move subtrees into external tilesets, see [Output](/gotiler/guides/gaussian-splats/output/). `0` writes a single `tileset.json`. |
| `--input-up-axis`           | `z`        | Input axis that points up: `x`, `y` or `z`, optionally signed, such as `-y`. |
| `--longitude`, `--latitude` | `0`        | Position of the bottom center of the splats, in EPSG:4326 degrees. |
| `--height`                  | `0`        | Height of the bottom center in meters above the WGS84 ellipsoid. |
| `--heading`                 | `0`        | Rotation in degrees from local north, positive eastward. |
| `--pitch`                   | `0`        | Rotation in degrees from the local east-north plane, positive up. |
| `--roll`                    | `0`        | Rotation in degrees about the local east axis. |
| `--scale`, `-s`             | `1`        | Uniform scale of the splats, which makes the input units meters. |
| `--no-preview`              | `false`    | Do not open the viewer after tiling. Without it, the viewer opens unless the output is not a terminal. |
| `--port`                    | `0`        | Local port of the viewer; `0` picks a free one. |
| `--plain`                   | `false`    | Print plain progress messages instead of progress bars. |
| `--help`, `-h`              |            | Show help. |

## View flags

`gotiler view` takes the `tileset.json` to show, or the folder that holds it. See [Viewing and positioning](/gotiler/guides/viewer/).

| Flag     | Default | Description                                     |
| -------- | ------: | ----------------------------------------------- |
| `--port` |     `0` | Local port of the viewer; `0` picks a free one. |

## Environment variables

| Variable           | Description |
| ------------------ | ----------- |
| `CESIUM_ION_TOKEN` | Cesium ion access token for the viewer, used by `gotiler view`, `--preview` and `gotiler splats`: the viewer then shows Cesium World Imagery and World Terrain instead of the default satellite imagery. There is no flag for it, so the token stays out of shell histories and process listings. |
