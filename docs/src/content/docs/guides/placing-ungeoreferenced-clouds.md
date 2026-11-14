---
title: Placing ungeoreferenced assets
description: Put point clouds without a coordinate system and Gaussian splat scenes on the globe, with placement flags or the browser viewer.
sidebar:
  order: 3
---

Scans in a local frame and trained Gaussian splat scenes have no coordinate system. gotiler puts them on the globe at the position you set, or at longitude 0, latitude 0 and height 0 if you set none, with their axes pointing east, north and up, and their units taken as meters:

- **Point clouds** need `--crs local` for this, otherwise files without a CRS are rejected. The position you set is where the origin of their coordinates goes.
- **Gaussian splats** are always placed this way. The position you set is where the bottom center of the scene goes.

`--input-up-axis` sets the input axis treated as up. Point clouds take `x`, `y` or `z`, default `z`; `y` is common for glTF, CAD and photogrammetry data. Gaussian splats take the same axes, optionally signed, such as `-y`, which scenes trained with OpenCV or COLMAP conventions usually need. If a model comes out on its side or upside down, tile it again with another `--input-up-axis`.

There are two ways to put the model in place:

1. **Placement flags:** pass the longitude, latitude, height, heading, pitch, roll and scale on the command line. This suits models whose placement is known, and scripts or pipelines that run gotiler unattended.
2. **The viewer:** once tiling ends, drag the model into place in the browser. Point clouds need `--preview` for it, and `gotiler splats` opens the viewer unless you pass `--no-preview`.

The two approaches also combine: placement flags can put the model roughly in place and the viewer can refine it. To adjust the position again later, run `gotiler view ./out` and click **Edit position**.

## Placement flags

Point clouds accept these flags only with `--crs local`, Gaussian splats always:

| Flag                        | Default | Effect                                                            |
| --------------------------- | ------: | ----------------------------------------------------------------- |
| `--longitude`, `--latitude` |     `0` | Where the model goes, in EPSG:4326 degrees.                       |
| `--height`                  |     `0` | Its height in meters above the WGS84 ellipsoid.                   |
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
3. Click **Save position**, which stores the position in `./out/tileset.json`, then close the page.

The viewer moves and rotates the model but cannot scale it, so pass `--scale` when tiling if the input is not in meters.
