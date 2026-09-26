package compression

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/version"
)

func TestRegistersCompressedKHREncoder(t *testing.T) {
	factory, ok := plugin.GeometryEncoderFactoryFor(EncoderCompressedGLBKHR)
	if !ok {
		t.Fatal("expected compressed GLB KHR encoder to be registered")
	}
	encoder := factory(model.DefaultAttributes())
	if encoder.TilesetVersion() != version.TilesetVersion_1_1 {
		t.Fatalf("unexpected tileset version %s", encoder.TilesetVersion())
	}
	compressed, ok := encoder.(*CompressedGltfEncoder)
	if !ok {
		t.Fatalf("unexpected encoder type %T", encoder)
	}
	if compressed.meshoptExtension != khrMeshoptCompression {
		t.Fatalf("expected the KHR encoder to use %s, got %s", khrMeshoptCompression, compressed.meshoptExtension)
	}
}

func TestCompressedEncoderKHRModeUsesKHRExtension(t *testing.T) {
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification)
	node := newMockNodeWithSummaries(standardSummaries(attrs), attachStandardAttributes(attrs, testPoints)...)
	doc := parseGLBJSON(t, encodeWith(t, New(attrs, WithKHRMeshopt()), node))

	for _, key := range []string{"extensionsUsed", "extensionsRequired"} {
		assertStringListContains(t, doc[key], "KHR_meshopt_compression")
		assertStringListDoesNotContain(t, doc[key], "EXT_meshopt_compression")
	}
	buffers := mustList(t, doc, "buffers")
	fallbackExt := mustMap(t, mustMapFromValue(t, buffers[1], "buffers[1]"), "extensions")
	if _, ok := fallbackExt["EXT_meshopt_compression"]; ok {
		t.Fatal("KHR mode fallback buffer must not use EXT_meshopt_compression")
	}
	if fallback, ok := mustMap(t, fallbackExt, "KHR_meshopt_compression")["fallback"].(bool); !ok || !fallback {
		t.Fatalf("expected buffer[1] KHR_meshopt_compression.fallback=true, got %#v", fallbackExt)
	}
	for i, raw := range mustList(t, doc, "bufferViews") {
		ext := mustMap(t, mustMapFromValue(t, raw, "bufferViews"), "extensions")
		if _, ok := ext["EXT_meshopt_compression"]; ok {
			t.Fatalf("bufferViews[%d] must not use EXT_meshopt_compression in KHR mode", i)
		}
		if got := mustMap(t, ext, "KHR_meshopt_compression")["mode"]; got != "ATTRIBUTES" {
			t.Fatalf("bufferViews[%d] meshopt mode = %#v, want ATTRIBUTES", i, got)
		}
	}
}

// TestCompressedEncoderStreamsDecodeToTileData decodes every compressed buffer
// view of a tile written in each mode and checks it against the tile data.
func TestCompressedEncoderStreamsDecodeToTileData(t *testing.T) {
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification)
	summaries := standardSummaries(attrs)
	rng := rand.New(rand.NewPCG(3, 5))
	points := make([]model.Point, 700)
	for i := range points {
		points[i] = model.Point{
			X: rng.Float32() * 100, Y: rng.Float32() * 100, Z: rng.Float32() * 10,
			R: uint8(rng.Uint32()), G: uint8(rng.Uint32()), B: uint8(rng.Uint32()),
			Attributes: packSummaryValues(summaries, map[string]any{
				model.AttrIntensity:      uint16(rng.Uint32()),
				model.AttrClassification: uint8(rng.IntN(32)),
			}),
		}
	}

	// expected contents of the buffer views, keyed by primitive attribute
	n := len(points)
	coords := make([][3]float32, n)
	colors := make([][3]uint8, n)
	intensity := make([]byte, n*4)
	classification := make([]byte, n*4)
	for i, pt := range points {
		coords[i] = [3]float32{pt.X, pt.Y, pt.Z}
		colors[i] = [3]uint8{srgbToLinear[pt.R], srgbToLinear[pt.G], srgbToLinear[pt.B]}
		binary.LittleEndian.PutUint16(intensity[i*4:], binary.LittleEndian.Uint16(pt.Attributes[0:]))
		classification[i*4] = pt.Attributes[2]
	}
	positions := make([]byte, n*8)
	buildPositionRaw(positions, quantizePositions(coords), n)
	colorData := make([]byte, n*4)
	buildColorRaw(colorData, colors, n)
	expected := map[string][]byte{
		"POSITION":        positions,
		"COLOR_0":         colorData,
		"_INTENSITY":      intensity,
		"_CLASSIFICATION": classification,
	}

	modes := []struct {
		extension string
		header    byte
		encoder   *CompressedGltfEncoder
	}{
		{extMeshoptCompression, 0xa0, New(attrs)},
		{khrMeshoptCompression, 0xa1, New(attrs, WithKHRMeshopt())},
	}
	for _, mode := range modes {
		glb := encodeWith(t, mode.encoder, newMockNodeWithSummaries(summaries, points...))
		doc := parseGLBJSON(t, glb)
		bin := parseGLBBinary(t, glb)
		bufferViews := mustList(t, doc, "bufferViews")
		accessors := mustList(t, doc, "accessors")
		primitive := mustMapFromValue(t, mustList(t, mustMapFromValue(t, mustList(t, doc, "meshes")[0], "mesh"), "primitives")[0], "primitive")
		primAttrs := mustMap(t, primitive, "attributes")
		if len(primAttrs) != len(expected) {
			t.Fatalf("%s: unexpected primitive attributes %#v", mode.extension, primAttrs)
		}
		for name, want := range expected {
			accessor := mustMapFromValue(t, accessors[int(mustNumber(t, primAttrs, name))], name)
			bufferView := mustMapFromValue(t, bufferViews[int(mustNumber(t, accessor, "bufferView"))], name)
			meshopt := mustMap(t, mustMap(t, bufferView, "extensions"), mode.extension)
			offset := int(mustNumber(t, meshopt, "byteOffset"))
			length := int(mustNumber(t, meshopt, "byteLength"))
			stream := bin[offset : offset+length]
			if stream[0] != mode.header {
				t.Fatalf("%s %s: stream header %#x, want %#x", mode.extension, name, stream[0], mode.header)
			}
			decoded, err := decodeMeshoptAttributes(stream, int(mustNumber(t, meshopt, "count")), int(mustNumber(t, meshopt, "byteStride")))
			if err != nil {
				t.Fatalf("%s %s: decode: %v", mode.extension, name, err)
			}
			if !bytes.Equal(decoded, want) {
				t.Fatalf("%s %s: decoded buffer view differs from the tile data", mode.extension, name)
			}
		}
	}
}

// parseGLBBinary returns the contents of the binary chunk of a GLB file.
func parseGLBBinary(t *testing.T, data []byte) []byte {
	t.Helper()
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	chunk := 20 + jsonLen
	if chunk+8 > len(data) {
		t.Fatalf("GLB has no binary chunk")
	}
	binLen := int(binary.LittleEndian.Uint32(data[chunk : chunk+4]))
	if string(data[chunk+4:chunk+8]) != "BIN\x00" {
		t.Fatalf("expected second chunk to be BIN, got %q", string(data[chunk+4:chunk+8]))
	}
	return data[chunk+8 : chunk+8+binLen]
}
