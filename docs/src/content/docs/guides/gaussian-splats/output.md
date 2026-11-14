---
title: Output
description: The tileset that gotiler splats writes, and how to show it in CesiumJS.
---

The output folder holds `tileset.json` and the tiles in its `data` folder. Each tile is a glTF file of SPZ-compressed splats, using the `KHR_gaussian_splatting` and `KHR_gaussian_splatting_compression_spz_2` extensions. Tiles refine by replacement: up close, a tile's children replace it.

A tileset of more than `--max-tiles-per-tileset` tiles (default 2,000) is split: `tileset.json` keeps the top levels of the tree, and each subtree below them goes into a file of its own in the `data` folder, such as `data/tile_0101.json`. The scene then shows sooner, as viewers fetch each subtree only once the camera gets close to it. `--max-tiles-per-tileset 0` always writes a single `tileset.json`.

Scenes larger than your computer's memory tile fine, but need free disk space in the output folder for temporary files, which gotiler removes when it ends.

## Showing the tileset in CesiumJS

To show the tileset in your own CesiumJS application, serve the output folder over HTTP and load its `tileset.json`:

```js
const tileset = await Cesium.Cesium3DTileset.fromUrl('https://example.com/out/tileset.json');
viewer.scene.primitives.add(tileset);
```

This needs a CesiumJS release that supports the `KHR_gaussian_splatting_compression_spz_2` extension, such as 1.147, which the gotiler viewer uses.
