---
title: Input formats and placement
description: Tile 3D Gaussian splat PLY files into 3D Tiles 1.1 for CesiumJS with gotiler splats, and place them on the globe.
---

`gotiler splats` tiles a PLY file of 3D Gaussian splats into a 3D Tiles 1.1 tileset, which CesiumJS streams and draws as splats:

```bash
gotiler splats -o ./out ./scene.ply
```

After tiling, the [viewer](/gotiler/guides/viewer/) opens on the result with the position panel shown, to check the tileset and move it into place. `--no-preview` skips it, and it does not open when the output is not a terminal, as in scripts and containers.

## Input

gotiler reads the PLY layout that the reference 3D Gaussian Splatting implementation writes, and that most trainers and editors export. Each vertex is a splat, with these properties:

- `x`, `y`, `z`: the center
- `f_dc_0` to `f_dc_2`: the base color, as degree-0 spherical harmonics
- `opacity`: the opacity, before the sigmoid
- `scale_0` to `scale_2`: the size along the splat's axes, as logarithms
- `rot_0` to `rot_3`: the rotation, as a quaternion in W, X, Y, Z order
- `f_rest_0` and up, optionally: 9, 24 or 45 coefficients for spherical harmonics of degree 1, 2 or 3

Both ASCII and binary PLY files work. Splats with invalid values are skipped, and the summary at the end says how many. Compressed splat PLY files, with packed properties, are not supported yet: export the scene as a standard splat PLY.

## Placement

Trained scenes have no coordinate system, so you place them on the globe yourself, with the placement flags or in the viewer: see [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/). `--input-up-axis` names the input axis that points up, optionally signed. Scenes trained with OpenCV or COLMAP conventions point down their Y axis, and usually need `-y`:

```bash
gotiler splats -o ./out --input-up-axis -y --longitude 12.4924 --latitude 41.8902 --height 20 ./scene.ply
```

You can move the scene again at any time with `gotiler view ./out`, without tiling it again.
