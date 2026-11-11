---
title: Examples
description: Ready-to-run gotiler splat commands for common Gaussian splat tiling jobs.
---

Ready-to-run commands for common jobs. Each one links the options it combines to the guide that explains them.

## Placing scenes

```bash
# Tile a scene, then place it on the globe in the viewer
gotiler splat -o ./out ./scene.ply

# A scene trained with OpenCV or COLMAP axes, placed in Rome and turned 30 degrees
gotiler splat -o ./out --input-up-axis -y \
  --longitude 12.4924 --latitude 41.8902 --height 20 --heading 30 ./scene.ply

# A scene in arbitrary units, 2.5 of them to the meter
gotiler splat -o ./out --scale 0.4 ./scene.ply

# Place an existing tileset again
gotiler view ./out
```

See [Input formats and placement](/gotiler/guides/gaussian-splats/input-and-placement/) and [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/).

## Size and detail

```bash
# Keep the colors that change with the view direction, for close-up scenes
gotiler splat -o ./out --max-sh-degree 3 ./scene.ply

# Keep every splat, however faint
gotiler splat -o ./out --min-opacity 0 ./scene.ply

# Switch to finer tiles from twice as far, or lighter tiles from half as far
gotiler splat -o ./out --ge-correction 2 ./scene.ply
gotiler splat -o ./out --ge-correction 0.5 ./scene.ply
```

See [Size and detail](/gotiler/guides/gaussian-splats/size-and-detail/).

## Scripts and containers

```bash
# No viewer and no progress bars
gotiler splat -o ./out --no-preview --plain ./scene.ply
```

The viewer also stays closed by itself when the output is not a terminal.
