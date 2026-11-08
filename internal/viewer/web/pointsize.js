// The point size control: buttons that make the tileset's points smaller or
// bigger.
//
// The size is a custom shader uniform rather than a style: Cesium rebuilds the
// shaders of glTF tiles only when they load, so changing a style's point size
// would not reach the tiles already shown, while a uniform takes effect on the
// next frame.

import { CustomShader, UniformType } from 'cesium';

const minPointSize = 1;
const maxPointSize = 14;
let pointSize = 3;
let shader;

const control = document.getElementById('point-size');
const smallerButton = document.getElementById('point-size-smaller');
const biggerButton = document.getElementById('point-size-bigger');
const valueLabel = document.getElementById('point-size-value');

/** Draws the tileset's points at the current size. */
export function applyPointSize(tileset) {
  shader = new CustomShader({
    uniforms: { u_pointSize: { type: UniformType.FLOAT, value: pointSize } },
    vertexShaderText: `
      void vertexMain(VertexInput vsInput, inout czm_modelVertexOutput vsOutput) {
        vsOutput.pointSize = u_pointSize;
      }
    `,
  });
  tileset.customShader = shader;
}

export function mountPointSizeControl(widget) {
  const change = (delta) => {
    pointSize = Math.min(maxPointSize, Math.max(minPointSize, pointSize + delta));
    shader.setUniform('u_pointSize', pointSize);
    widget.scene.requestRender();
    render();
  };
  smallerButton.addEventListener('click', () => change(-1));
  biggerButton.addEventListener('click', () => change(1));
  render();
  control.classList.remove('is-hidden');
}

function render() {
  valueLabel.textContent = `${pointSize} px`;
  smallerButton.disabled = pointSize <= minPointSize;
  biggerButton.disabled = pointSize >= maxPointSize;
}
