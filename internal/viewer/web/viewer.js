import {
  Cesium3DTileset,
  CesiumWidget,
  Color,
  EllipsoidTerrainProvider,
  HeadingPitchRange,
  ImageryLayer,
  Ion,
  Math as CesiumMath,
  Matrix4,
  UrlTemplateImageryProvider,
  createWorldImageryAsync,
  createWorldTerrainAsync,
} from 'cesium';
import { api } from './api.js';
import { setStatus } from './notices.js';
import { mountPlacementPanel, showUnmovable } from './placement.js';

async function createWidget(config) {
  const widget = new CesiumWidget(document.getElementById('cesium-host'), {
    baseLayer: false,
    scene3DOnly: true,
    msaaSamples: 1,
  });
  widget.scene.globe.baseColor = Color.fromCssColorString('#1f5f84');

  if (config.ionToken) {
    Ion.defaultAccessToken = config.ionToken;
    widget.scene.imageryLayers.add(new ImageryLayer(await createWorldImageryAsync()));
    widget.scene.terrainProvider = await createWorldTerrainAsync();
  } else {
    const imagery = new UrlTemplateImageryProvider({
      url: 'https://tiles.versatiles.org/tiles/satellite/{z}/{x}/{y}',
      maximumLevel: 14,
      credit: 'Imagery courtesy of VersaTiles & OpenStreetMap contributors',
    });
    widget.scene.imageryLayers.add(new ImageryLayer(imagery));
    widget.scene.terrainProvider = new EllipsoidTerrainProvider();
  }
  return widget;
}

function frameCamera(widget, tileset) {
  const sphere = tileset.boundingSphere;
  widget.camera.viewBoundingSphere(
    sphere,
    new HeadingPitchRange(0, CesiumMath.toRadians(-35), Math.max(sphere.radius * 2.5, 200)),
  );
  widget.camera.lookAtTransform(Matrix4.IDENTITY);
}

async function start() {
  let config;
  try {
    config = await api('/api/config');
  } catch (error) {
    setStatus(`Cannot reach gotiler (${error.message}). Open the full URL printed by gotiler, which includes an access token.`, { error: true });
    return;
  }
  try {
    const widget = await createWidget(config);
    setStatus('Loading the tileset…');
    let tileset = widget.scene.primitives.add(await Cesium3DTileset.fromUrl(config.tilesetUrl));
    frameCamera(widget, tileset);
    setStatus('');
    if (!config.editable) {
      showUnmovable(config.readOnlyReason);
      return;
    }
    mountPlacementPanel({
      widget,
      startEditing: config.startEditing,
      getTileset: () => tileset,
      async reloadTileset() {
        const reloaded = await Cesium3DTileset.fromUrl(config.tilesetUrl);
        widget.scene.primitives.remove(tileset);
        tileset = widget.scene.primitives.add(reloaded);
        return tileset;
      },
    });
  } catch (error) {
    setStatus(`The tileset could not be shown: ${error.message ?? error}`, { error: true });
  }
}

start();
