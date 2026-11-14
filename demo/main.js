// The live demo: a CesiumJS globe with the datasets of datasets.js, a toolbar
// to switch between orbiting and flying, and a panel of display settings.

import {
  Cartesian3,
  Cartographic,
  Cesium3DTileset,
  CesiumWidget,
  Color,
  Credit,
  EllipsoidTerrainProvider,
  HeadingPitchRange,
  Ion,
  Math as CesiumMath,
  Matrix4,
  createWorldTerrainAsync,
} from 'cesium';
import { datasets } from './datasets.js';
import { createFlyNavigation } from './fly.js';
import { PointShading, ramps } from './shading.js';
import { installMemoryGuard, splatTilesetOptions } from './splats.js';

// A Cesium ion token for the imagery. 
Ion.defaultAccessToken =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJqdGkiOiI3Yzg0ZGY1Ni0xYTJiLTRiNzgtYmZlYy03ZjgyOTViMjhjMmQiLCJpZCI6MjEzODY1LCJzdWIiOiJmZWRlcmljby5ib25maWdsaSIsImlzcyI6Imh0dHBzOi8vYXBpLmNlc2l1bS5jb20iLCJhdWQiOiJmZWRlcmljby5ib25maWdsaV9kZWZhdWx0IiwiaWF0IjoxNzgzNDM1MzYyfQ.sIOI6tvp8yWm824zSZDd_1QUGCDQAazR71BSFd34aEM';

const megabyte = 1024 * 1024;
const $ = (id) => document.getElementById(id);

// the settings shared by every point cloud
const pointSettings = { mode: 'rgb', ramp: 'viridis', pointSize: 3, attenuation: true };
// the tile cache of each kind of dataset, in megabytes: splats take far more
// memory in the browser than their tiles' size
const memoryBudgets = { points: 512, splats: 256 };

const widget = new CesiumWidget($('globe'), { scene3DOnly: true });
const { scene, camera } = widget;
scene.globe.baseColor = Color.fromCssColorString('#1f2a36');

let current; // { dataset, tileset, shading, credit, setDetail, ground }
let loadToken = 0;
const flatTerrain = new EllipsoidTerrainProvider();
let worldTerrain; // a promise of Cesium World Terrain, loaded on first use

// for debugging from the browser console
window.demo = {
  widget,
  get current() {
    return current;
  },
};

function setStatus(message, { error = false } = {}) {
  const status = $('status');
  status.textContent = message;
  status.hidden = !message;
  status.classList.toggle('is-error', error);
}

function setPressed(buttons, picked) {
  for (const button of buttons) {
    button.setAttribute('aria-pressed', String(button === picked));
  }
}

function setSwitch(element, on) {
  element.setAttribute('aria-checked', String(on));
}

function onSwitch(element, handler) {
  element.addEventListener('click', () => {
    const on = element.getAttribute('aria-checked') !== 'true';
    setSwitch(element, on);
    handler(on);
  });
}

// --- datasets

async function isAvailable(dataset) {
  try {
    const response = await fetch(dataset.url, { method: 'HEAD' });
    return response.ok;
  } catch {
    return false;
  }
}

// World terrain for the datasets that ask for it, and the flat globe for the
// others, whose heights follow no terrain model. Without world terrain, the
// dataset still shows, on the flat globe.
async function terrainFor(dataset) {
  if (!dataset.terrain) {
    return flatTerrain;
  }
  worldTerrain ??= createWorldTerrainAsync();
  try {
    return await worldTerrain;
  } catch (error) {
    worldTerrain = undefined;
    console.warn(`Cesium World Terrain could not be loaded: ${error.message ?? error}`);
    return flatTerrain;
  }
}

function frame(tileset, animate) {
  const sphere = tileset.boundingSphere;
  const offset = new HeadingPitchRange(0, CesiumMath.toRadians(-35), Math.max(sphere.radius * 2.2, 60));
  if (animate) {
    camera.flyToBoundingSphere(sphere, { offset, duration: 1.5 });
  } else {
    camera.viewBoundingSphere(sphere, offset);
    camera.lookAtTransform(Matrix4.IDENTITY);
  }
}

async function loadDataset(dataset, { animate }) {
  const token = ++loadToken;
  setStatus('');
  setPressed(document.querySelectorAll('.dataset'), document.querySelector(`.dataset[data-id="${dataset.id}"]`));
  history.replaceState(null, '', `#${dataset.id}`);
  if (current) {
    scene.primitives.remove(current.tileset);
    widget.creditDisplay.removeStaticCredit(current.credit);
    current = undefined;
  }

  const splats = dataset.kind === 'splats';
  const memory = memoryBudgets[dataset.kind];
  const terrain = terrainFor(dataset);
  let tileset;
  try {
    tileset = await Cesium3DTileset.fromUrl(dataset.url, {
      ...(splats ? splatTilesetOptions : {}),
      maximumScreenSpaceError: dataset.maximumScreenSpaceError,
      cacheBytes: memory * megabyte,
      maximumCacheOverflowBytes: (memory / 2) * megabyte,
      debugShowBoundingVolume: $('bounds').getAttribute('aria-checked') === 'true',
    });
  } catch (error) {
    if (token === loadToken) {
      setStatus(`${dataset.name} could not be loaded: ${error.message ?? error}`, { error: true });
    }
    return;
  }
  const terrainProvider = await terrain;
  if (token !== loadToken) {
    tileset.destroy();
    return;
  }
  if (scene.terrainProvider !== terrainProvider) {
    scene.terrainProvider = terrainProvider;
  }
  scene.primitives.add(tileset);

  const credit = new Credit(dataset.credit, true);
  widget.creditDisplay.addStaticCredit(credit);
  const origin = Matrix4.getTranslation(tileset.root.computedTransform, new Cartesian3());
  const ground = Cartographic.fromCartesian(origin)?.height ?? 0;
  current = { dataset, tileset, credit, ground };

  if (splats) {
    current.setDetail = installMemoryGuard(tileset, dataset.maximumScreenSpaceError);
  } else {
    current.shading = new PointShading(dataset, pointSettings);
    current.shading.apply(tileset);
    current.setDetail = (error) => {
      tileset.maximumScreenSpaceError = error;
    };
  }
  renderPanel();
  frame(tileset, animate);
}

function renderDatasets(available) {
  const list = $('dataset-list');
  list.replaceChildren(
    ...available.map((dataset) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'dataset';
      button.dataset.id = dataset.id;
      button.setAttribute('aria-pressed', 'false');
      button.innerHTML = `<strong></strong><small></small>`;
      button.querySelector('strong').textContent = dataset.name;
      button.querySelector('small').textContent = dataset.summary;
      button.addEventListener('click', () => {
        if (current?.dataset !== dataset) {
          loadDataset(dataset, { animate: true });
        }
      });
      return button;
    }),
  );
}

// --- the settings panel

const colorButtons = [...document.querySelectorAll('[data-color]')];

function renderPanel() {
  const { dataset, shading, tileset } = current;
  const points = dataset.kind === 'points';
  $('color-section').hidden = !points;
  $('points-section').hidden = !points;
  $('classes-section').hidden = !points || dataset.classes.length === 0;

  $('detail').value = String(33 - tileset.maximumScreenSpaceError);
  $('detail-value').textContent = `SSE ${tileset.maximumScreenSpaceError}`;
  if (!points) {
    return;
  }

  const classified = dataset.classes.length > 0;
  const classButton = colorButtons.find((button) => button.dataset.color === 'class');
  classButton.disabled = !classified;
  if (!classified && pointSettings.mode === 'class') {
    pointSettings.mode = 'rgb';
  }
  shading.setMode(pointSettings.mode);
  setPressed(colorButtons, colorButtons.find((button) => button.dataset.color === pointSettings.mode));
  $('height-settings').hidden = pointSettings.mode !== 'height';

  // heights run from the lowest point of the root bounding box
  const extent = shading.heightExtent();
  const span = Math.max(1, Math.ceil(extent.max - extent.min));
  current.heightBase = extent.min;
  for (const input of [$('height-from'), $('height-to')]) {
    input.min = '0';
    input.max = String(span);
  }
  $('height-from').value = '0';
  $('height-to').value = String(span);
  updateHeightRange();

  $('classes').replaceChildren(
    ...dataset.classes.map(({ code, name, color }) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'class';
      button.title = `Class ${code}`;
      button.setAttribute('aria-pressed', String(shading.isClassShown(code)));
      button.innerHTML = '<i></i><span></span>';
      button.querySelector('i').style.background = color;
      button.querySelector('span').textContent = name;
      button.addEventListener('click', () => {
        const shown = !shading.isClassShown(code);
        shading.setClassShown(code, shown);
        button.setAttribute('aria-pressed', String(shown));
      });
      return button;
    }),
  );

  $('point-size').value = String(pointSettings.pointSize);
  $('point-size-value').textContent = `${pointSettings.pointSize} px`;
  setSwitch($('attenuation'), pointSettings.attenuation);
}

function updateHeightRange() {
  let from = Number($('height-from').value);
  let to = Number($('height-to').value);
  if (from > to) {
    [from, to] = [to, from];
  }
  $('height-from-value').textContent = `${Math.round(from)} m`;
  $('height-to-value').textContent = `${Math.round(to)} m`;
  current?.shading?.setHeightRange(current.heightBase + from, current.heightBase + to);
}

function mountPanel() {
  for (const button of colorButtons) {
    button.addEventListener('click', () => {
      pointSettings.mode = button.dataset.color;
      setPressed(colorButtons, button);
      $('height-settings').hidden = pointSettings.mode !== 'height';
      current?.shading?.setMode(pointSettings.mode);
    });
  }

  const rampButtons = Object.entries(ramps).map(([name, stops]) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'ramp';
    button.title = name[0].toUpperCase() + name.slice(1);
    button.setAttribute('aria-label', `${button.title} color ramp`);
    button.setAttribute('aria-pressed', String(name === pointSettings.ramp));
    button.style.background = `linear-gradient(90deg, ${stops.join(', ')})`;
    button.addEventListener('click', () => {
      pointSettings.ramp = name;
      setPressed(rampButtons, button);
      current?.shading?.setRamp(name);
    });
    return button;
  });
  $('ramps').replaceChildren(...rampButtons);
  $('height-from').addEventListener('input', updateHeightRange);
  $('height-to').addEventListener('input', updateHeightRange);

  $('point-size').addEventListener('input', () => {
    pointSettings.pointSize = Number($('point-size').value);
    $('point-size-value').textContent = `${pointSettings.pointSize} px`;
    current?.shading?.setPointSize(pointSettings.pointSize);
  });
  onSwitch($('attenuation'), (on) => {
    pointSettings.attenuation = on;
    current?.shading?.setAttenuation(on);
  });

  $('detail').addEventListener('input', () => {
    const error = 33 - Number($('detail').value);
    $('detail-value').textContent = `SSE ${error}`;
    current?.setDetail(error);
  });
  onSwitch($('bounds'), (on) => {
    if (current) {
      current.tileset.debugShowBoundingVolume = on;
    }
  });
}

// --- the toolbar and fly navigation

function mountToolbar() {
  const navigationButtons = [...document.querySelectorAll('[data-navigation]')];
  const fly = createFlyNavigation(widget, {
    groundHeight: () => current?.ground ?? 0,
    onChange({ active, captured, speed }) {
      setPressed(navigationButtons, navigationButtons.find((b) => b.dataset.navigation === (active ? 'fly' : 'orbit')));
      $('globe').classList.toggle('flying', active);
      $('hint').hidden = !active;
      $('speed').textContent = `· ${speed < 10 ? speed.toFixed(1) : Math.round(speed)} m/s`;
      $('hint-lead').innerHTML = captured
        ? 'Moving the mouse looks around · <kbd>F</kbd> back to orbit'
        : 'Click the view to look around with the mouse · <kbd>F</kbd> back to orbit';
    },
  });
  for (const button of navigationButtons) {
    button.addEventListener('click', () => {
      camera.lookAtTransform(Matrix4.IDENTITY);
      fly.setActive(button.dataset.navigation === 'fly');
    });
  }
  // flying needs a keyboard and a mouse
  if (!matchMedia('(any-pointer: fine)').matches) {
    $('navigation').hidden = true;
  }

  $('home').addEventListener('click', () => {
    if (current) {
      frame(current.tileset, true);
    }
  });

  const toggle = $('settings-toggle');
  const setPanel = (open) => {
    $('panel').hidden = !open;
    toggle.setAttribute('aria-pressed', String(open));
  };
  setPanel(innerWidth >= 1100);
  toggle.addEventListener('click', () => setPanel($('panel').hidden));

  if (!document.fullscreenEnabled) {
    $('fullscreen').hidden = true;
  }
  $('fullscreen').addEventListener('click', () => {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else {
      document.documentElement.requestFullscreen();
    }
  });
}

async function start() {
  mountPanel();
  mountToolbar();
  setStatus('Loading the datasets…');
  const available = (await Promise.all(datasets.map(async (d) => ((await isAvailable(d)) ? d : null)))).filter(Boolean);
  if (available.length === 0) {
    setStatus('No dataset could be found.', { error: true });
    return;
  }
  renderDatasets(available);
  // the address names the dataset shown, as in /#melbourne
  const requested = () => available.find((d) => `#${d.id}` === location.hash);
  addEventListener('hashchange', () => {
    const dataset = requested();
    if (dataset && dataset !== current?.dataset) {
      loadDataset(dataset, { animate: true });
    }
  });
  await loadDataset(requested() ?? available[0], { animate: false });
}

start();
