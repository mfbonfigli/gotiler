package glb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/spz"
)

func randomSplats(rng *rand.Rand, count, shCount int) []splat.Splat {
	uniform := func(low, high float64) float64 { return low + (high-low)*rng.Float64() }
	randomVec3 := func(low, high float64) geom.Vec3 {
		return geom.Vec3{X: uniform(low, high), Y: uniform(low, high), Z: uniform(low, high)}
	}
	splats := make([]splat.Splat, count)
	for i := range splats {
		splats[i] = splat.Splat{
			Center:   randomVec3(-50, 50),
			Rotation: geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize(),
			Scale:    randomVec3(0.001, 1),
			Opacity:  rng.Float64(),
			SH0:      randomVec3(-2, 2),
		}
		for range shCount {
			splats[i].SHRest = append(splats[i].SHRest, randomVec3(-0.9, 0.9))
		}
	}
	return splats
}

func write(t *testing.T, splats []splat.Splat, version spz.Version) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Write(&out, splats, version); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func payload(t *testing.T, splats []splat.Splat, version spz.Version) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := spz.Encode(&out, splats, version); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func readDocument(t *testing.T, data []byte) *gltf.Document {
	t.Helper()
	doc := new(gltf.Document)
	if err := gltf.NewDecoder(bytes.NewReader(data)).Decode(doc); err != nil {
		t.Fatalf("the output is not a valid GLB: %v", err)
	}
	return doc
}

type wantAccessor struct {
	accessorType  gltf.AccessorType
	componentType gltf.ComponentType
	normalized    bool
}

func wantAttributes(shCount int) map[string]wantAccessor {
	vec3 := wantAccessor{gltf.AccessorVec3, gltf.ComponentFloat, false}
	attributes := map[string]wantAccessor{
		"POSITION":                        vec3,
		"COLOR_0":                         {gltf.AccessorVec4, gltf.ComponentUbyte, true},
		"KHR_gaussian_splatting:SCALE":    vec3,
		"KHR_gaussian_splatting:ROTATION": {gltf.AccessorVec4, gltf.ComponentFloat, false},
	}
	names := []string{
		"SH_DEGREE_1_COEF_0", "SH_DEGREE_1_COEF_1", "SH_DEGREE_1_COEF_2",
		"SH_DEGREE_2_COEF_0", "SH_DEGREE_2_COEF_1", "SH_DEGREE_2_COEF_2", "SH_DEGREE_2_COEF_3", "SH_DEGREE_2_COEF_4",
		"SH_DEGREE_3_COEF_0", "SH_DEGREE_3_COEF_1", "SH_DEGREE_3_COEF_2", "SH_DEGREE_3_COEF_3", "SH_DEGREE_3_COEF_4", "SH_DEGREE_3_COEF_5", "SH_DEGREE_3_COEF_6",
	}
	for _, name := range names[:shCount] {
		attributes["KHR_gaussian_splatting:"+name] = vec3
	}
	return attributes
}

func TestWriteProducesASplatGLB(t *testing.T) {
	for _, version := range []spz.Version{spz.Version2, spz.Version3} {
		for degree, shCount := range []int{0, 3, 8, 15} {
			t.Run(fmt.Sprintf("v%d degree %d", version, degree), func(t *testing.T) {
				splats := randomSplats(rand.New(rand.NewPCG(uint64(version), uint64(degree))), 200, shCount)
				doc := readDocument(t, write(t, splats, version))

				extensions := []string{"KHR_gaussian_splatting", "KHR_gaussian_splatting_compression_spz_2"}
				if !slices.Equal(doc.ExtensionsUsed, extensions) || !slices.Equal(doc.ExtensionsRequired, extensions) {
					t.Errorf("extensions used %v, required %v; want %v for both", doc.ExtensionsUsed, doc.ExtensionsRequired, extensions)
				}
				if doc.Asset.Version != "2.0" || doc.Asset.Generator != "gotiler" {
					t.Errorf("asset = %+v, want version 2.0 by gotiler", doc.Asset)
				}

				want := payload(t, splats, version)
				if len(doc.Buffers) != 1 || !bytes.Equal(doc.Buffers[0].Data, want) {
					t.Fatalf("the GLB holds %d buffers, want one holding the SPZ payload", len(doc.Buffers))
				}
				if v := doc.BufferViews; len(v) != 1 || v[0].Buffer != 0 || v[0].ByteOffset != 0 || v[0].ByteLength != len(want) || v[0].ByteStride != 0 {
					t.Errorf("buffer views = %+v, want one spanning the %d-byte payload", doc.BufferViews, len(want))
				}

				if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
					t.Fatalf("the GLB holds %d meshes, want one with one primitive", len(doc.Meshes))
				}
				primitive := doc.Meshes[0].Primitives[0]
				if primitive.Mode != gltf.PrimitivePoints {
					t.Errorf("primitive mode = %v, want points", primitive.Mode)
				}
				assertAttributes(t, doc, primitive.Attributes, len(splats), wantAttributes(shCount))
				assertSplattingExtension(t, primitive.Extensions)

				if len(doc.Nodes) != 1 || doc.Nodes[0].Mesh == nil || *doc.Nodes[0].Mesh != 0 || doc.Nodes[0].Matrix != zUpToYUp {
					t.Errorf("nodes = %+v, want one holding mesh 0 with the Z-up to Y-up matrix", doc.Nodes)
				}
				if doc.Scene == nil || *doc.Scene != 0 || !slices.Equal(doc.Scenes[0].Nodes, []int{0}) {
					t.Errorf("scene %v with nodes %v, want scene 0 holding node 0", doc.Scene, doc.Scenes[0].Nodes)
				}
			})
		}
	}
}

func assertAttributes(t *testing.T, doc *gltf.Document, attributes gltf.PrimitiveAttributes, count int, want map[string]wantAccessor) {
	t.Helper()
	if len(attributes) != len(want) {
		t.Errorf("the primitive has %d attributes, want %d", len(attributes), len(want))
	}
	for name, w := range want {
		index, ok := attributes[name]
		if !ok {
			t.Errorf("the primitive has no %s attribute", name)
			continue
		}
		a := doc.Accessors[index]
		if a.BufferView != nil || a.Count != count || a.Type != w.accessorType || a.ComponentType != w.componentType || a.Normalized != w.normalized {
			t.Errorf("%s accessor = %+v, want no buffer view, %d %v %v, normalized %v", name, a, count, w.accessorType, w.componentType, w.normalized)
		}
		if name != "POSITION" && (a.Min != nil || a.Max != nil) {
			t.Errorf("%s accessor has min %v and max %v, want none", name, a.Min, a.Max)
		}
	}
}

func assertSplattingExtension(t *testing.T, extensions gltf.Extensions) {
	t.Helper()
	raw, err := json.Marshal(extensions["KHR_gaussian_splatting"])
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kernel     string `json:"kernel"`
		ColorSpace string `json:"colorSpace"`
		Extensions map[string]struct {
			BufferView *int `json:"bufferView"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("KHR_gaussian_splatting extension %s: %v", raw, err)
	}
	compression, ok := got.Extensions["KHR_gaussian_splatting_compression_spz_2"]
	if got.Kernel != "ellipse" || got.ColorSpace != "srgb_rec709_display" || !ok || compression.BufferView == nil || *compression.BufferView != 0 {
		t.Errorf("KHR_gaussian_splatting extension = %s, want an ellipse kernel, sRGB colors and the SPZ payload in buffer view 0", raw)
	}
}

func TestWritePositionBounds(t *testing.T) {
	splats := randomSplats(rand.New(rand.NewPCG(1, 2)), 3, 0)
	splats[0].Center = geom.Vec3{X: -1, Y: 5, Z: 2}
	splats[1].Center = geom.Vec3{X: 3, Y: -4, Z: 2.5}
	splats[2].Center = geom.Vec3{X: 0, Y: 0, Z: -7}
	doc := readDocument(t, write(t, splats, spz.DefaultVersion))
	position := doc.Accessors[doc.Meshes[0].Primitives[0].Attributes["POSITION"]]
	if !slices.Equal(position.Min, []float64{-1, -4, -7}) || !slices.Equal(position.Max, []float64{3, 5, 2.5}) {
		t.Errorf("POSITION min %v, max %v; want [-1 -4 -7] and [3 5 2.5]", position.Min, position.Max)
	}
}

func TestWriteGLBContainer(t *testing.T) {
	data := write(t, randomSplats(rand.New(rand.NewPCG(3, 4)), 101, 3), spz.DefaultVersion)
	if string(data[:4]) != "glTF" || binary.LittleEndian.Uint32(data[4:]) != 2 || int(binary.LittleEndian.Uint32(data[8:])) != len(data) {
		t.Fatalf("GLB header = %v, want glTF version 2 of length %d", data[:12], len(data))
	}
	jsonLength := int(binary.LittleEndian.Uint32(data[12:]))
	binStart := 20 + jsonLength
	binLength := int(binary.LittleEndian.Uint32(data[binStart:]))
	if string(data[16:20]) != "JSON" || string(data[binStart+4:binStart+8]) != "BIN\x00" {
		t.Errorf("chunk types %q and %q, want JSON and BIN", data[16:20], data[binStart+4:binStart+8])
	}
	if jsonLength%4 != 0 || binLength%4 != 0 || binStart+8+binLength != len(data) {
		t.Errorf("chunk lengths %d and %d in %d bytes, want 4-byte aligned chunks filling the file", jsonLength, binLength, len(data))
	}
}

func TestWriteReusesBuffers(t *testing.T) {
	small := randomSplats(rand.New(rand.NewPCG(5, 6)), 50, 8)
	want := write(t, small, spz.Version3)
	// About 40,000 random splats of degree 3 compress to more than the pooled
	// capacity, so the grown buffer must not leak into later tiles.
	large := randomSplats(rand.New(rand.NewPCG(7, 8)), 40_000, 15)
	for range 2 {
		write(t, large, spz.Version2)
		if got := write(t, small, spz.Version3); !bytes.Equal(got, want) {
			t.Fatal("a tile written after a larger one differs from the same tile written first")
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteErrors(t *testing.T) {
	var out bytes.Buffer
	err := Write(&out, nil, spz.DefaultVersion)
	if err == nil || !strings.Contains(err.Error(), "no splats to encode") {
		t.Errorf("error = %v, want the SPZ encoder's error for no splats", err)
	}
	if out.Len() != 0 {
		t.Errorf("Write wrote %d bytes before failing, want none", out.Len())
	}

	errDiskFull := errors.New("disk full")
	err = Write(failingWriter{errDiskFull}, randomSplats(rand.New(rand.NewPCG(9, 10)), 10, 0), spz.DefaultVersion)
	if !errors.Is(err, errDiskFull) {
		t.Errorf("error = %v, want %v", err, errDiskFull)
	}
}

func BenchmarkWrite(b *testing.B) {
	splats := randomSplats(rand.New(rand.NewPCG(1, 1)), 20_000, 15)
	b.ReportAllocs()
	for b.Loop() {
		if err := Write(io.Discard, splats, spz.DefaultVersion); err != nil {
			b.Fatal(err)
		}
	}
}
