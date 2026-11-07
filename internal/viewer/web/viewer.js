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

const token = new URLSearchParams(window.location.search).get('token') ?? '';
const statusBox = document.getElementById('status');

function setStatus(message, { error = false } = {}) {
  statusBox.textContent = message;
  statusBox.classList.toggle('is-error', error);
  statusBox.classList.toggle('is-hidden', !message);
}

/** Calls a viewer API, adding the access token from the page URL. */
export async function api(path, options = {}) {
  const url = new URL(path, window.location.origin);
  url.searchParams.set('token', token);
  const response = await fetch(url, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `${response.status} ${response.statusText}`);
  }
  return response.status === 204 ? null : response.json();
}

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

async function loadTileset(widget, url) {
  const tileset = await Cesium3DTileset.fromUrl(url);
  widget.scene.primitives.add(tileset);
  const sphere = tileset.boundingSphere;
  widget.camera.viewBoundingSphere(
    sphere,
    new HeadingPitchRange(0, CesiumMath.toRadians(-35), Math.max(sphere.radius * 2.5, 200)),
  );
  widget.camera.lookAtTransform(Matrix4.IDENTITY);
  return tileset;
}

/**
 * Keeps an events stream open while the page is open: the server stops
 * shortly after the last page closes.
 */
function connectToServer() {
  const events = new EventSource(`/api/events?token=${encodeURIComponent(token)}`);
  events.onerror = () => {
    if (events.readyState !== EventSource.OPEN) {
      setStatus('Lost the connection to gotiler: the viewer has stopped.', { error: true });
    }
  };
}

async function start() {
  let config;
  try {
    config = await api('/api/config');
  } catch (error) {
    setStatus(`Cannot reach gotiler (${error.message}). Open the full URL printed by gotiler, which includes an access token.`, { error: true });
    return;
  }
  connectToServer();
  try {
    const widget = await createWidget(config);
    setStatus('Loading the tileset…');
    await loadTileset(widget, config.tilesetUrl);
    setStatus(config.editable ? '' : `View only: ${config.readOnlyReason}.`);
  } catch (error) {
    setStatus(`The tileset could not be shown: ${error.message ?? error}`, { error: true });
  }
}

start();
