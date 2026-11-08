// The placement panel: address search, offset and rotation fields, picking a
// position on the globe, the move and rotate gizmo, and saving the result as
// the tileset's new root transform.
//
// The tileset is moved around a pivot: the east-north-up frame at the center
// of its bounding sphere. The edited position is the frame
//   desired = ENU(target) * translation * rotation
// where the target is a searched or picked location, or the pivot itself
// until one is chosen. The preview applies desired * pivot^-1 as the
// tileset's model matrix, and saving writes that same matrix times the
// current root transform. The rotation matrix is the exact orientation; the
// RX/RY/RZ fields add up the degrees applied about each axis, and editing
// them rebuilds the matrix from those angles.

import {
  BoundingSphere,
  Cartesian3,
  Cartographic,
  HeadingPitchRange,
  Math as CesiumMath,
  Matrix3,
  Matrix4,
  ScreenSpaceEventHandler,
  ScreenSpaceEventType,
  Transforms,
} from 'cesium';
import { api } from './api.js';
import { Gizmo } from './gizmo.js';
import { showToast } from './notices.js';

// Below this distance from the Earth's center a tileset has no geographic
// position, so its pivot frame is a plain translation.
const minimumPlacedRadius = 100000;

const panel = document.getElementById('placement');
const searchForm = document.getElementById('search-form');
const searchQuery = document.getElementById('search-query');
const searchResults = document.getElementById('search-results');
const pickButton = document.getElementById('pick');
const resetButton = document.getElementById('reset');
const saveButton = document.getElementById('save');
const message = document.getElementById('placement-message');
const translationInputs = [...panel.querySelectorAll('[data-translation]')];
const rotationInputs = [...panel.querySelectorAll('[data-rotation]')];

let widget;
let getTileset;
let reloadTileset;
let pivot;
let state;
let gizmo;
let dragStart;

function initialState() {
  return {
    target: null,
    translation: { x: 0, y: 0, z: 0 },
    rotation: { x: 0, y: 0, z: 0 },
    rotationMatrix: Matrix3.clone(Matrix3.IDENTITY),
    dirty: false,
    picking: false,
    busy: false,
  };
}

export function mountPlacementPanel(options) {
  ({ widget, getTileset, reloadTileset } = options);
  pivot = pivotFrame(getTileset());
  state = initialState();
  gizmo = new Gizmo(widget, {
    onDragStart() {
      dragStart = {
        translation: { ...state.translation },
        rotation: { ...state.rotation },
        rotationMatrix: Matrix3.clone(state.rotationMatrix),
      };
    },
    onMove(axis, meters) {
      // the arrows follow the rotated axes, while the offsets are along the
      // unrotated east, north and up axes
      const direction = Matrix3.multiplyByVector(dragStart.rotationMatrix, unitVector(axis), new Cartesian3());
      state.translation = {
        x: dragStart.translation.x + direction.x * meters,
        y: dragStart.translation.y + direction.y * meters,
        z: dragStart.translation.z + direction.z * meters,
      };
      edited();
    },
    onRotate(axis, degrees) {
      state.rotation = { ...dragStart.rotation, [axis]: dragStart.rotation[axis] + degrees };
      state.rotationMatrix = Matrix3.multiply(dragStart.rotationMatrix, axisRotation(axis, degrees), new Matrix3());
      edited();
    },
    onHoverChange: updateCursor,
  });

  searchForm.addEventListener('submit', (event) => {
    event.preventDefault();
    search();
  });
  for (const input of [...translationInputs, ...rotationInputs]) {
    input.addEventListener('change', () => updateFromFields());
  }
  pickButton.addEventListener('click', () => {
    state.picking = !state.picking;
    setMessage(state.picking ? 'Click the globe where the tileset should go.' : '');
    render();
  });
  resetButton.addEventListener('click', () => {
    state = initialState();
    applyPreview();
    setMessage('');
    render();
  });
  saveButton.addEventListener('click', () => save());

  const clicks = new ScreenSpaceEventHandler(widget.scene.canvas);
  clicks.setInputAction((event) => {
    if (state.picking) pickTarget(event.position);
  }, ScreenSpaceEventType.LEFT_CLICK);

  window.addEventListener('beforeunload', (event) => {
    if (state.dirty) event.preventDefault();
  });

  panel.classList.remove('is-hidden');
  applyPreview();
  render();
}

function pivotFrame(tileset) {
  const center = tileset.boundingSphere.center;
  if (Cartesian3.magnitude(center) < minimumPlacedRadius) {
    return Matrix4.fromTranslation(center, new Matrix4());
  }
  return Transforms.eastNorthUpToFixedFrame(center, undefined, new Matrix4());
}

function axisRotation(axis, degrees) {
  const radians = CesiumMath.toRadians(degrees);
  if (axis === 'x') return Matrix3.fromRotationX(radians);
  if (axis === 'y') return Matrix3.fromRotationY(radians);
  return Matrix3.fromRotationZ(radians);
}

function unitVector(axis) {
  if (axis === 'x') return Cartesian3.UNIT_X;
  if (axis === 'y') return Cartesian3.UNIT_Y;
  return Cartesian3.UNIT_Z;
}

function rotationFromDegrees(degrees) {
  const yx = Matrix3.multiply(axisRotation('y', degrees.y), axisRotation('x', degrees.x), new Matrix3());
  return Matrix3.multiply(axisRotation('z', degrees.z), yx, new Matrix3());
}

function desiredFrame() {
  const frame = state.target
    ? Transforms.eastNorthUpToFixedFrame(Cartesian3.fromDegrees(state.target.longitude, state.target.latitude, state.target.height))
    : Matrix4.clone(pivot);
  const { x, y, z } = state.translation;
  const local = Matrix4.fromRotationTranslation(state.rotationMatrix, new Cartesian3(x, y, z));
  return Matrix4.multiply(frame, local, new Matrix4());
}

function placementDelta() {
  return Matrix4.multiply(desiredFrame(), Matrix4.inverseTransformation(pivot, new Matrix4()), new Matrix4());
}

function applyPreview() {
  getTileset().modelMatrix = placementDelta();
  gizmo.setFrame(desiredFrame(), Math.max(getTileset().boundingSphere.radius * 0.8, 5));
  widget.scene.requestRender();
}

function edited() {
  state.dirty = true;
  applyPreview();
  render();
}

function updateFromFields() {
  for (const input of translationInputs) {
    state.translation[input.dataset.translation] = finiteOrZero(input.value);
  }
  for (const input of rotationInputs) {
    state.rotation[input.dataset.rotation] = finiteOrZero(input.value);
  }
  state.rotationMatrix = rotationFromDegrees(state.rotation);
  edited();
}

function finiteOrZero(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}

function moveTo(longitude, latitude, height, description) {
  state.target = { longitude, latitude, height };
  state.translation = { x: 0, y: 0, z: 0 };
  state.picking = false;
  state.dirty = true;
  applyPreview();
  flyToTileset();
  setMessage(`Moved to ${description}.`);
  render();
}

function pickTarget(screenPosition) {
  const ray = widget.camera.getPickRay(screenPosition);
  const position = ray && widget.scene.globe.pick(ray, widget.scene);
  if (!position) {
    setMessage('There is no ground under the cursor.', { error: true });
    return;
  }
  const picked = Cartographic.fromCartesian(position);
  moveTo(CesiumMath.toDegrees(picked.longitude), CesiumMath.toDegrees(picked.latitude), picked.height, 'the picked position');
}

function flyToTileset() {
  const center = Matrix4.getTranslation(desiredFrame(), new Cartesian3());
  const radius = Math.max(getTileset().boundingSphere.radius, 10);
  const camera = widget.camera;
  camera.flyToBoundingSphere(new BoundingSphere(center, radius), {
    offset: new HeadingPitchRange(camera.heading, Math.min(camera.pitch, CesiumMath.toRadians(-10)), Math.max(radius * 3, 60)),
    duration: 0.45,
  });
}

async function search() {
  const query = searchQuery.value.trim();
  if (!query) return;
  state.busy = true;
  setMessage('Searching…');
  render();
  try {
    const places = await api(`/api/geocode?q=${encodeURIComponent(query)}`);
    showResults(places);
    setMessage(places.length ? '' : 'No places found.');
  } catch (error) {
    setMessage(error.message, { error: true });
  } finally {
    state.busy = false;
    render();
  }
}

function showResults(places) {
  searchResults.replaceChildren(
    ...places.map((place) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = place.displayName;
      button.addEventListener('click', () => {
        searchResults.replaceChildren();
        moveTo(place.longitude, place.latitude, 0, place.displayName);
      });
      const item = document.createElement('li');
      item.append(button);
      return item;
    }),
  );
}

async function save() {
  const rootTransform = Matrix4.multiply(placementDelta(), getTileset().root.transform, new Matrix4());
  state.busy = true;
  setMessage('Saving…');
  render();
  try {
    await api('/api/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ transform: Matrix4.toArray(rootTransform) }),
    });
    const tileset = await reloadTileset();
    pivot = pivotFrame(tileset);
    state = initialState();
    applyPreview();
    setMessage('');
    showToast("Position saved. You can close this window when you're done.");
  } catch (error) {
    setMessage(`Saving failed: ${error.message}`, { error: true });
  } finally {
    state.busy = false;
    render();
  }
}

function setMessage(text, { error = false } = {}) {
  message.textContent = text;
  message.classList.toggle('is-error', error);
}

function render() {
  for (const input of translationInputs) {
    if (document.activeElement !== input) input.value = state.translation[input.dataset.translation].toFixed(2);
  }
  for (const input of rotationInputs) {
    if (document.activeElement !== input) input.value = state.rotation[input.dataset.rotation].toFixed(1);
  }
  pickButton.classList.toggle('is-active', state.picking);
  pickButton.textContent = state.picking ? 'Click the globe' : 'Pick position';
  updateCursor();
  saveButton.disabled = !state.dirty || state.busy;
  resetButton.disabled = state.busy;
  searchQuery.disabled = state.busy;
}

function updateCursor() {
  widget.scene.canvas.style.cursor = state.picking ? 'crosshair' : gizmo.hovering ? 'pointer' : '';
}
