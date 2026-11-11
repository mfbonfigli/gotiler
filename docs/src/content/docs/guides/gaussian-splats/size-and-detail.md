---
title: Size and detail
description: Choose the spherical-harmonic degree, the opacity threshold, the tile size and the geometric errors of a Gaussian splat tileset.
---

Large scenes run out of viewer memory before they run out of detail, so the defaults of `gotiler splat` keep what shows from afar:

- `--max-sh-degree` defaults to `0`, which keeps the base color of each splat and drops the colors that change with the view direction, such as reflections. CesiumJS takes 32 bytes per splat for everything else, plus 24 bytes at degree 1 and 120 bytes at degree 3. `--max-sh-degree 3` keeps whatever the input holds.
- `--min-opacity` defaults to `0.05`, which drops the almost invisible splats. In one trained scene of a million splats they were a quarter of the splats, but covered less than 1% of the area that the splats cover. `--min-opacity 0` keeps them all.
- `--splats-per-tile` sets the most splats a tile holds, 20,000 by default.
- `--ge-correction` scales the geometric errors of the tiles: values above 1 make viewers switch to finer tiles from farther away, sharper but heavier, and values below 1 only up close.

## Levels of detail

The leaf tiles hold the splats of the input, after the opacity threshold. Each tile above them holds about half the splats of its two children, merged in pairs from theirs, so the root holds a coarse version of the whole scene. Merging needs no retraining and no source images: each pair becomes one splat with the same overall shape and color, and the pairs merged first are those that change the scene least.

The geometric error of a tile grows with the spacing of its splats. Viewers replace a tile by its children once its error covers too many pixels on screen, 16 in CesiumJS by default, so closer views load finer tiles. `--ge-correction` scales every error, and viewers can also change the pixel threshold, as CesiumJS does with `maximumScreenSpaceError`.
