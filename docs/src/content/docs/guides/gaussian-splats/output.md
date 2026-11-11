---
title: Output
description: The tileset that gotiler splat writes, and how to show it in CesiumJS.
---

The output folder holds `tileset.json` and the tiles in its `data` folder. Each tile is a glTF file of SPZ-compressed splats, with the `KHR_gaussian_splatting` and `KHR_gaussian_splatting_compression_spz_2` extensions. The tileset declares both as required, since CesiumJS draws the tiles as splats only then, and as points otherwise.

Tiles refine by replacement: a tile's children replace it when seen up close. The placement on the globe lives only in the root transform, which the [viewer](/gotiler/guides/viewer/) rewrites when it saves a new position.

The input does not need to fit in memory: gotiler works on temporary files in the output folder and removes them when it ends, even after an error or Ctrl+C.

## Showing the tileset in CesiumJS

To show the tileset in your own CesiumJS application, serve the output folder over HTTP and load its `tileset.json`:

```js
const tileset = await Cesium.Cesium3DTileset.fromUrl('https://example.com/out/tileset.json');
viewer.scene.primitives.add(tileset);
```

CesiumJS needs to support the `KHR_gaussian_splatting_compression_spz_2` extension; the gotiler viewer uses CesiumJS 1.146.
