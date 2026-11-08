// The placement gizmo, ported from GoTiler Studio: an arrow per axis to move
// the tileset along it and a ring per axis to rotate the tileset about it,
// drawn at the tileset's placement frame.

import {
  Cartesian2,
  Cartesian3,
  Color,
  Material,
  Math as CesiumMath,
  Matrix4,
  PointPrimitiveCollection,
  PolylineCollection,
  SceneTransforms,
  ScreenSpaceEventHandler,
  ScreenSpaceEventType,
} from 'cesium';

const handles = [
  { axis: 'x', unit: Cartesian3.UNIT_X, color: Color.RED, ringRadius: 0.72, ringPoint: (a, b) => new Cartesian3(0, a, b) },
  { axis: 'y', unit: Cartesian3.UNIT_Y, color: Color.GREEN, ringRadius: 0.82, ringPoint: (a, b) => new Cartesian3(a, 0, b) },
  { axis: 'z', unit: Cartesian3.UNIT_Z, color: Color.BLUE, ringRadius: 0.92, ringPoint: (a, b) => new Cartesian3(a, b, 0) },
];
const ringSegments = 64;

export class Gizmo {
  /**
   * @param widget the CesiumWidget showing the tileset
   * @param callbacks onDragStart(); onMove(axis, meters) and onRotate(axis,
   *   degrees) with the total since the drag started; onHoverChange()
   */
  constructor(widget, callbacks) {
    this.widget = widget;
    this.callbacks = callbacks;
    this.lines = widget.scene.primitives.add(new PolylineCollection());
    this.points = widget.scene.primitives.add(new PointPrimitiveCollection());
    this.drawn = [];
    this.hoveredKey = null;
    this.drag = null;

    const events = new ScreenSpaceEventHandler(widget.scene.canvas);
    events.setInputAction((event) => this.startDrag(event.position), ScreenSpaceEventType.LEFT_DOWN);
    events.setInputAction((event) => {
      if (this.drag) this.continueDrag(event.endPosition);
      else this.setHovered(this.handleAt(event.endPosition));
    }, ScreenSpaceEventType.MOUSE_MOVE);
    events.setInputAction(() => this.endDrag(), ScreenSpaceEventType.LEFT_UP);
  }

  get hovering() {
    return this.hoveredKey !== null;
  }

  /** Draws the gizmo at frame, with arrows size meters long. */
  setFrame(frame, size) {
    this.frame = Matrix4.clone(frame);
    this.size = size;
    this.redraw();
  }

  redraw() {
    this.lines.removeAll();
    this.points.removeAll();
    this.drawn = [];
    const origin = Matrix4.getTranslation(this.frame, new Cartesian3());
    for (const handle of handles) {
      const end = this.toWorld(Cartesian3.multiplyByScalar(handle.unit, this.size, new Cartesian3()));
      const arrowId = { gizmo: true, kind: 'move', axis: handle.axis };
      this.drawn.push({
        key: keyOf(arrowId),
        color: handle.color,
        line: this.lines.add({ positions: [origin, end], material: colorMaterial(handle.color), width: 4, id: arrowId }),
        point: this.points.add({
          position: end,
          color: handle.color,
          pixelSize: 13,
          outlineColor: Color.WHITE,
          outlineWidth: 1,
          disableDepthTestDistance: Number.POSITIVE_INFINITY,
          id: arrowId,
        }),
        width: 4,
        pixelSize: 13,
      });

      const ring = [];
      for (let i = 0; i <= ringSegments; i++) {
        const angle = (i / ringSegments) * Math.PI * 2;
        const radius = handle.ringRadius * this.size;
        ring.push(this.toWorld(handle.ringPoint(Math.cos(angle) * radius, Math.sin(angle) * radius)));
      }
      const ringId = { gizmo: true, kind: 'rotate', axis: handle.axis };
      this.drawn.push({
        key: keyOf(ringId),
        color: handle.color,
        line: this.lines.add({ positions: ring, material: colorMaterial(handle.color), width: 3, id: ringId }),
        point: null,
        width: 3,
      });
    }
    this.hoveredKey = null;
    this.widget.scene.requestRender();
  }

  toWorld(local) {
    return Matrix4.multiplyByPoint(this.frame, local, new Cartesian3());
  }

  handleAt(position) {
    if (!position) return null;
    const picked = this.widget.scene.pick(position);
    const id = picked?.id ?? picked?.primitive?.id;
    return id?.gizmo ? id : null;
  }

  setHovered(id) {
    const key = keyOf(id);
    if (key === this.hoveredKey) return;
    this.hoveredKey = key;
    for (const handle of this.drawn) {
      const hovered = handle.key === key;
      const color = hovered ? highlight(handle.color) : handle.color;
      handle.line.material = colorMaterial(color);
      handle.line.width = hovered ? handle.width + 2 : handle.width;
      if (handle.point) {
        handle.point.color = color;
        handle.point.pixelSize = hovered ? handle.pixelSize + 3 : handle.pixelSize;
      }
    }
    this.widget.scene.requestRender();
    this.callbacks.onHoverChange();
  }

  startDrag(position) {
    const id = this.handleAt(position);
    if (!id) {
      this.restoreCameraInertia();
      return;
    }
    this.setHovered(null);
    this.suppressCameraInertia();
    this.drag = {
      kind: id.kind,
      axis: id.axis,
      start: Cartesian2.clone(position),
      screenAxis: id.kind === 'move' ? this.screenAxis(id.axis) : null,
      rotation: id.kind === 'rotate' ? this.rotationStart(id.axis, position) : null,
    };
    this.setCameraEnabled(false);
    this.callbacks.onDragStart();
  }

  continueDrag(position) {
    if (!position) return;
    const dx = position.x - this.drag.start.x;
    const dy = position.y - this.drag.start.y;
    if (this.drag.kind === 'move') {
      const axis = this.drag.screenAxis;
      const meters = axis ? (dx * axis.x + dy * axis.y) * axis.metersPerPixel : -dy * Math.max(this.size / 160, 0.01);
      this.callbacks.onMove(this.drag.axis, meters);
      return;
    }
    const rotation = this.drag.rotation;
    const degrees = rotation
      ? signedDegreesBetween(rotation.startAngle, angleAround(rotation.center, position)) * rotation.sign
      : (dx - dy) * 0.35;
    this.callbacks.onRotate(this.drag.axis, Math.round(degrees));
  }

  endDrag() {
    if (!this.drag) return;
    this.drag = null;
    this.setCameraEnabled(true);
  }

  /**
   * Cesium replays the last movement of a drag shorter than 0.4 s as camera
   * inertia once the camera is enabled again, which would spin the globe
   * after a quick gizmo drag. Inertia stays off until the next press outside
   * the gizmo, which discards that movement.
   */
  suppressCameraInertia() {
    const controller = this.widget.scene.screenSpaceCameraController;
    this.savedInertia ??= { spin: controller.inertiaSpin, translate: controller.inertiaTranslate, zoom: controller.inertiaZoom };
    controller.inertiaSpin = 1;
    controller.inertiaTranslate = 1;
    controller.inertiaZoom = 1;
  }

  restoreCameraInertia() {
    if (!this.savedInertia) return;
    const controller = this.widget.scene.screenSpaceCameraController;
    controller.inertiaSpin = this.savedInertia.spin;
    controller.inertiaTranslate = this.savedInertia.translate;
    controller.inertiaZoom = this.savedInertia.zoom;
    this.savedInertia = null;
  }

  setCameraEnabled(enabled) {
    const controller = this.widget.scene.screenSpaceCameraController;
    controller.enableRotate = enabled;
    controller.enableTranslate = enabled;
    controller.enableZoom = enabled;
    controller.enableTilt = enabled;
  }

  /** The on-screen direction of an axis arrow and the meters each pixel along it represents. */
  screenAxis(axis) {
    const unit = handles.find((handle) => handle.axis === axis).unit;
    const origin = this.windowPosition(Matrix4.getTranslation(this.frame, new Cartesian3()));
    const end = this.windowPosition(this.toWorld(Cartesian3.multiplyByScalar(unit, this.size, new Cartesian3())));
    if (!origin || !end) return null;
    const length = Math.hypot(end.x - origin.x, end.y - origin.y);
    if (length < 0.001) return null;
    return { x: (end.x - origin.x) / length, y: (end.y - origin.y) / length, metersPerPixel: this.size / length };
  }

  /**
   * Rotation drags follow the pointer's angle around the gizmo center on
   * screen; the sign flips when the camera looks at the ring from behind.
   */
  rotationStart(axis, position) {
    const originWorld = Matrix4.getTranslation(this.frame, new Cartesian3());
    const center = this.windowPosition(originWorld);
    if (!center) return null;
    const unit = handles.find((handle) => handle.axis === axis).unit;
    const normal = Cartesian3.normalize(Cartesian3.subtract(this.toWorld(unit), originWorld, new Cartesian3()), new Cartesian3());
    return {
      center,
      startAngle: angleAround(center, position),
      sign: Cartesian3.dot(normal, this.widget.camera.direction) > 0 ? 1 : -1,
    };
  }

  windowPosition(world) {
    return SceneTransforms.worldToWindowCoordinates(this.widget.scene, world, new Cartesian2());
  }
}

function keyOf(id) {
  return id ? `${id.kind}:${id.axis}` : null;
}

function colorMaterial(color) {
  return Material.fromType(Material.ColorType, { color });
}

function highlight(color) {
  return new Color(
    Math.min(color.red * 1.25 + 0.22, 1),
    Math.min(color.green * 1.25 + 0.22, 1),
    Math.min(color.blue * 1.25 + 0.22, 1),
    color.alpha,
  );
}

function angleAround(center, position) {
  return Math.atan2(position.y - center.y, position.x - center.x);
}

function signedDegreesBetween(startAngle, angle) {
  let delta = angle - startAngle;
  while (delta > Math.PI) delta -= Math.PI * 2;
  while (delta < -Math.PI) delta += Math.PI * 2;
  return CesiumMath.toDegrees(delta);
}
