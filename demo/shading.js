// How point cloud tilesets are drawn: their colors (RGB, by class or by
// height), the classes shown, and the point size.
//
// Everything is a custom shader uniform, so a change reaches the tiles
// already shown on the next frame. A custom vertex shader always sets the
// point size, which turns off CesiumJS's attenuation, so the shading keeps
// two shaders: a fragment-only one when points grow with distance, and one
// that also sets a fixed size.

import {
  Cartesian2,
  Cartesian3,
  Color,
  CustomShader,
  LightingModel,
  TextureMagnificationFilter,
  TextureMinificationFilter,
  TextureUniform,
  UniformType,
} from 'cesium';

// color ramps for heights, as stops spread evenly from low to high
export const ramps = {
  viridis: ['#440154', '#472d7b', '#3b528b', '#2c728e', '#21918c', '#28ae80', '#5ec962', '#addc30', '#fde725'],
  turbo: ['#30123b', '#4662d7', '#36aaf9', '#1ae4b6', '#72fe5e', '#c8ef34', '#faba39', '#f66b19', '#ca2a04', '#7a0403'],
  magma: ['#000004', '#1c1044', '#4f127b', '#812581', '#b5367a', '#e55964', '#fb8761', '#fec287', '#fcfdbf'],
  gray: ['#1a1a1a', '#f0f0f0'],
};

const modes = { rgb: 0, class: 1, height: 2 };

// a 256 by 1 RGBA texture, sampled without wrapping
function texture(data, filter) {
  return new TextureUniform({
    typedArray: data,
    width: 256,
    height: 1,
    repeat: false,
    minificationFilter: filter === 'nearest' ? TextureMinificationFilter.NEAREST : TextureMinificationFilter.LINEAR,
    magnificationFilter: filter === 'nearest' ? TextureMagnificationFilter.NEAREST : TextureMagnificationFilter.LINEAR,
  });
}

function rampTexture(stops) {
  const colors = stops.map((stop) => Color.fromCssColorString(stop));
  const data = new Uint8Array(256 * 4);
  for (let i = 0; i < 256; i++) {
    const position = (i / 255) * (colors.length - 1);
    const low = Math.min(Math.floor(position), colors.length - 2);
    const color = Color.lerp(colors[low], colors[low + 1], position - low, new Color());
    data.set([color.red * 255, color.green * 255, color.blue * 255, 255], i * 4);
  }
  return texture(data, 'linear');
}

// the class colors by code, with the alpha telling whether the class is shown
function classTexture(classes, hidden) {
  const data = new Uint8Array(256 * 4);
  for (let code = 0; code < 256; code++) {
    data.set([160, 160, 160, 255], code * 4);
  }
  for (const { code, color } of classes) {
    const c = Color.fromCssColorString(color);
    data.set([c.red * 255, c.green * 255, c.blue * 255, hidden.has(code) ? 0 : 255], code * 4);
  }
  return texture(data, 'nearest');
}

function fragmentShader(hasClasses) {
  const classes = hasClasses
    ? `
    vec4 entry = texture(u_classes, vec2((fsInput.metadata.CLASSIFICATION + 0.5) / 256.0, 0.5));
    if (entry.a < 0.5) {
      discard;
    }
    if (u_mode > 0.5 && u_mode < 1.5) {
      material.diffuse = czm_srgbToLinear(entry.rgb);
      return;
    }`
    : '';
  return `
  void fragmentMain(FragmentInput fsInput, inout czm_modelMaterial material) {${classes}
    if (u_mode > 1.5) {
      float height = dot(fsInput.attributes.positionWC - u_origin, u_up);
      float t = clamp((height - u_range.x) / max(u_range.y - u_range.x, 0.01), 0.0, 1.0);
      material.diffuse = czm_srgbToLinear(texture(u_ramp, vec2((t * 255.0 + 0.5) / 256.0, 0.5)).rgb);
    }
  }`;
}

const fixedSizeVertexShader = `
  void vertexMain(VertexInput vsInput, inout czm_modelVertexOutput vsOutput) {
    vsOutput.pointSize = u_pointSize;
  }`;

export class PointShading {
  /**
   * @param {object} dataset a point cloud dataset of datasets.js
   * @param {object} settings the starting mode, ramp, point size and
   *   attenuation
   */
  constructor(dataset, settings) {
    this.classes = dataset.classes ?? [];
    this.hidden = new Set(this.classes.filter((c) => c.hidden).map((c) => c.code));
    this.settings = { ...settings };
    const uniforms = () => ({
      u_mode: { type: UniformType.FLOAT, value: modes[settings.mode] },
      u_ramp: { type: UniformType.SAMPLER_2D, value: rampTexture(ramps[settings.ramp]) },
      u_classes: { type: UniformType.SAMPLER_2D, value: classTexture(this.classes, this.hidden) },
      u_range: { type: UniformType.VEC2, value: new Cartesian2(0, 1) },
      u_origin: { type: UniformType.VEC3, value: new Cartesian3() },
      // a new vector: setUniform copies into it, and UNIT_Z is frozen
      u_up: { type: UniformType.VEC3, value: new Cartesian3(0, 0, 1) },
      u_pointSize: { type: UniformType.FLOAT, value: settings.pointSize },
    });
    const fragmentShaderText = fragmentShader(this.classes.length > 0);
    this.attenuated = new CustomShader({ lightingModel: LightingModel.UNLIT, uniforms: uniforms(), fragmentShaderText });
    this.fixed = new CustomShader({
      lightingModel: LightingModel.UNLIT,
      uniforms: uniforms(),
      vertexShaderText: fixedSizeVertexShader,
      fragmentShaderText,
    });
  }

  /** Draws the tileset with this shading. */
  apply(tileset) {
    this.tileset = tileset;
    // heights are measured along the up axis of the tileset's local frame
    const frame = tileset.root.computedTransform;
    const origin = new Cartesian3(frame[12], frame[13], frame[14]);
    const up = Cartesian3.normalize(new Cartesian3(frame[8], frame[9], frame[10]), new Cartesian3());
    this.set('u_origin', origin);
    this.set('u_up', up);
    this.update();
  }

  /** The range of heights the tileset covers, from its root bounding box. */
  heightExtent() {
    const frame = this.tileset.root.computedTransform;
    const origin = new Cartesian3(frame[12], frame[13], frame[14]);
    const up = Cartesian3.normalize(new Cartesian3(frame[8], frame[9], frame[10]), new Cartesian3());
    const box = this.tileset.root.boundingVolume.boundingVolume;
    if (box?.halfAxes) {
      const center = Cartesian3.dot(Cartesian3.subtract(box.center, origin, new Cartesian3()), up);
      let extent = 0;
      for (let column = 0; column < 3; column++) {
        const axis = new Cartesian3(box.halfAxes[column * 3], box.halfAxes[column * 3 + 1], box.halfAxes[column * 3 + 2]);
        extent += Math.abs(Cartesian3.dot(axis, up));
      }
      return { min: center - extent, max: center + extent };
    }
    const sphere = this.tileset.boundingSphere;
    const center = Cartesian3.dot(Cartesian3.subtract(sphere.center, origin, new Cartesian3()), up);
    return { min: center - sphere.radius, max: center + sphere.radius };
  }

  set(name, value) {
    this.attenuated.setUniform(name, value);
    this.fixed.setUniform(name, value);
  }

  setMode(mode) {
    this.settings.mode = mode;
    this.set('u_mode', modes[mode]);
  }

  setRamp(name) {
    this.settings.ramp = name;
    this.set('u_ramp', rampTexture(ramps[name]));
  }

  setHeightRange(from, to) {
    this.set('u_range', new Cartesian2(from, to));
  }

  setClassShown(code, shown) {
    if (shown) {
      this.hidden.delete(code);
    } else {
      this.hidden.add(code);
    }
    this.set('u_classes', classTexture(this.classes, this.hidden));
  }

  isClassShown(code) {
    return !this.hidden.has(code);
  }

  setPointSize(size) {
    this.settings.pointSize = size;
    this.set('u_pointSize', size);
    this.update();
  }

  setAttenuation(on) {
    this.settings.attenuation = on;
    this.update();
  }

  // the shader and point cloud shading for the current settings
  update() {
    if (!this.tileset) {
      return;
    }
    const { attenuation, pointSize } = this.settings;
    const shader = attenuation ? this.attenuated : this.fixed;
    if (this.tileset.customShader !== shader) {
      this.tileset.customShader = shader;
    }
    const shading = this.tileset.pointCloudShading;
    shading.attenuation = attenuation;
    shading.maximumAttenuation = pointSize;
    // on by default in CesiumJS, whenever attenuation is
    shading.eyeDomeLighting = false;
  }
}
