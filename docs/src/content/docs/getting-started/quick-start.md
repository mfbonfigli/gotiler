---
title: Quick start
description: Tile a first point cloud with gotiler and look at the result on a 3D globe.
sidebar:
  order: 2
---

This page assumes gotiler is [installed](/gotiler/getting-started/installation/) and on your `PATH`.

## Tile a file

Pass the output folder with `-o` and the input file:

```bash
gotiler pcloud -o ./out ./input.las
```

gotiler writes `tileset.json` and its tiles into `./out`. LAS and LAZ files usually carry their coordinate system, which gotiler reads on its own. E57 and PLY files carry none, so pass one with `--crs`: see [Coordinate systems](/gotiler/guides/coordinate-systems/).

## Tile a folder

Pass a folder to tile every point cloud file in it, each into its own tileset in a subfolder of the output named after the file. With `--join` the files are merged into a single tileset, here also packed into one `.3tz` archive:

```bash
gotiler pcloud -o ./out ./las_folder
gotiler pcloud -o ./out --join --3tz ./las_folder
```

Folders are not scanned recursively: files in subfolders are ignored.

## Look at the result

`gotiler view` opens a tileset on a 3D globe in the browser:

```bash
gotiler view ./out
```

Add `--preview` to a tiling command to open the viewer as soon as tiling ends. See [Viewing and positioning](/gotiler/guides/viewer/) for what the viewer can do.

To show the tileset in your own CesiumJS application, serve the output folder over HTTP and load its `tileset.json`:

```js
const tileset = await Cesium.Cesium3DTileset.fromUrl('https://example.com/out/tileset.json');
viewer.scene.primitives.add(tileset);
```

## Next steps

- `gotiler help` lists the commands, `gotiler help pcloud` the flags of one, and `gotiler version` prints the version.
- The [Point clouds](/gotiler/guides/input-formats/) guides explain the input formats, coordinate systems, attributes, colors and output options, and the [Gaussian splats](/gotiler/guides/gaussian-splats/input-and-placement/) guides the tiling of splat scenes.
- The [CLI flags](/gotiler/reference/cli-flags/) reference lists every option on one page.
