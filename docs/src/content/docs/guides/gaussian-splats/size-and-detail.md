---
title: Size and detail
description: Choose the spherical-harmonic degree, the opacity threshold, the tile size and the geometric errors of a Gaussian splat tileset.
---

In large scenes, viewer memory runs out before visible detail does, so the defaults of `gotiler splats` keep tilesets light:

- `--max-sh-degree` defaults to `0`, which keeps the base color of each splat and drops the colors that change with the view direction, such as reflections. Those take a lot of viewer memory: at degree 3, each splat takes about five times as much. `--max-sh-degree 3` keeps whatever the input holds.
- `--min-opacity` defaults to `0.05`, which drops the almost invisible splats: in trained scenes they can be a large share of the splats while adding almost nothing to the picture. `--min-opacity 0` keeps them all.
- `--splats-per-tile` sets the most splats a tile holds, 40,000 by default.
- `--ge-correction` scales the geometric errors of the tiles: values above 1 make viewers switch to finer tiles from farther away, sharper but heavier, and values below 1 only up close.

## Levels of detail

The finest tiles hold every splat of the input that passes the opacity threshold. Each level above holds a simplified version of the scene with about half as many splats, up to a coarse version of the whole scene at the root, so viewers show the scene from afar right away and stream detail as you get closer. The simplification needs no retraining and no source images: gotiler merges nearby splats into larger ones, starting with the merges that change the scene least.

The geometric error of a tile grows with the spacing of its splats. Viewers replace a tile by its children once its error covers too many pixels on screen, 16 in CesiumJS by default, so closer views load finer tiles. `--ge-correction` scales every error, and viewers can also change the pixel threshold, as CesiumJS does with `maximumScreenSpaceError`.
