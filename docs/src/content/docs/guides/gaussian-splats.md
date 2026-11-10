---
title: Gaussian splats
description: Tile 3D Gaussian splat PLY files into 3D Tiles 1.1 for CesiumJS with gotiler splat, and place them on the globe.
sidebar:
  order: 8
---

`gotiler splat` tiles a PLY file of 3D Gaussian splats into a 3D Tiles 1.1 tileset, which CesiumJS streams and draws as splats:

```bash
gotiler splat -o ./out ./scene.ply
```

After tiling, the [viewer](/gotiler/guides/viewer/) opens on the result with the position panel shown, to check the tileset and move it into place. `--no-preview` skips it, and it does not open when the output is not a terminal, as in scripts and containers.

## Input

The PLY file holds one vertex per splat with the properties that the reference 3D Gaussian Splatting implementation writes, and that most trainers and editors export:

- `x`, `y`, `z`: the center
- `f_dc_0` to `f_dc_2`: the base color, as degree-0 spherical harmonics
- `opacity`: the opacity, before the sigmoid
- `scale_0` to `scale_2`: the size along the splat's axes, as logarithms
- `rot_0` to `rot_3`: the rotation, as a quaternion in W, X, Y, Z order
- `f_rest_0` and up, optionally: 9, 24 or 45 coefficients for spherical harmonics of degree 1, 2 or 3

ASCII and binary PLY files are read. Vertices with non-finite values are skipped, and the summary at the end counts them. Compressed splat PLY files, with packed properties, are not supported yet: export the scene as a standard splat PLY.

## Placing the splats

Trained scenes carry no coordinate system, so gotiler places them on the globe through the root transform of the tileset. `--longitude`, `--latitude` and `--height` put the bottom center of the splats there, and `--heading`, `--pitch` and `--roll` rotate them. Trainers often work in arbitrary units: `--scale` sets the factor that turns them into meters.

`--input-up-axis` names the input axis that points up, `x`, `y` or `z`, optionally signed. Scenes trained with OpenCV or COLMAP conventions point down their Y axis, and usually need `-y`:

```bash
gotiler splat -o ./out --input-up-axis -y --longitude 12.4924 --latitude 41.8902 --height 20 ./scene.ply
```

Since the position lives only in the root transform, the viewer can move the tileset after tiling without rewriting its tiles, then or later with `gotiler view ./out`. A scene that comes out upside down or on its side needs another `--input-up-axis`.

## Size and detail

Large scenes run out of viewer memory before they run out of detail, so the defaults keep what shows from afar:

- `--max-sh-degree` defaults to `0`, which keeps the base color of each splat and drops the colors that change with the view direction, such as reflections. CesiumJS takes 32 bytes per splat for everything else, plus 24 bytes at degree 1 and 120 bytes at degree 3. `--max-sh-degree 3` keeps whatever the input holds.
- `--min-opacity` defaults to `0.05`, which drops the almost invisible splats. In one trained scene of a million splats they were a quarter of the splats, but covered less than 1% of the area that the splats cover. `--min-opacity 0` keeps them all.
- `--splats-per-tile` sets the most splats a tile holds, 20,000 by default.
- `--ge-correction` scales the geometric errors of the tiles: values above 1 make viewers switch to finer tiles from farther away, sharper but heavier, and values below 1 only up close.

## Output

The output folder holds `tileset.json` and the tiles in its `data` folder. Each tile is a glTF file of SPZ-compressed splats, with the `KHR_gaussian_splatting` and `KHR_gaussian_splatting_compression_spz_2` extensions. The tileset declares both as required, since CesiumJS draws the tiles as splats only then, and as points otherwise.

The leaf tiles hold the splats of the input. Each tile above them holds about half the splats of its two children, merged from theirs, and replaces them when seen from afar. The input does not need to fit in memory: gotiler works on temporary files in the output folder and removes them when it ends.
