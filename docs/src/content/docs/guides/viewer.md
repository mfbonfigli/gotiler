---
title: Viewing and positioning
description: Look at a tileset on a 3D globe with gotiler view, and move it into place with address search, a gizmo and precise offsets.
sidebar:
  order: 7
---

`gotiler view` shows a tileset on a 3D globe in your browser and lets you move it into place. Pass the `tileset.json`, or the folder that holds it:

```bash
gotiler view ./out
```

The viewer opens in your default browser; if it can't, open the address gotiler prints. To see a point cloud as soon as tiling ends, add `--preview` to `gotiler pcloud`. [`gotiler splats`](/gotiler/guides/gaussian-splats/input-and-placement/) opens the viewer on its own:

```bash
gotiler pcloud -o ./out --crs local --input-up-axis y --preview ./model.ply
```

Close the page, or press Ctrl+C in the terminal, to stop the viewer; reloading the page keeps it running. Only your own computer can reach it.

The **−** and **+** buttons in the top-left corner make points smaller or larger. On large Gaussian splat scenes, the viewer lowers the detail when the browser runs short of memory, and raises it again once there is room.

## Moving a tileset

Click **Edit position** to show the position panel and the gizmo; `--preview` and `gotiler splats` open the viewer with them already shown. Then:

- search an address, or click **Pick position** and then the globe, to move the tileset's center there
- drag the gizmo's arrows to move the tileset, or its rings to rotate it
- fine-tune the position with offsets toward east, north and up (X, Y, Z, in meters) and rotations about those directions (RX, RY, RZ, in degrees)
- click **Save position** to store the new position in `tileset.json`: saving takes no time, as the tiles stay as they are

**Done** hides the panel and the gizmo again, and discards the changes you did not save.

Tilesets from other tools whose bounding volumes are geographic regions can be viewed but not moved.

## Options

| Flag     | Default | Description                                     |
| -------- | ------: | ----------------------------------------------- |
| `--port` |     `0` | Local port of the viewer; `0` picks a free one. |

To show Cesium World Imagery and World Terrain instead of the default VersaTiles satellite imagery, so picked positions follow the terrain, set the `CESIUM_ION_TOKEN` environment variable to a [Cesium ion](https://ion.cesium.com/) access token. The viewer page needs the token to load the imagery, so use one limited to the `assets:read` scope.

The viewer needs an internet connection: it loads CesiumJS and the map imagery online, and searches addresses with [Nominatim](https://nominatim.org/).
