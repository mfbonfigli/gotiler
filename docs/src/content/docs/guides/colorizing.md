---
title: Colorizing points
description: Color points by an attribute or a coordinate with one of 36 color ramps, or from an RGB GeoTIFF orthophoto.
sidebar:
  order: 5
---

gotiler can replace the colors of the points while tiling, either from the values of an attribute or coordinate, or from an orthophoto.

## From an attribute or coordinate

`--colorize attribute:gradient[:modifier...]` replaces point colors using a numeric attribute or a local coordinate (`x`, `y` or `z`):

```bash
gotiler -o ./out --colorize z:viridis ./input.las
gotiler -o ./out --colorize classification:las-classification ./input.las
```

No bounds are needed: the gradient spans the 2nd to 98th percentile of the values found in the data, so outliers and skewed distributions (typical for intensity or amplitude) don't wash it out. Gradients with an absolute scale, like `las-classification`, are applied as is.

The [color ramps](/gotiler/reference/color-ramps/) reference lists all 36 gradients. Good starting points: `viridis` for elevation, `turbo` for intensity, `rdbu` with `stretch=minmax` for change detection, `las-classification` for class codes.

### Modifiers

Modifiers can be combined:

| Modifier             | Effect                                                                                           |
| -------------------- | ------------------------------------------------------------------------------------------------ |
| `reverse`            | Reverses the gradient color order.                                                               |
| `steps=N`            | Quantizes the gradient into N discrete color bands (contour-band look).                          |
| `stretch=pLow,pHigh` | Sets the percentile stretch (default `2,98`). `stretch=minmax` scales over the full value range. |
| `blend=A`            | Blends the gradient with the original point color (`1` = gradient only, `0.5` = even mix).       |

```bash
gotiler -o ./out --colorize intensity:turbo:reverse:steps=8:stretch=5,95 ./input.las
```

## From a GeoTIFF orthophoto

`--geotiff-colorize` colors points from an RGB/RGBA GeoTIFF orthophoto, sampled at each point's location and reprojected on the fly when the CRSs differ. Points outside the orthophoto keep their original colors.

```bash
gotiler -o ./out --geotiff-colorize ./ortho.tif ./input.las
```

:::note
Colors are sampled while tiling, at the position the points have then. To colorize an ungeoreferenced cloud placed with `--crs local`, put it at its true location with the [placement flags](/gotiler/guides/placing-ungeoreferenced-clouds/#placement-flags): moving it later in the viewer does not recolor it.
:::
