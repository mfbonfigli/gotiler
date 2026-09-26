// Generates the meshopt reference vectors used by the compression tests: a few
// deterministic attribute streams encoded by the meshoptimizer reference
// encoder, in both bitstream versions, so the tests can check the Go decoder
// (and through it the Go encoders) against the reference implementation.
//
// Usage: copy this script to a folder outside the repository, since the
// meshoptimizer import resolves next to the script, and run it there:
//
//	npm install meshoptimizer@1.3.0
//	node gen_vectors.mjs <output folder>
import { MeshoptEncoder } from 'meshoptimizer';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';

const outDir = process.argv[2] || '.';

// xorshift32, for inputs that are identical on every run
let seed = 0x9e3779b9;
function rand() {
	seed ^= seed << 13;
	seed >>>= 0;
	seed ^= seed >>> 17;
	seed ^= seed << 5;
	seed >>>= 0;
	return seed;
}

function build(count, stride, fill) {
	const data = new Uint8Array(count * stride);
	const view = new DataView(data.buffer);
	for (let i = 0; i < count; i++) {
		fill(view, i * stride, i);
	}
	return data;
}

// random walk per 16-bit lane, like quantized point positions
function walk16(count, stride, lanes, step) {
	const state = new Array(lanes).fill(0).map(() => rand() & 0xffff);
	return build(count, stride, (view, off) => {
		for (let l = 0; l < lanes; l++) {
			state[l] = (state[l] + (rand() % (2 * step + 1)) - step) & 0xffff;
			view.setUint16(off + l * 2, state[l], true);
		}
	});
}

const inputs = {
	positions: [700, 8, walk16(700, 8, 3, 300)],
	colors: [300, 4, build(300, 4, (view, off, i) => {
		for (let c = 0; c < 3; c++) {
			view.setUint8(off + c, (128 + 100 * Math.sin(i / (20 + 7 * c)) + (rand() % 9)) & 0xff);
		}
	})],
	floats: [500, 4, build(500, 4, (view, off, i) => {
		view.setFloat32(off, 1000 + i * 0.37 + (rand() % 100) / 1000, true);
	})],
	// changing bits straddle a byte boundary, where rotated XOR deltas shine
	bitfield: [400, 4, build(400, 4, (view, off) => {
		view.setUint32(off, 0x3f800000 | ((rand() & 0xf) << 6), true);
	})],
	random: [200, 12, build(200, 12, (view, off) => {
		for (let k = 0; k < 12; k++) view.setUint8(off + k, rand() & 0xff);
	})],
	sparse: [1000, 16, build(1000, 16, (view, off) => {
		for (let k = 0; k < 16; k++) view.setUint8(off + k, rand() % 20 === 0 ? rand() & 0xff : 0);
	})],
	wide: [100, 64, walk16(100, 64, 32, 5)],
	single: [1, 4, build(1, 4, (view, off) => view.setUint32(off, 0x04030201, true))],
};

// version 1 at the default level 2 (byte and 16-bit channels) and at level 3
// (which also considers rotated XOR channels), and version 0
const configs = [
	{ suffix: 'v1l2', version: 1, level: 2 },
	{ suffix: 'v1l3', version: 1, level: 3 },
	{ suffix: 'v0', version: 0, level: 2 },
];

await MeshoptEncoder.ready;
const manifest = [];
for (const [name, [count, stride, data]] of Object.entries(inputs)) {
	writeFileSync(join(outDir, `${name}.input.bin`), data);
	const entry = { name, count, stride, input: `${name}.input.bin`, encoded: [] };
	for (const { suffix, version, level } of configs) {
		const encoded = MeshoptEncoder.encodeVertexBufferLevel(data, count, stride, level, version);
		const file = `${name}.${suffix}.bin`;
		writeFileSync(join(outDir, file), encoded);
		entry.encoded.push({ file, version, level });
	}
	manifest.push(entry);
}
writeFileSync(join(outDir, 'manifest.json'), JSON.stringify(manifest, null, '\t') + '\n');
console.log(`wrote ${manifest.length} vectors to ${outDir}`);
