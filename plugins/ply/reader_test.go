package ply

import (
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

func writeTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestReader(t *testing.T, path string, attrs ...string) *Reader {
	t.Helper()
	r, err := NewReader(path, "EPSG:32633", model.NewAttributes(attrs...))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

func readAllPoints(t *testing.T, r pointcloud.Reader) []geom.Point64 {
	t.Helper()
	var points []geom.Point64
	for {
		pt, err := r.GetNext()
		if err == io.EOF {
			return points
		}
		if err != nil {
			t.Fatalf("GetNext: %v", err)
		}
		points = append(points, pt)
	}
}

func float32Attribute(v float32) model.AttributeValues {
	b := make([]byte, 4)
	putAttribute(b, model.AttributeFloat32, float64(v))
	return b
}

// colorCloud is a small cloud with a face element before the vertices and a
// vertex the reader skips because of its NaN coordinate.
var colorCloud = []testElement{
	{name: "face", properties: []property{listProp("vertex_indices", uint8Type, int32Type)}, records: [][]float64{{3, 0, 1, 2}}},
	{
		name: "vertex",
		properties: []property{
			scalarProp("x", float64Type), scalarProp("y", float64Type), scalarProp("z", float64Type),
			scalarProp("red", uint8Type), scalarProp("green", uint8Type), scalarProp("blue", uint8Type),
			scalarProp("scalar_Intensity", float32Type),
		},
		records: [][]float64{
			{500000.5, 4500000.25, 100, 255, 128, 0, 0.5},
			{math.NaN(), 1, 1, 0, 0, 0, 0},
			{500001.5, 4500001.25, 101, 0, 64, 255, 0.25},
		},
	},
}

var colorCloudPoints = []geom.Point64{
	{Vector: model.Vector{X: 500000.5, Y: 4500000.25, Z: 100}, R: 255, G: 128, B: 0, Attributes: float32Attribute(0.5)},
	{Vector: model.Vector{X: 500001.5, Y: 4500001.25, Z: 101}, R: 0, G: 64, B: 255, Attributes: float32Attribute(0.25)},
}

func TestReaderIsRegisteredForTheplyExtension(t *testing.T) {
	if !plugin.IsPointCloudExtension("scan.PLY") {
		t.Fatal(".ply is not a registered point cloud extension")
	}
	factory, ok := plugin.PointCloudReaderFactoryFor(".ply")
	if !ok {
		t.Fatal("no reader factory registered for .ply")
	}
	path := writeTestFile(t, "cloud.ply", buildPLY(t, formatBinaryLittleEndian, colorCloud...))
	r, err := factory(path, "EPSG:32633", plugin.ReaderOptions{RequestedAttributes: model.NewAttributes("intensity")})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer r.Close()
	if want := []model.AttributeDescriptor{{Name: "intensity", Type: model.AttributeFloat32}}; !reflect.DeepEqual(r.AttributeSchema(), want) {
		t.Errorf("AttributeSchema = %v, want %v", r.AttributeSchema(), want)
	}
}

func TestReaderReadsPoints(t *testing.T) {
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			r := newTestReader(t, writeTestFile(t, "cloud.ply", buildPLY(t, f, colorCloud...)), "intensity", "classification")
			if r.NumberOfPoints() != 3 {
				t.Errorf("NumberOfPoints = %d, want the 3 declared vertices", r.NumberOfPoints())
			}
			if r.GetCRS() != "EPSG:32633" {
				t.Errorf("GetCRS = %q", r.GetCRS())
			}
			if want := []model.AttributeDescriptor{{Name: "intensity", Type: model.AttributeFloat32}}; !reflect.DeepEqual(r.AttributeSchema(), want) {
				t.Errorf("AttributeSchema = %v, want %v", r.AttributeSchema(), want)
			}
			if got := readAllPoints(t, r); !reflect.DeepEqual(got, colorCloudPoints) {
				t.Errorf("points = %+v, want %+v", got, colorCloudPoints)
			}
			if _, err := r.GetNext(); err != io.EOF {
				t.Errorf("GetNext after the last point = %v, want io.EOF", err)
			}
		})
	}
}

func TestReaderResetRestartsFromTheFirstPoint(t *testing.T) {
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			r := newTestReader(t, writeTestFile(t, "cloud.ply", buildPLY(t, f, colorCloud...)), "intensity")
			if _, err := r.GetNext(); err != nil {
				t.Fatalf("GetNext: %v", err)
			}
			if err := r.Reset(); err != nil {
				t.Fatalf("Reset: %v", err)
			}
			if got := readAllPoints(t, r); !reflect.DeepEqual(got, colorCloudPoints) {
				t.Errorf("points after Reset = %+v, want %+v", got, colorCloudPoints)
			}
		})
	}
}

func TestReaderCRS(t *testing.T) {
	path := writeTestFile(t, "cloud.ply", buildPLY(t, formatASCII, colorCloud...))

	_, err := NewReader(path, "", nil)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "--crs local") {
		t.Errorf("error = %v, want a missing CRS error naming the file and suggesting --crs local", err)
	}

	r, err := NewReader(path, pointcloud.CRSLocal, nil)
	if err != nil {
		t.Fatalf("NewReader with the local CRS: %v", err)
	}
	defer r.Close()
	if r.GetCRS() != pointcloud.CRSLocal {
		t.Errorf("GetCRS = %q, want %q", r.GetCRS(), pointcloud.CRSLocal)
	}
}

func TestReaderOpenErrors(t *testing.T) {
	splat := testElement{
		name:       "vertex",
		properties: floatProps("x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"),
		records:    [][]float64{{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}},
	}
	complete := buildPLY(t, formatBinaryBigEndian, colorCloud...)
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"not a PLY file", []byte("solid cube\nfacet normal 0 0 1\n"), "not a PLY file"},
		{"no vertex element", []byte("ply\nformat ascii 1.0\nelement face 0\nend_header\n"), "PLY file has no vertex element"},
		{"missing coordinates", buildPLY(t, formatASCII, testElement{name: "vertex", properties: floatProps("x", "y")}), "missing coordinates: z"},
		{"Gaussian splat", buildPLY(t, formatBinaryLittleEndian, splat), "the file is a Gaussian splat PLY"},
		{"truncated binary", complete[:len(complete)-5], "the file is truncated: the header declares 3 vertices, needing 93 bytes, but only 88 bytes follow"},
		{"absurd vertex count", []byte("ply\nformat binary_little_endian 1.0\nelement vertex 9223372036854775807\nproperty double x\nproperty double y\nproperty double z\nend_header\n"), "the file is truncated: the header declares 9223372036854775807 vertices"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestFile(t, "cloud.ply", tc.data)
			_, err := NewReader(path, "EPSG:32633", nil)
			if err == nil {
				t.Fatalf("NewReader succeeded, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), path) {
				t.Errorf("error = %q, want it to name the file and contain %q", err, tc.wantErr)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		_, err := NewReader(filepath.Join(t.TempDir(), "missing.ply"), "EPSG:32633", nil)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error = %v, want fs.ErrNotExist", err)
		}
	})
}

func TestReaderReadErrorsNameTheFileAndVertex(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"invalid number", "1 2 3\n4 five 6\n7 8 9\n", `PLY vertex 1: property "y": invalid number "five"`},
		{"missing vertices", "1 2 3\n", "PLY vertex 1: data ends early, the header declares 3 vertices"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := "ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nend_header\n"
			path := writeTestFile(t, "cloud.ply", []byte(header+tc.body))
			r := newTestReader(t, path)
			if _, err := r.GetNext(); err != nil {
				t.Fatalf("first GetNext: %v", err)
			}
			_, err := r.GetNext()
			if err == nil || errors.Is(err, io.EOF) {
				t.Fatalf("GetNext = %v, want a read error", err)
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to name the file and contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestReaderSupportsConcurrentGetNext(t *testing.T) {
	const vertexCount = 2000
	vertex := testElement{name: "vertex", properties: xyzProperties}
	for i := range vertexCount {
		vertex.records = append(vertex.records, []float64{float64(i), 0, 0})
	}
	r := newTestReader(t, writeTestFile(t, "cloud.ply", buildPLY(t, formatBinaryLittleEndian, vertex)))

	var mu sync.Mutex
	var seen []int
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for {
				pt, err := r.GetNext()
				if err != nil {
					return
				}
				mu.Lock()
				seen = append(seen, int(pt.X))
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	slices.Sort(seen)
	if len(seen) != vertexCount {
		t.Fatalf("read %d points, want %d", len(seen), vertexCount)
	}
	for i, x := range seen {
		if x != i {
			t.Fatalf("point %d read %d times or not at all", i, slices.Index(seen, i))
		}
	}
}
