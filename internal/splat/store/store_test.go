package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// randomSplats returns splats with centers far from the origin, as
// georeferenced inputs have, numbered by SourceIndex.
func randomSplats(rng *rand.Rand, count, shCount int) []splat.Splat {
	randomVec3 := func(scale float64) geom.Vec3 {
		return geom.Vec3{X: scale * rng.NormFloat64(), Y: scale * rng.NormFloat64(), Z: scale * rng.NormFloat64()}
	}
	splats := make([]splat.Splat, count)
	for i := range splats {
		splats[i] = splat.Splat{
			Center:      geom.Vec3{X: 4_500_000 + rng.Float64(), Y: 500_000 + rng.Float64(), Z: 100 * rng.Float64()},
			Rotation:    geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize(),
			Scale:       randomVec3(1),
			Opacity:     rng.Float64(),
			SH0:         randomVec3(1),
			SourceIndex: 1000 + i,
		}
		for range shCount {
			splats[i].SHRest = append(splats[i].SHRest, randomVec3(0.5))
		}
	}
	return splats
}

// stored returns a splat as the store gives it back: every value but the
// center rounded to float32.
func stored(s splat.Splat) splat.Splat {
	f := func(v float64) float64 { return float64(float32(v)) }
	v := func(v geom.Vec3) geom.Vec3 { return geom.Vec3{X: f(v.X), Y: f(v.Y), Z: f(v.Z)} }
	out := s
	out.Rotation = geom.Quat{X: f(s.Rotation.X), Y: f(s.Rotation.Y), Z: f(s.Rotation.Z), W: f(s.Rotation.W)}
	out.Scale = v(s.Scale)
	out.Opacity = f(s.Opacity)
	out.SH0 = v(s.SH0)
	out.SHRest = nil
	for _, c := range s.SHRest {
		out.SHRest = append(out.SHRest, v(c))
	}
	return out
}

func assertSplats(t *testing.T, got, want []splat.Splat) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d splats, want %d", len(got), len(want))
	}
	for i := range want {
		if w := stored(want[i]); !reflect.DeepEqual(got[i], w) {
			t.Fatalf("splat %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func writeStore(t *testing.T, path string, shCount int, batches ...[]splat.Splat) {
	t.Helper()
	w, err := Create(path, shCount)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, batch := range batches {
		if err := w.Write(batch); err != nil {
			t.Fatal(err)
		}
		total += len(batch)
	}
	if w.Count() != total {
		t.Errorf("Count() = %d, want %d", w.Count(), total)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// readBatches reads a whole reader with Next, copying each batch.
func readBatches(t *testing.T, r *Reader) []splat.Splat {
	t.Helper()
	var all []splat.Splat
	var batch []splat.Splat
	for {
		var err error
		batch, err = r.Next(batch)
		if errors.Is(err, io.EOF) {
			if len(batch) != 0 {
				t.Errorf("Next returned %d splats with io.EOF, want none", len(batch))
			}
			return all
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) == 0 || len(batch) > BatchSize {
			t.Fatalf("Next returned %d splats, want 1 to %d", len(batch), BatchSize)
		}
		for _, s := range batch {
			s.SHRest = append([]geom.Vec3(nil), s.SHRest...)
			all = append(all, s)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, shCount := range []int{0, 3, 8, 15} {
		t.Run(fmt.Sprintf("%d coefficients", shCount), func(t *testing.T) {
			splats := randomSplats(rand.New(rand.NewPCG(1, uint64(shCount))), 2*BatchSize+123, shCount)
			path := filepath.Join(t.TempDir(), "splats.bin")
			writeStore(t, path, shCount, splats[:10], splats[10:BatchSize+500], splats[BatchSize+500:])
			if info, err := os.Stat(path); err != nil || info.Size() != int64(len(splats)*RecordSize(shCount)) {
				t.Fatalf("the store holds %v bytes (%v), want %d", info.Size(), err, len(splats)*RecordSize(shCount))
			}

			r, err := Open(path, shCount)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if r.Count() != len(splats) {
				t.Errorf("Count() = %d, want %d", r.Count(), len(splats))
			}
			assertSplats(t, readBatches(t, r), splats)
			if batch, err := r.Next(nil); len(batch) != 0 || !errors.Is(err, io.EOF) {
				t.Errorf("Next after the end returned %d splats and %v, want none and io.EOF", len(batch), err)
			}

			all, err := ReadAll(path, shCount)
			if err != nil {
				t.Fatal(err)
			}
			assertSplats(t, all, splats)
		})
	}
}

func TestRecordSize(t *testing.T) {
	for shCount, want := range map[int]int{0: 76, 3: 112, 8: 172, 15: 256} {
		if got := RecordSize(shCount); got != want {
			t.Errorf("RecordSize(%d) = %d, want %d", shCount, got, want)
		}
	}
}

func TestReadAllGivesEachSplatItsOwnCoefficients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	writeStore(t, path, 3, randomSplats(rand.New(rand.NewPCG(2, 3)), 10, 3))
	all, err := ReadAll(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	next := all[1].SHRest[0]
	_ = append(all[0].SHRest, geom.Vec3{X: 99})
	all[0].SHRest[0] = geom.Vec3{X: -99}
	if all[1].SHRest[0] != next {
		t.Errorf("changing splat 0's coefficients changed splat 1's to %+v", all[1].SHRest[0])
	}
}

func TestNextReusesItsCoefficientMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	splats := randomSplats(rand.New(rand.NewPCG(4, 5)), BatchSize+1, 3)
	writeStore(t, path, 3, splats)
	r, err := Open(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	first, err := r.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	kept := first[0]
	if _, err := r.Next(nil); err != nil {
		t.Fatal(err)
	}
	if want := stored(splats[BatchSize]).SHRest[0]; kept.SHRest[0] != want {
		t.Errorf("after the second batch, the first batch's coefficients hold %+v, want the second batch's %+v", kept.SHRest[0], want)
	}
}

func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	splats := randomSplats(rand.New(rand.NewPCG(6, 7)), 15, 8)
	for _, part := range [][]splat.Splat{splats[:10], splats[10:]} {
		w, err := Append(path, 8)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(part); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ReadAll(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	assertSplats(t, all, splats)
}

func TestCreateTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	splats := randomSplats(rand.New(rand.NewPCG(8, 9)), 10, 0)
	writeStore(t, path, 0, splats)
	writeStore(t, path, 0, splats[:3])
	all, err := ReadAll(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertSplats(t, all, splats[:3])
}

func TestOpenRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	splats := randomSplats(rand.New(rand.NewPCG(10, 11)), 10_000, 3)
	writeStore(t, path, 3, splats)
	tests := []struct {
		start, count int
		want         []splat.Splat
	}{
		{0, -1, splats},
		{5000, -1, splats[5000:]},
		{4000, 4500, splats[4000:8500]},
		{9999, 1, splats[9999:]},
		{10_000, 0, nil},
		{10_000, -1, nil},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d+%d", tt.start, tt.count), func(t *testing.T) {
			r, err := OpenRange(path, 3, tt.start, tt.count)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if r.Count() != len(tt.want) {
				t.Errorf("Count() = %d, want %d", r.Count(), len(tt.want))
			}
			assertSplats(t, readBatches(t, r), tt.want)
		})
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "splats.bin")
	writeStore(t, path, 3, randomSplats(rand.New(rand.NewPCG(12, 13)), 10, 3))
	odd := filepath.Join(dir, "odd.bin")
	if err := os.WriteFile(odd, make([]byte, 5), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                  string
		path                  string
		shCount, start, count int
		want                  string
	}{
		{"negative start", path, 3, -1, 1, "cannot read 1 splats from index -1: the store holds 10"},
		{"past the end", path, 3, 9, 2, "cannot read 2 splats from index 9: the store holds 10"},
		{"start past the end", path, 3, 11, -1, "cannot read -1 splats from index 11: the store holds 10"},
		{"partial record", odd, 0, 0, -1, "the store holds 5 bytes, which is not a whole number of 76-byte splat records"},
		{"other record size", path, 0, 0, -1, "the store holds 1120 bytes, which is not a whole number of 76-byte splat records"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := OpenRange(tt.path, tt.shCount, tt.start, tt.count)
			if err == nil {
				r.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), tt.path) {
				t.Errorf("error = %v, want one naming %s and containing %q", err, tt.path, tt.want)
			}
		})
	}

	if _, err := Open(filepath.Join(dir, "missing.bin"), 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening a missing store: error = %v, want fs.ErrNotExist", err)
	}
	if _, err := ReadAll(filepath.Join(dir, "missing.bin"), 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("reading a missing store: error = %v, want fs.ErrNotExist", err)
	}
	if _, err := Create(filepath.Join(dir, "missing", "splats.bin"), 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("creating a store in a missing folder: error = %v, want fs.ErrNotExist", err)
	}
}

func TestNextReportsAStoreCutShort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "splats.bin")
	writeStore(t, path, 3, randomSplats(rand.New(rand.NewPCG(16, 17)), 10, 3))
	r, err := Open(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := os.Truncate(path, int64(5*RecordSize(3))); err != nil {
		t.Fatal(err)
	}
	if batch, err := r.Next(nil); !errors.Is(err, io.ErrUnexpectedEOF) || len(batch) != 0 {
		t.Errorf("Next returned %d splats and %v, want none and io.ErrUnexpectedEOF", len(batch), err)
	}
}

func TestWriteRejectsOtherSHDegrees(t *testing.T) {
	w, err := Create(filepath.Join(t.TempDir(), "splats.bin"), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	splats := randomSplats(rand.New(rand.NewPCG(14, 15)), 2, 3)
	splats[1].SHRest = nil
	err = w.Write(splats)
	if err == nil || !strings.Contains(err.Error(), "splat 1 has 0 spherical-harmonic coefficients, but the store holds 3") {
		t.Errorf("error = %v, want one about splat 1's coefficients", err)
	}
}

func TestCloseTwice(t *testing.T) {
	w, err := Create(filepath.Join(t.TempDir(), "splats.bin"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("closing a closed writer: error = %v, want nil", err)
	}
}

func BenchmarkReadAll(b *testing.B) {
	path := filepath.Join(b.TempDir(), "splats.bin")
	w, err := Create(path, 15)
	if err != nil {
		b.Fatal(err)
	}
	if err := w.Write(randomSplats(rand.New(rand.NewPCG(1, 1)), 20_000, 15)); err != nil {
		b.Fatal(err)
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ReadAll(path, 15); err != nil {
			b.Fatal(err)
		}
	}
}
