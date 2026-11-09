---
title: Placing ungeoreferenced point clouds
description: Tile point clouds that have no coordinate system with --crs local, and put them on the globe with placement flags or the browser viewer.
sidebar:
  label: Placing ungeoreferenced clouds
  order: 3
---

Input files without a CRS normally fail. With `--crs local`, their coordinates are treated as a local Z-up cartesian system in meters and placed on the WGS84 ellipsoid, by default with the origin at longitude 0, latitude 0, height 0 and the axes aligned east-north-up. `--input-up-axis` (`x`, `y` or `z`, default `z`) sets the input axis treated as up; `y` is common for glTF, CAD and photogrammetry data.

There are two ways to put the model in place:

1. **Placement flags:** pass the longitude, latitude, height, heading, pitch, roll and scale on the command line. This suits models whose placement is known, and scripts or pipelines that run gotiler unattended.
2. **The browser viewer:** add `--preview`, and once tiling ends, move the model into place with the edit tools of the viewer page that opens in the browser.

The two approaches also combine: placement flags can put the model roughly in place and the viewer can refine it. To adjust the position again later, run `gotiler view ./out` and click **Edit position**.

## Placement flags

These flags are accepted only with `--crs local`:

| Flag                        | Default | Effect                                                            |
| --------------------------- | ------: | ----------------------------------------------------------------- |
| `--longitude`, `--latitude` |     `0` | Position of the model origin, in EPSG:4326 degrees.               |
| `--height`                  |     `0` | Height of the origin in meters above the WGS84 ellipsoid.         |
| `--heading`                 |     `0` | Rotation in degrees from local north, positive eastward.          |
| `--pitch`                   |     `0` | Rotation in degrees from the local east-north plane, positive up. |
| `--roll`                    |     `0` | Rotation in degrees about the local east axis.                    |
| `--scale`, `-s`             |     `1` | Uniform scale; input units are assumed to be meters.              |

```bash
gotiler -o ./out --crs local \
  --longitude 12.492 --latitude 41.890 --height 76 \
  --heading 45 --input-up-axis y ./scan.las
```

Heading, pitch and roll follow the CesiumJS convention.

## Placing in the viewer

```bash
gotiler -o ./out --crs local --input-up-axis y --preview ./model.ply
```

When tiling ends, gotiler opens the [viewer](/gotiler/guides/viewer/) in the browser with its position panel and gizmo already shown:

1. Search an address, or click **Pick position** and then the globe, to move the model's center there.
2. Fine-tune the position and orientation by dragging the gizmo's arrows and rings, or by typing offsets and rotations in the panel.
3. Click **Save position**, which writes the new root transform into `./out/tileset.json`, then close the page.

The viewer moves and rotates the model but cannot scale it, so pass `--scale` when tiling if the input is not in meters.
