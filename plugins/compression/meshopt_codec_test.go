package compression

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// Streams encoded by the meshoptimizer reference encoder, see gen_vectors.mjs.
//
//go:embed testdata/meshopt
var meshoptVectorFiles embed.FS

type meshoptVector struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Stride  int    `json:"stride"`
	Input   string `json:"input"`
	Encoded []struct {
		File    string `json:"file"`
		Version int    `json:"version"`
		Level   int    `json:"level"`
	} `json:"encoded"`
}

func readMeshoptVectorFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := meshoptVectorFiles.ReadFile("testdata/meshopt/" + name)
	if err != nil {
		t.Fatalf("read meshopt vector file: %v", err)
	}
	return data
}

func readMeshoptVectors(t *testing.T) []meshoptVector {
	t.Helper()
	var vectors []meshoptVector
	if err := json.Unmarshal(readMeshoptVectorFile(t, "manifest.json"), &vectors); err != nil {
		t.Fatalf("parse meshopt vector manifest: %v", err)
	}
	return vectors
}

// assertMeshoptStatsCoverFormat fails unless stats include every control mode,
// group bit width and channel mode of the version 1 format.
func assertMeshoptStatsCoverFormat(t *testing.T, stats meshoptDecodeStats, channels bool) {
	t.Helper()
	for ctrl, n := range stats.controls {
		if n == 0 {
			t.Errorf("no byte position used control mode %d", ctrl)
		}
	}
	for _, width := range []int{0, 1, 2, 4, 8} {
		if stats.widths[width] == 0 {
			t.Errorf("no group used bit width %d", width)
		}
	}
	if channels {
		for mode, n := range stats.channels {
			if n == 0 {
				t.Errorf("no channel used channel mode %d", mode)
			}
		}
	}
}

// TestMeshoptDecoderMatchesReferenceEncoder checks the test decoder against
// the reference encoder, so that the round trips below validate the Go
// encoders against the reference implementation, not against themselves.
func TestMeshoptDecoderMatchesReferenceEncoder(t *testing.T) {
	var stats meshoptDecodeStats
	for _, v := range readMeshoptVectors(t) {
		input := readMeshoptVectorFile(t, v.Input)
		for _, enc := range v.Encoded {
			stream := readMeshoptVectorFile(t, enc.File)
			if got := int(stream[0] & 0x0f); got != enc.Version {
				t.Fatalf("%s: stream version %d, want %d", enc.File, got, enc.Version)
			}
			decoded, err := decodeMeshoptAttributesStats(stream, v.Count, v.Stride, &stats)
			if err != nil {
				t.Fatalf("%s: decode: %v", enc.File, err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatalf("%s: decoded data differs from the input", enc.File)
			}
		}
	}
	assertMeshoptStatsCoverFormat(t, stats, true)
}

// TestMeshoptV1EncoderMatchesReference checks that the version 1 encoder,
// which follows the reference heuristics, produces the same bytes as the
// reference encoder at level 3.
func TestMeshoptV1EncoderMatchesReference(t *testing.T) {
	compared := 0
	for _, v := range readMeshoptVectors(t) {
		input := readMeshoptVectorFile(t, v.Input)
		for _, enc := range v.Encoded {
			if enc.Version != 1 || enc.Level != 3 {
				continue
			}
			want := readMeshoptVectorFile(t, enc.File)
			got := encodeMeshoptAttributesV1Into(nil, input, v.Count, v.Stride)
			if !bytes.Equal(got, want) {
				t.Errorf("%s: encoded %d bytes, reference %d bytes, contents differ", v.Name, len(got), len(want))
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no version 1 level 3 reference vectors found")
	}
}

type meshoptTestCase struct {
	name          string
	count, stride int
	data          []byte
}

// meshoptTestCorpus returns deterministic streams of many shapes: sizes around
// group and block boundaries, and contents triggering every encoding mode.
func meshoptTestCorpus() []meshoptTestCase {
	rng := rand.New(rand.NewPCG(7, 11))
	patterns := []struct {
		name string
		fill func(data []byte, count, stride int)
	}{
		{"zeros", func(data []byte, count, stride int) {}},
		{"constant", func(data []byte, count, stride int) {
			element := make([]byte, stride)
			for k := range element {
				element[k] = byte(rng.Uint32())
			}
			for i := 0; i < count; i++ {
				copy(data[i*stride:], element)
			}
		}},
		{"ramp", func(data []byte, count, stride int) {
			for i := 0; i < count; i++ {
				for k := 0; k < stride; k++ {
					data[i*stride+k] = byte(i * (k + 1))
				}
			}
		}},
		{"random", func(data []byte, count, stride int) {
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
		}},
		{"sparse", func(data []byte, count, stride int) {
			for i := range data {
				if rng.IntN(20) == 0 {
					data[i] = byte(rng.Uint32())
				}
			}
		}},
		{"walk16", func(data []byte, count, stride int) {
			lanes := make([]uint16, stride/2)
			for i := 0; i < count; i++ {
				for l := range lanes {
					lanes[l] += uint16(rng.IntN(601) - 300)
					binary.LittleEndian.PutUint16(data[i*stride+2*l:], lanes[l])
				}
			}
		}},
		{"float32", func(data []byte, count, stride int) {
			for i := 0; i < count; i++ {
				for l := 0; l < stride/4; l++ {
					v := float32(1000 + float64(i)*0.37 + float64(l)*5 + rng.Float64()*0.1)
					binary.LittleEndian.PutUint32(data[i*stride+4*l:], math.Float32bits(v))
				}
			}
		}},
		{"bitfield", func(data []byte, count, stride int) {
			for i := 0; i < count; i++ {
				for l := 0; l < stride/4; l++ {
					binary.LittleEndian.PutUint32(data[i*stride+4*l:], 0x3f800000|uint32(rng.IntN(16))<<6)
				}
			}
		}},
	}
	var cases []meshoptTestCase
	for _, stride := range []int{4, 8, 12, 16, 64, 256} {
		counts := []int{1, 2, 15, 16, 17, 100, 255, 256, 257, 1000, 4097}
		if stride >= 64 {
			counts = []int{1, 17, 100, 257}
		}
		for _, count := range counts {
			for _, p := range patterns {
				data := make([]byte, count*stride)
				p.fill(data, count, stride)
				cases = append(cases, meshoptTestCase{
					name:   fmt.Sprintf("%s/stride%d/count%d", p.name, stride, count),
					count:  count,
					stride: stride,
					data:   data,
				})
			}
		}
	}
	return cases
}

func TestMeshoptEncodersRoundTrip(t *testing.T) {
	encoders := []struct {
		name   string
		header byte
		encode func(dst []byte, data []byte, count, stride int) []byte
	}{
		{"v0", 0xa0, encodeMeshoptAttributesInto},
		{"v1", 0xa1, encodeMeshoptAttributesV1Into},
	}
	for _, enc := range encoders {
		var stats meshoptDecodeStats
		for _, c := range meshoptTestCorpus() {
			prefix := []byte{0xde, 0xad}
			out := enc.encode(prefix, c.data, c.count, c.stride)
			if !bytes.Equal(out[:2], prefix) {
				t.Fatalf("%s %s: destination prefix overwritten", enc.name, c.name)
			}
			stream := out[2:]
			if stream[0] != enc.header {
				t.Fatalf("%s %s: header %#x, want %#x", enc.name, c.name, stream[0], enc.header)
			}
			decoded, err := decodeMeshoptAttributesStats(stream, c.count, c.stride, &stats)
			if err != nil {
				t.Fatalf("%s %s: decode: %v", enc.name, c.name, err)
			}
			if !bytes.Equal(decoded, c.data) {
				t.Fatalf("%s %s: decoded data differs from the input", enc.name, c.name)
			}
		}
		if enc.name == "v1" {
			assertMeshoptStatsCoverFormat(t, stats, true)
		}
	}
}

func TestMeshoptV1ChannelModesRoundTrip(t *testing.T) {
	modes := []byte{meshoptChannelBytes, meshoptChannelShort}
	for rot := 0; rot < 16; rot++ {
		modes = append(modes, byte(meshoptChannelXor|rot<<4))
	}
	for _, c := range meshoptTestCorpus() {
		if c.stride > 16 || c.count > 1000 {
			continue
		}
		for i := range modes {
			// a different mode on every channel, cycling through all modes
			channels := make([]byte, c.stride/4)
			for ch := range channels {
				channels[ch] = modes[(i+ch)%len(modes)]
			}
			stream := encodeMeshoptAttributesV1WithChannels(nil, c.data, c.count, c.stride, channels)
			decoded, err := decodeMeshoptAttributes(stream, c.count, c.stride)
			if err != nil {
				t.Fatalf("%s channels %x: decode: %v", c.name, channels, err)
			}
			if !bytes.Equal(decoded, c.data) {
				t.Fatalf("%s channels %x: decoded data differs from the input", c.name, channels)
			}
		}
	}
}

func TestMeshoptV1EncodingHandlesEmptyAndSingleElementStreams(t *testing.T) {
	prefix := []byte{1, 2, 3}
	if got := encodeMeshoptAttributesV1Into(prefix, nil, 0, 4); !bytes.Equal(got, prefix) {
		t.Fatalf("empty stream changed destination: got %#v want %#v", got, prefix)
	}

	// header, all-zero control byte (0xaa), tail padding, baseline, channel
	got := encodeMeshoptAttributesV1Into(nil, []byte{1, 2, 3, 4}, 1, 4)
	want := readMeshoptVectorFile(t, "single.v1l2.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("single element stream = %x, want reference %x", got, want)
	}
}

func TestZigzag16(t *testing.T) {
	cases := map[uint16]uint16{
		0:      0,
		1:      2,
		0xffff: 1, // -1
		0xfffe: 3, // -2
		0x7fff: 0xfffe,
		0x8000: 0xffff,
	}
	for in, want := range cases {
		if got := zigzag16(in); got != want {
			t.Fatalf("zigzag16(%#x) = %#x, want %#x", in, got, want)
		}
	}
}
