package glb

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/texture"
)

// quad returns two triangles over the square from x, y of the given side,
// with texture coordinates across the whole image.
func quad(x, y, side float32) *mesh.Indexed {
	corner := func(dx, dy float32) ([3]float32, [2]float32) {
		return [3]float32{x + dx*side, y + dy*side, 1.5}, [2]float32{dx, dy}
	}
	var triangles []mesh.Triangle
	for _, cells := range [2][3][2]float32{{{0, 0}, {1, 0}, {1, 1}}, {{0, 0}, {1, 1}, {0, 1}}} {
		var t mesh.Triangle
		for c, d := range cells {
			t.P[c], t.UV[c] = corner(d[0], d[1])
		}
		triangles = append(triangles, t)
	}
	return mesh.Weld(triangles)
}

// positions returns the positions that a tile written without compression
// holds, in the frame of the tileset.
func positions(t *testing.T, data []byte) [][3]float64 {
	t.Helper()
	var doc gltf.Document
	if err := gltf.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	accessor := doc.Accessors[doc.Meshes[0].Primitives[0].Attributes[gltf.POSITION]]
	view := doc.BufferViews[*accessor.BufferView]
	raw := doc.Buffers[0].Data[view.ByteOffset : view.ByteOffset+view.ByteLength]
	m := doc.Nodes[0].Matrix
	var out [][3]float64
	for i := range accessor.Count {
		var q [3]float64
		for a := range 3 {
			q[a] = float64(binary.LittleEndian.Uint16(raw[i*positionStride+2*a:]))
		}
		// The node turns Z-up into Y-up: x stays, y becomes -z and z becomes y.
		out = append(out, [3]float64{m[12] + m[0]*q[0], -(m[14] + m[6]*q[1]), m[13] + m[9]*q[2]})
	}
	return out
}

func TestWriteStoresSharedPositionsIdenticallyAcrossTiles(t *testing.T) {
	atlas := texture.Solid([3]uint8{10, 20, 30})
	// Two tiles of different sizes that meet along x = 37.3.
	opts := Options{Quality: 80, StepExponent: StepExponent(100)}
	var left, right bytes.Buffer
	if err := Write(&left, quad(-62.7, 3.1, 100), atlas, opts); err != nil {
		t.Fatal(err)
	}
	if err := Write(&right, quad(37.3, 3.1, 41.9), atlas, opts); err != nil {
		t.Fatal(err)
	}

	shared := map[[3]float64]int{}
	for _, data := range [][]byte{left.Bytes(), right.Bytes()} {
		for _, p := range positions(t, data) {
			if p[0] > 37.29 && p[0] < 37.31 && p[1] < 4 {
				shared[p]++
			}
		}
	}
	if len(shared) != 1 {
		t.Fatalf("the corner that the tiles share is stored as %v", shared)
	}
	for p, count := range shared {
		if count != 2 || p[2] < 1.49 || p[2] > 1.51 {
			t.Errorf("the shared corner is at %v in %d tiles", p, count)
		}
	}
}

func TestWriteDeclaresItsExtensions(t *testing.T) {
	atlas := texture.Solid([3]uint8{10, 20, 30})
	for _, compress := range []bool{false, true} {
		var out bytes.Buffer
		if err := Write(&out, quad(0, 0, 10), atlas, Options{Quality: 80, Compress: compress}); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for extension, want := range map[string]bool{
			ExtensionQuantization:   true,
			ExtensionUnlit:          true,
			ExtensionMeshopt:        compress,
			`"mode":"TRIANGLES"`:    compress,
			`"mode":"ATTRIBUTES"`:   compress,
			`"image/jpeg"`:          true,
			`"KHR_materials_unlit"`: true,
		} {
			if strings.Contains(text, extension) != want {
				t.Errorf("compress %v: the tile mentions %s: %v, want %v", compress, extension, !want, want)
			}
		}
	}
	empty := &mesh.Indexed{}
	if err := Write(&bytes.Buffer{}, empty, atlas, Options{Quality: 80}); err == nil {
		t.Error("a mesh without triangles was written")
	}
}

func TestStepExponentFitsSixteenBits(t *testing.T) {
	for _, size := range []float64{0.001, 1, 100, 65534, 1e6} {
		step := 1.0
		for e := StepExponent(size); e > 0; e-- {
			step *= 2
		}
		for e := StepExponent(size); e < 0; e++ {
			step /= 2
		}
		if size/step > gridLimit {
			t.Errorf("a size of %v does not fit 16 bits at steps of %v", size, step)
		}
		if StepExponent(size) > minStepExponent && size/(step/2) <= gridLimit-1 {
			t.Errorf("a size of %v would fit at half the step of %v", size, step)
		}
	}
}
