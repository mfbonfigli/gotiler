// Fly navigation: WASD moves the camera where it looks, E and Q move it up
// and down, Shift goes faster and the mouse wheel sets the speed. Clicking
// the view captures the mouse to look around; Esc, handled by the browser,
// releases it. CesiumJS's own mouse controls are off while flying.

import { Cartesian3, Math as CesiumMath } from 'cesium';

const lookSpeed = 0.002; // radians per pixel of mouse movement
const pitchLimit = CesiumMath.toRadians(89);
const keyActions = {
  KeyW: 'forward',
  ArrowUp: 'forward',
  KeyS: 'backward',
  ArrowDown: 'backward',
  KeyA: 'left',
  ArrowLeft: 'left',
  KeyD: 'right',
  ArrowRight: 'right',
  KeyE: 'up',
  Space: 'up',
  KeyQ: 'down',
  KeyC: 'down',
};

function typingIn(target) {
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'SELECT', 'TEXTAREA'].includes(target.tagName));
}

/**
 * Adds fly navigation to the widget. groundHeight returns the height of the
 * shown dataset above the ellipsoid: the speed grows with the height above
 * it. onChange is called when flying starts or stops, the mouse is captured
 * or released, or the speed changes.
 */
export function createFlyNavigation(widget, { groundHeight, onChange }) {
  const { scene, camera, canvas } = widget;
  const held = new Set();
  let active = false;
  let captured = false;
  let speedFactor = 1;
  let shift = false;
  let lastFrame;
  let speed = 0;

  const notify = () => onChange?.({ active, captured, speed });

  function setActive(value) {
    if (active === value) {
      return;
    }
    active = value;
    held.clear();
    scene.screenSpaceCameraController.enableInputs = !active;
    if (!active && captured) {
      document.exitPointerLock();
    }
    notify();
  }

  canvas.addEventListener('click', () => {
    if (active && !captured) {
      canvas.requestPointerLock?.();
    }
  });
  document.addEventListener('pointerlockchange', () => {
    captured = document.pointerLockElement === canvas;
    notify();
  });
  document.addEventListener('mousemove', (event) => {
    if (!captured) {
      return;
    }
    const pitch = CesiumMath.clamp(camera.pitch - event.movementY * lookSpeed, -pitchLimit, pitchLimit);
    camera.setView({ orientation: { heading: camera.heading + event.movementX * lookSpeed, pitch, roll: 0 } });
  });
  canvas.addEventListener(
    'wheel',
    (event) => {
      if (!active) {
        return;
      }
      event.preventDefault();
      speedFactor = CesiumMath.clamp(speedFactor * (event.deltaY < 0 ? 1.25 : 0.8), 0.05, 50);
    },
    { passive: false },
  );

  addEventListener('keydown', (event) => {
    if (typingIn(event.target) || event.ctrlKey || event.metaKey || event.altKey) {
      return;
    }
    if (event.code === 'KeyF' && !event.repeat) {
      setActive(!active);
      return;
    }
    shift = event.shiftKey;
    const action = keyActions[event.code];
    if (active && action) {
      held.add(action);
      event.preventDefault();
    }
  });
  addEventListener('keyup', (event) => {
    shift = event.shiftKey;
    const action = keyActions[event.code];
    if (action) {
      held.delete(action);
    }
  });
  addEventListener('blur', () => held.clear());

  let lastNotified = 0;
  scene.preRender.addEventListener(() => {
    const now = performance.now();
    const seconds = lastFrame === undefined ? 0 : Math.min((now - lastFrame) / 1000, 0.1);
    lastFrame = now;
    if (!active) {
      return;
    }
    const above = Math.abs(camera.positionCartographic.height - groundHeight());
    speed = CesiumMath.clamp(above * 0.5, 2, 3000) * speedFactor * (shift ? 4 : 1);
    if (now - lastNotified > 250) {
      lastNotified = now;
      notify();
    }
    if (held.size === 0 || seconds === 0) {
      return;
    }
    const step = speed * seconds;
    if (held.has('forward')) camera.moveForward(step);
    if (held.has('backward')) camera.moveBackward(step);
    if (held.has('left')) camera.moveLeft(step);
    if (held.has('right')) camera.moveRight(step);
    if (held.has('up') || held.has('down')) {
      const up = scene.globe.ellipsoid.geodeticSurfaceNormal(camera.positionWC, new Cartesian3());
      camera.move(up, held.has('up') ? step : -step);
    }
  });

  return {
    get active() {
      return active;
    },
    setActive,
  };
}
