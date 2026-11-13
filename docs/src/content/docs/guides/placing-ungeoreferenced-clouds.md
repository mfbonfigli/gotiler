---
title: Placing ungeoreferenced assets
description: Put point clouds without a coordinate system and Gaussian splat scenes on the globe, with placement flags or the browser viewer.
sidebar:
  order: 3
---

Scans in a local frame and trained Gaussian splat scenes have no coordinate system. gotiler treats their coordinates as a local cartesian system in meters and places it on the WGS84 ellipsoid through the root transform of the tileset, by default at longitude 0, latitude 0, height 0, with the axes aligned east-north-up:

- **Point clouds** are placed this way with `--crs local`; without it, input files without a CRS fail. The point placed is the origin of the model's coordinates.
- **Gaussian splats** are always placed this way, since `gotiler splats` takes no CRS. The point placed is the bottom center of the splats: the center of the box that holds them, at the height of its bottom.

`--input-up-axis` sets the input axis treated as up. Point clouds take `x`, `y` or `z`, default `z`; `y` is common for glTF, CAD and photogrammetry data. Gaussian splats take the same axes, optionally signed, such as `-y`, which scenes trained with OpenCV or COLMAP conventions usually need. The up axis is applied when tiling, so a model that comes out on its side or upside down needs another `--input-up-axis`.

There are two ways to put the model in place:

1. **Placement flags:** pass the longitude, latitude, height, heading, pitch, roll and scale on the command line. This suits models whose placement is known, and scripts or pipelines that run gotiler unattended.
2. **The browser viewer:** once tiling ends, move the model into place with the edit tools of the viewer page that opens in the browser. Point clouds need `--preview` for it, and `gotiler splats` opens it unless `--no-preview` is set.

The two approaches also combine: placement flags can put the model roughly in place and the viewer can refine it. To adjust the position again later, run `gotiler view ./out` and click **Edit position**.

## Placement flags

Point clouds accept these flags only with `--crs local`, Gaussian splats always:

| Flag                        | Default | Effect                                                            |
| --------------------------- | ------: | ----------------------------------------------------------------- |
| `--longitude`, `--latitude` |     `0` | Position of the point placed, in EPSG:4326 degrees.               |
| `--height`                  |     `0` | Height of the point placed in meters above the WGS84 ellipsoid.   |
| `--heading`                 |     `0` | Rotation in degrees from local north, positive eastward.          |
| `--pitch`                   |     `0` | Rotation in degrees from the local east-north plane, positive up. |
| `--roll`                    |     `0` | Rotation in degrees about the local east axis.                    |
| `--scale`, `-s`             |     `1` | Uniform scale that turns the input units into meters.             |

```bash
gotiler pcloud -o ./out --crs local \
  --longitude 12.492 --latitude 41.890 --height 76 \
  --heading 45 --input-up-axis y ./scan.las

gotiler splats -o ./out \
  --longitude 12.492 --latitude 41.890 --height 20 \
  --scale 1.5 --input-up-axis -y ./scene.ply
```

Heading, pitch and roll follow the CesiumJS convention. Trained splat scenes often have arbitrary units: `--scale` sets the factor that makes them meters.

## Placing in the viewer

```bash
gotiler pcloud -o ./out --crs local --input-up-axis y --preview ./model.ply
gotiler splats -o ./out --input-up-axis -y ./scene.ply
```

When tiling ends, gotiler opens the [viewer](/gotiler/guides/viewer/) in the browser with its position panel and gizmo already shown:

1. Search an address, or click **Pick position** and then the globe, to move the model's center there.
2. Fine-tune the position and orientation by dragging the gizmo's arrows and rings, or by typing offsets and rotations in the panel.
3. Click **Save position**, which writes the new root transform into `./out/tileset.json`, then close the page.

The viewer moves and rotates the model but cannot scale it, so pass `--scale` when tiling if the input is not in meters.
