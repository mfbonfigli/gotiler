---
title: Viewing and positioning
description: Look at a tileset on a 3D globe with gotiler view, and move it into place with address search, a gizmo and precise offsets.
sidebar:
  order: 7
---

`gotiler view` opens a tileset in a local web viewer that shows it on a CesiumJS globe and lets you move it into place. Pass the `tileset.json`, or the folder that holds it:

```bash
gotiler view ./out
```

gotiler opens the viewer in the default browser and prints its address, which carries a random access token, in case the browser cannot be opened. Add `--preview` to a point cloud tiling command to open the viewer on the result as soon as tiling ends. [`gotiler splat`](/gotiler/guides/gaussian-splats/) opens it by default:

```bash
gotiler pcloud -o ./out --crs local --input-up-axis y --preview ./model.ply
```

The viewer only listens on `127.0.0.1` and stops when its page is closed, or with Ctrl+C; reloading the page keeps it running. Points are drawn 3 pixels wide; the **−** and **+** buttons in the top-left corner change their size from 1 to 14 pixels.

Gaussian splat tilesets show as splats, without the point size buttons. To keep the memory of large scenes in check, the viewer loads their tiles with a smaller cache than for point clouds, and lowers the detail when they take more memory than the cache allows.

## Moving a tileset

`gotiler view` opens as a plain viewer: click **Edit position** to show the position panel and the gizmo, and **Done** to hide them again, discarding unsaved changes. `--preview` opens with them already shown. The position panel moves the tileset:

- search an address, or click **Pick position** and then the globe, to move the cloud's center there
- drag the arrows of the gizmo drawn on the tileset to move it along its axes, or its rings to rotate it about them
- fine-tune it with offsets along the local east, north and up axes (X, Y, Z, in meters) and rotations about them (RX, RY, RZ, in degrees)
- **Save position** writes the new root transform into `tileset.json`, replacing the previous one; the tiles themselves are not rewritten

Tilesets using `region` bounding volumes are shown but cannot be moved, since the root transform does not apply to them.

## Options

| Flag     | Default | Description                                     |
| -------- | ------: | ----------------------------------------------- |
| `--port` |     `0` | Local port of the viewer; `0` picks a free one. |

To show Cesium World Imagery and World Terrain instead of the default VersaTiles satellite imagery, so picked positions follow the terrain, set the `CESIUM_ION_TOKEN` environment variable to a [Cesium ion](https://ion.cesium.com/) access token. The viewer page needs the token to load the imagery, so use one limited to the `assets:read` scope.

The viewer page loads CesiumJS and the map imagery from the internet, and address search uses [Nominatim](https://nominatim.org/).
