package ply

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// rawSplat holds the values a trainer writes for one splat: a log scale, an
// opacity logit, a W, X, Y, Z rotation and the f_rest values channel by
// channel.
type rawSplat struct {
	center, sh0, logScale [3]float64
	opacityLogit          float64
	rotationWXYZ          [4]float64
	shRest                []float64
}

// values returns the values of the splat keyed by property name.
func (r rawSplat) values() map[string]float64 {
	v := map[string]float64{
		"x": r.center[0], "y": r.center[1], "z": r.center[2],
		"f_dc_0": r.sh0[0], "f_dc_1": r.sh0[1], "f_dc_2": r.sh0[2],
		"opacity": r.opacityLogit,
		"scale_0": r.logScale[0], "scale_1": r.logScale[1], "scale_2": r.logScale[2],
		"rot_0": r.rotationWXYZ[0], "rot_1": r.rotationWXYZ[1], "rot_2": r.rotationWXYZ[2], "rot_3": r.rotationWXYZ[3],
	}
	for i, value := range r.shRest {
		v[fmt.Sprintf("f_rest_%d", i)] = value
	}
	return v
}

func splatPropertyNames(shRestCount int) []string {
	names := slices.Clone(splatProperties)
	for i := range shRestCount {
		names = append(names, fmt.Sprintf("f_rest_%d", i))
	}
	return names
}

// splatElement returns a vertex element with the given float properties,
// holding the splats.
func splatElement(names []string, splats ...rawSplat) testElement {
	e := testElement{name: "vertex", properties: floatProps(names...)}
	for _, s := range splats {
		values := s.values()
		record := make([]float64, len(names))
		for i, name := range names {
			record[i] = values[strings.ToLower(name)]
		}
		e.records = append(e.records, record)
	}
	return e
}

func openSplatReader(t *testing.T, data []byte) *SplatReader {
	t.Helper()
	r, err := NewSplatReader(writeTestFile(t, "splats.ply", data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() }) //nolint:errcheck
	return r
}

func readAllSplats(t *testing.T, r *SplatReader) []splat.Splat {
	t.Helper()
	var splats []splat.Splat
	for {
		s, err := r.Next()
		if errors.Is(err, io.EOF) {
			return splats
		}
		if err != nil {
			t.Fatal(err)
		}
		splats = append(splats, s)
	}
}

func assertSplats(t *testing.T, got, want []splat.Splat) {
	t.Helper()
	// float properties round the values written by the tests to float32
	const tolerance = 1e-6
	near := func(a, b float64) bool { return math.Abs(a-b) <= tolerance*math.Max(1, math.Abs(b)) }
	nearVec3 := func(a, b geom.Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }
	if len(got) != len(want) {
		t.Fatalf("read %d splats, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		same := nearVec3(g.Center, w.Center) && nearVec3(g.Scale, w.Scale) && nearVec3(g.SH0, w.SH0) &&
			near(g.Rotation.X, w.Rotation.X) && near(g.Rotation.Y, w.Rotation.Y) && near(g.Rotation.Z, w.Rotation.Z) && near(g.Rotation.W, w.Rotation.W) &&
			near(g.Opacity, w.Opacity) && g.SourceIndex == w.SourceIndex && len(g.SHRest) == len(w.SHRest)
		for c := range w.SHRest {
			same = same && c < len(g.SHRest) && nearVec3(g.SHRest[c], w.SHRest[c])
		}
		if !same {
			t.Errorf("splat %d = %+v, want %+v", i, g, w)
		}
	}
}

var (
	// firstSplat has a degree-1 color whose coefficients are numbered by
	// channel in the file: red 1-3, green 4-6, blue 7-9.
	firstSplat = rawSplat{
		center:       [3]float64{1, 2, 3},
		sh0:          [3]float64{0.5, -0.25, 1},
		logScale:     [3]float64{math.Log(0.5), 0, math.Log(2)},
		opacityLogit: 0,
		rotationWXYZ: [4]float64{2, 0, 0, 0},
		shRest:       []float64{1, 2, 3, 4, 5, 6, 7, 8, 9},
	}
	firstSplatRead = splat.Splat{
		Center:   geom.Vec3{X: 1, Y: 2, Z: 3},
		Rotation: geom.IdentityQuat(),
		Scale:    geom.Vec3{X: 0.5, Y: 1, Z: 2},
		Opacity:  0.5,
		SH0:      geom.Vec3{X: 0.5, Y: -0.25, Z: 1},
		SHRest:   []geom.Vec3{{X: 1, Y: 4, Z: 7}, {X: 2, Y: 5, Z: 8}, {X: 3, Y: 6, Z: 9}},
	}
	secondSplat = rawSplat{
		center:       [3]float64{-4, 0.25, 8},
		opacityLogit: math.Log(9),
		rotationWXYZ: [4]float64{0, 0, 0, 1},
		shRest:       make([]float64, 9),
	}
	secondSplatRead = splat.Splat{
		Center:      geom.Vec3{X: -4, Y: 0.25, Z: 8},
		Rotation:    geom.Quat{Z: 1},
		Scale:       geom.Vec3{X: 1, Y: 1, Z: 1},
		Opacity:     0.9,
		SHRest:      make([]geom.Vec3, 3),
		SourceIndex: 1,
	}
)

func TestSplatReaderReadsSplats(t *testing.T) {
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			r := openSplatReader(t, buildPLY(t, f, splatElement(splatPropertyNames(9), firstSplat, secondSplat)))
			if r.NumberOfSplats() != 2 {
				t.Errorf("NumberOfSplats = %d, want 2", r.NumberOfSplats())
			}
			if r.SHDegree() != 1 {
				t.Errorf("SHDegree = %d, want 1", r.SHDegree())
			}
			assertSplats(t, readAllSplats(t, r), []splat.Splat{firstSplatRead, secondSplatRead})
			if _, err := r.Next(); !errors.Is(err, io.EOF) {
				t.Errorf("Next after the last splat: err = %v, want io.EOF", err)
			}
			if r.Skipped() != 0 {
				t.Errorf("Skipped = %d, want 0", r.Skipped())
			}
		})
	}
}

func TestSplatReaderSHDegrees(t *testing.T) {
	for degree, count := range []int{0, 9, 24, 45} {
		t.Run(fmt.Sprintf("degree %d", degree), func(t *testing.T) {
			raw := secondSplat
			raw.shRest = make([]float64, count)
			for i := range raw.shRest {
				raw.shRest[i] = float64(i)
			}
			want := secondSplatRead
			want.SourceIndex = 0
			want.SHRest = nil
			perChannel := count / 3
			for i := range perChannel {
				want.SHRest = append(want.SHRest, geom.Vec3{X: float64(i), Y: float64(i + perChannel), Z: float64(i + 2*perChannel)})
			}

			r := openSplatReader(t, buildPLY(t, formatBinaryLittleEndian, splatElement(splatPropertyNames(count), raw)))
			if r.SHDegree() != degree {
				t.Errorf("SHDegree = %d, want %d", r.SHDegree(), degree)
			}
			assertSplats(t, readAllSplats(t, r), []splat.Splat{want})
		})
	}
}

func TestSplatReaderFindsPropertiesInAnyOrder(t *testing.T) {
	names := splatPropertyNames(9)
	slices.Reverse(names)
	names[slices.Index(names, "opacity")] = "Opacity"
	names = append([]string{"nx", "ny", "nz"}, names...)
	vertex := splatElement(names, firstSplat)
	vertex.properties = append(vertex.properties, scalarProp("red", uint8Type), listProp("neighbors", uint8Type, int32Type))
	vertex.records[0] = append(vertex.records[0], 200, 2, 7, 9)

	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			r := openSplatReader(t, buildPLY(t, f, vertex))
			assertSplats(t, readAllSplats(t, r), []splat.Splat{firstSplatRead})
		})
	}
}

func TestSplatReaderSkipsInvalidSplats(t *testing.T) {
	nan := math.NaN()
	invalid := func(modify func(*rawSplat)) rawSplat {
		s := secondSplat
		s.shRest = slices.Clone(s.shRest)
		modify(&s)
		return s
	}
	splats := []rawSplat{
		firstSplat,
		invalid(func(s *rawSplat) { s.center[1] = nan }),
		invalid(func(s *rawSplat) { s.logScale[2] = 1000 }),
		invalid(func(s *rawSplat) { s.opacityLogit = nan }),
		invalid(func(s *rawSplat) { s.rotationWXYZ[2] = nan }),
		invalid(func(s *rawSplat) { s.shRest[4] = nan }),
		secondSplat,
	}
	want := secondSplatRead
	want.SourceIndex = 6

	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			r := openSplatReader(t, buildPLY(t, f, splatElement(splatPropertyNames(9), splats...)))
			assertSplats(t, readAllSplats(t, r), []splat.Splat{firstSplatRead, want})
			if r.Skipped() != 5 {
				t.Errorf("Skipped = %d, want 5", r.Skipped())
			}
			if r.NumberOfSplats() != 7 {
				t.Errorf("NumberOfSplats = %d, want the 7 vertices of the header", r.NumberOfSplats())
			}
		})
	}
}

func TestSplatReaderOpenErrors(t *testing.T) {
	complete := buildPLY(t, formatBinaryLittleEndian, splatElement(splatPropertyNames(0), secondSplat, secondSplat))
	withNames := func(names ...string) []byte {
		return buildPLY(t, formatBinaryLittleEndian, splatElement(names, secondSplat))
	}
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"not a PLY file", []byte("solid cube\n"), "not a PLY file"},
		{"no vertex element", []byte("ply\nformat ascii 1.0\nelement face 0\nend_header\n"), "PLY file has no vertex element"},
		{
			"point cloud",
			buildPLY(t, formatASCII, testElement{name: "vertex", properties: floatProps("x", "y", "z", "red", "green", "blue")}),
			"the file is not a Gaussian splat PLY: its vertex element is missing f_dc_0, f_dc_1, f_dc_2, opacity, scale_0, scale_1, scale_2, rot_0, rot_1, rot_2, rot_3",
		},
		{
			"compressed splats",
			buildPLY(t, formatBinaryLittleEndian, testElement{name: "vertex", properties: floatProps("packed_position", "packed_rotation", "packed_scale", "packed_color")}),
			"the file is a compressed Gaussian splat PLY, which is not supported yet",
		},
		{"incomplete degree", withNames(splatPropertyNames(6)...), "the file has 6 f_rest_ properties: spherical harmonics of degree 1, 2 or 3 need 9, 24 or 45"},
		{"gap in the coefficients", withNames(append(splatPropertyNames(8), "f_rest_9")...), "the file has no f_rest_8 property, but has higher ones"},
		{"coefficient without a number", withNames(append(splatPropertyNames(8), "f_rest_x")...), "the property f_rest_x is not a spherical-harmonic coefficient"},
		{"coefficient with a leading zero", withNames(append(splatPropertyNames(8), "f_rest_08")...), "the property f_rest_08 is not a spherical-harmonic coefficient"},
		{"truncated binary", complete[:len(complete)-3], "the file is truncated: the header declares 2 vertices, needing 112 bytes, but only 109 bytes follow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestFile(t, "splats.ply", tt.data)
			_, err := NewSplatReader(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), path) {
				t.Errorf("error = %v, want one naming %s and containing %q", err, path, tt.wantErr)
			}
		})
	}

	if _, err := NewSplatReader("does-not-exist.ply"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening a missing file: err = %v, want fs.ErrNotExist", err)
	}
}

func TestSplatReaderReportsDataEndingEarly(t *testing.T) {
	data := buildPLY(t, formatASCII, splatElement(splatPropertyNames(0), secondSplat, secondSplat))
	lines := strings.SplitAfter(string(data), "\n")
	path := writeTestFile(t, "splats.ply", []byte(strings.Join(lines[:len(lines)-2], "")))

	r, err := NewSplatReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck
	if _, err := r.Next(); err != nil {
		t.Fatalf("reading the first splat: %v", err)
	}
	if _, err := r.Next(); err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "data ends early") {
		t.Errorf("reading past the data: err = %v, want a data ends early error naming %s", err, path)
	}
}
