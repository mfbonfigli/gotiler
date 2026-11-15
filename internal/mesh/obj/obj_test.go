package obj

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string, opts Options) ([]mesh.Triangle, Info) {
	t.Helper()
	if opts.TempDir == "" {
		opts.TempDir = t.TempDir()
	}
	var triangles []mesh.Triangle
	info, err := Read(path, opts, func(tr *mesh.Triangle) error {
		triangles = append(triangles, *tr)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return triangles, info
}

func TestReadTexturedQuadWithMaterials(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "scene.mtl", "newmtl wall\nKd 1 0 0.5\nmap_Kd textures/wall.png\nnewmtl plain\nKd 0 1 0\n")
	path := write(t, dir, "scene.obj", strings.Join([]string{
		"# a quad and a triangle",
		"mtllib scene.mtl",
		"v 100.5 200.25 10 0.1 0.2 0.3",
		"v 101.5 200.25 10",
		"v 101.5 201.25 1.05e+01",
		"v 100.5 201.25 10",
		"vn 0 0 1",
		"vt 0 0",
		"vt 1 0",
		"vt 1 1",
		"vt 0 1",
		"usemtl wall",
		"f 1/1/1 2/2/1 3/3/1 4/4/1",
		"usemtl plain",
		"f -4 -3 -2",
		"",
	}, "\n"))

	triangles, info := read(t, path, Options{})

	if len(triangles) != 3 || info.Triangles != 3 || info.Vertices != 4 {
		t.Fatalf("got %d triangles, info %+v", len(triangles), info)
	}
	if info.Origin != [3]float64{100, 200, 10} {
		t.Errorf("origin = %v", info.Origin)
	}
	// The quad fans out of its first corner.
	want := mesh.Triangle{
		P:        [3][3]float32{{0.5, 0.25, 0}, {1.5, 1.25, 0.5}, {0.5, 1.25, 0}},
		UV:       [3][2]float32{{0, 0}, {1, 1}, {0, 1}},
		Material: 0,
	}
	if triangles[1] != want {
		t.Errorf("second triangle = %+v, want %+v", triangles[1], want)
	}
	if triangles[2].Material != 1 || triangles[2].P[2] != [3]float32{1.5, 1.25, 0.5} {
		t.Errorf("third triangle = %+v", triangles[2])
	}
	if len(info.Materials) != 2 {
		t.Fatalf("materials = %+v", info.Materials)
	}
	if got := info.Materials[0]; got.Texture != filepath.Join(dir, "textures", "wall.png") || got.Color != [3]uint8{255, 0, 128} {
		t.Errorf("wall = %+v", got)
	}
	if got := info.Materials[1]; got.Texture != "" || got.Color != [3]uint8{0, 255, 0} {
		t.Errorf("plain = %+v", got)
	}
}

func TestReadFacesWithoutMaterialOrLibrary(t *testing.T) {
	path := write(t, t.TempDir(), "bare.obj", "mtllib missing.mtl\nv 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\nusemtl later\nf 1//1 2//1 3//1\n")

	triangles, info := read(t, path, Options{})

	if len(triangles) != 2 || triangles[0].Material == triangles[1].Material {
		t.Fatalf("triangles = %+v", triangles)
	}
	if len(info.Materials) != 2 || info.Materials[triangles[1].Material].Name != "later" {
		t.Errorf("materials = %+v", info.Materials)
	}
}

func TestReadVerticesBeyondTheCache(t *testing.T) {
	const count = 3*pageRecords + 17
	var b strings.Builder
	for i := range count {
		fmt.Fprintf(&b, "v %d %d.5 0\nvt %d 0\n", i, i%7, i)
	}
	// Faces reach back to the first page, then forward again.
	fmt.Fprintf(&b, "f 1/1 2/2 %d/%d\nf %d/%d 3/3 %d/%d\n", count, count, pageRecords+5, pageRecords+5, 2*pageRecords+9, 2*pageRecords+9)
	path := write(t, t.TempDir(), "large.obj", b.String())

	// The cache holds two pages of each kind.
	triangles, _ := read(t, path, Options{CacheBytes: 1})

	if len(triangles) != 2 {
		t.Fatalf("got %d triangles", len(triangles))
	}
	last := float32(count - 1)
	if triangles[0].P[2] != [3]float32{last, float32((count-1)%7) + 0.5, 0} || triangles[0].UV[2][0] != last {
		t.Errorf("first triangle = %+v", triangles[0])
	}
	if triangles[1].P[0][0] != pageRecords+4 || triangles[1].P[1][0] != 2 || triangles[1].UV[2][0] != 2*pageRecords+8 {
		t.Errorf("second triangle = %+v", triangles[1])
	}
}

func TestReadReportsTheLineOfAnError(t *testing.T) {
	path := write(t, t.TempDir(), "bad.obj", "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 9\n")
	_, err := Read(path, Options{TempDir: t.TempDir()}, func(*mesh.Triangle) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "bad.obj, line 4") {
		t.Fatalf("err = %v", err)
	}
}

func TestNextFloat(t *testing.T) {
	for text, want := range map[string]float64{
		"1":                      1,
		"-0.5":                   -0.5,
		"+3.25e2":                325,
		"4.38694389519666e+03":   4386.94389519666,
		"4.8264742427293109e+02": 482.64742427293109,
		"1e-30":                  1e-30,
		".5":                     0.5,
		"0.000001":               0.000001,
	} {
		got, rest, ok := nextFloat([]byte("  " + text + " tail"))
		if !ok || got != want || string(rest) != " tail" {
			t.Errorf("nextFloat(%q) = %v, %q, %v, want %v", text, got, rest, ok, want)
		}
	}
	if _, _, ok := nextFloat([]byte("abc")); ok {
		t.Error("nextFloat accepted letters")
	}
}
