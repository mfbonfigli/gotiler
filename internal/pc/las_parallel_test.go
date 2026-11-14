package pc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfbonfigli/golaz"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// ---------------------------------------------------------------------------
// lasChunkPipeline, driven by fake decoders
// ---------------------------------------------------------------------------

// fakeChunkPoints is the content of fake chunk i: i%5+1 points whose X encodes
// chunk and position, so any reordering or loss shows up in the sequence.
func fakeChunkPoints(chunk int) []geom.Point64 {
	pts := make([]geom.Point64, chunk%5+1)
	for j := range pts {
		pts[j].X = float64(chunk*1000 + j)
	}
	return pts
}

// fakeDecoderFactory returns decoders that take a random short time per chunk,
// so chunks finish out of order. failAt, if >= 0, is a chunk that fails after
// emitting its first point. decoded counts the chunks decoded so far.
func fakeDecoderFactory(failAt int, decoded *atomic.Int64) func() (lasChunkDecoder, error) {
	var seed atomic.Int64
	return func() (lasChunkDecoder, error) {
		rng := rand.New(rand.NewSource(seed.Add(1)))
		return func(chunk int, dst []geom.Point64) ([]geom.Point64, error) {
			time.Sleep(time.Duration(rng.Intn(400)) * time.Microsecond)
			if decoded != nil {
				decoded.Add(1)
			}
			pts := fakeChunkPoints(chunk)
			if chunk == failAt {
				return append(dst, pts[0]), fmt.Errorf("chunk %d is corrupt", chunk)
			}
			return append(dst, pts...), nil
		}, nil
	}
}

// drainPipeline reads until an error and returns the X of every point.
func drainPipeline(p *lasChunkPipeline) ([]float64, error) {
	var xs []float64
	for {
		pt, err := p.next()
		if err != nil {
			return xs, err
		}
		xs = append(xs, pt.X)
	}
}

func fakeSequence(chunks int) []float64 {
	var xs []float64
	for c := 0; c < chunks; c++ {
		for _, pt := range fakeChunkPoints(c) {
			xs = append(xs, pt.X)
		}
	}
	return xs
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLasChunkPipelineDeliversChunksInOrder(t *testing.T) {
	for _, workers := range []int{1, 2, 5, 16} {
		p := newLasChunkPipeline(57, workers, fakeDecoderFactory(-1, nil))
		got, err := drainPipeline(p)
		if err != io.EOF {
			t.Fatalf("%d workers: ended with %v, want io.EOF", workers, err)
		}
		if want := fakeSequence(57); !equalFloats(got, want) {
			t.Fatalf("%d workers: points are not the chunks in order (%d points, want %d)", workers, len(got), len(want))
		}
		// The end of the stream is stable.
		for i := 0; i < 3; i++ {
			if _, err := p.next(); err != io.EOF {
				t.Fatalf("%d workers: read after the end: %v, want io.EOF", workers, err)
			}
		}
		p.close()
	}
}

// A failing chunk surfaces at its place in the stream: everything before it is
// delivered, nothing after it, and the error does not go away.
func TestLasChunkPipelineReportsErrorInOrder(t *testing.T) {
	p := newLasChunkPipeline(30, 4, fakeDecoderFactory(6, nil))
	defer p.close()
	got, err := drainPipeline(p)
	if err == nil || err == io.EOF {
		t.Fatalf("ended with %v, want the chunk error", err)
	}
	want := append(fakeSequence(6), fakeChunkPoints(6)[0].X)
	if !equalFloats(got, want) {
		t.Fatalf("got %d points before the error, want the %d that precede it", len(got), len(want))
	}
	for i := 0; i < 3; i++ {
		if _, again := p.next(); again == nil || again.Error() != err.Error() {
			t.Fatalf("read after the error: %v, want the same error %q", again, err)
		}
	}
}

func TestLasChunkPipelineDecoderCreationError(t *testing.T) {
	boom := errors.New("no decoder for you")
	p := newLasChunkPipeline(8, 3, func() (lasChunkDecoder, error) { return nil, boom })
	defer p.close()
	if _, err := p.next(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the decoder creation error", err)
	}
}

func TestLasChunkPipelineReset(t *testing.T) {
	p := newLasChunkPipeline(40, 4, fakeDecoderFactory(-1, nil))
	defer p.close()
	want := fakeSequence(40)
	// Reset before reading, in the middle of a chunk, and after the end.
	p.reset()
	for round, stopAfter := range []int{17, len(want), 0, 5} {
		for i := 0; i < stopAfter; i++ {
			pt, err := p.next()
			if err != nil {
				t.Fatalf("round %d: point %d: %v", round, i, err)
			}
			if pt.X != want[i] {
				t.Fatalf("round %d: point %d is %v, want %v", round, i, pt.X, want[i])
			}
		}
		p.reset()
	}
	got, err := drainPipeline(p)
	if err != io.EOF || !equalFloats(got, want) {
		t.Fatalf("after resets: %d points ending with %v, want all %d and io.EOF", len(got), err, len(want))
	}
}

// Workers must not decode the whole file ahead of a slow consumer: that would
// hold every point in memory.
func TestLasChunkPipelineBoundsReadAhead(t *testing.T) {
	const workers = 3
	var decoded atomic.Int64
	p := newLasChunkPipeline(200, workers, fakeDecoderFactory(-1, &decoded))
	defer p.close()
	if _, err := p.next(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the workers run as far as they are allowed
	if n := int(decoded.Load()); n > lasReadAheadChunks(workers) {
		t.Fatalf("%d chunks decoded while the consumer holds the first one, want at most %d", n, lasReadAheadChunks(workers))
	} else if n < 2 {
		t.Fatalf("only %d chunk decoded: no read-ahead at all", n)
	}
}

func TestLasChunkPipelineCloseStopsWorkers(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		p := newLasChunkPipeline(500, 8, fakeDecoderFactory(-1, nil))
		for j := 0; j < 7; j++ {
			if _, err := p.next(); err != nil {
				t.Fatal(err)
			}
		}
		p.close()
		p.close() // closing twice is harmless
		if _, err := p.next(); err == nil {
			t.Fatal("read after close returned a point")
		}
	}
	// close waits for the workers, so nothing should be left behind.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("%d goroutines before, %d after closing 20 pipelines", before, after)
	}
}

// ---------------------------------------------------------------------------
// How many decoders a file gets
// ---------------------------------------------------------------------------

func TestLasDecodeWorkers(t *testing.T) {
	chunks := func(n int, points uint32) []golaz.Chunk {
		out := make([]golaz.Chunk, n)
		for i := range out {
			out[i].NumPoints = points
		}
		return out
	}
	tests := []struct {
		name       string
		chunks     []golaz.Chunk
		maxWorkers int
		want       int
	}{
		{"usual 50k chunks", chunks(100, 50000), 8, 8},
		{"fewer chunks than workers", chunks(3, 50000), 8, 3},
		{"one chunk: nothing to parallelise", chunks(1, 50000), 8, 0},
		{"no chunks", nil, 8, 0},
		{"one worker allowed: sequential", chunks(100, 50000), 1, 0},
		{"big chunks: fewer workers keep the buffered points bounded", chunks(100, 250000), 8, 2},
		{"huge chunks: sequential", chunks(100, 400000), 8, 0},
		{"one huge chunk among small ones decides", append(chunks(99, 50000), chunks(1, 400000)...), 8, 0},
	}
	for _, tc := range tests {
		if got := lasDecodeWorkers(tc.chunks, tc.maxWorkers); got != tc.want {
			t.Errorf("%s: %d workers, want %d", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// GoLasReader: parallel reading gives the sequential stream
// ---------------------------------------------------------------------------

// writeTestLasFile writes n synthetic points in the given point format with
// golaz and returns the path. A ".laz" name is compressed with chunkSize
// points per chunk.
func writeTestLasFile(t *testing.T, name string, format uint8, n int, chunkSize uint32) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	hdr := golaz.WriterHeader{PointFormat: format, ScaleX: 0.01, ScaleY: 0.01, ScaleZ: 0.01, OffsetX: 500000, OffsetY: 4400000}
	var opts []golaz.WriterOption
	if filepath.Ext(name) == ".laz" {
		opts = append(opts, golaz.WithChunkSize(chunkSize))
	}
	w, err := golaz.Create(path, hdr, opts...)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	rng := rand.New(rand.NewSource(int64(format)*1000 + int64(n)))
	x, y, z := 500000.0, 4400000.0, 120.0
	for i := 0; i < n; i++ {
		x += rng.Float64() * 0.8
		y += rng.Float64()*0.6 - 0.2
		z += rng.Float64()*0.5 - 0.25
		p := golaz.NewPoint(format)
		w.SetCoordinates(p, x, y, z)
		p.Intensity = uint16(rng.Intn(4000))
		p.NumberOfReturns = uint8(1 + rng.Intn(3))
		p.ReturnNumber = uint8(1 + rng.Intn(int(p.NumberOfReturns)))
		p.Classification = uint8(rng.Intn(12))
		if i%17 == 0 {
			p.ClassificationFlags = 1 << 2 // withheld
		}
		p.UserData = uint8(rng.Intn(256))
		p.PointSourceID = uint16(10 + i/700)
		if p.HasGPSTime() {
			p.SetGPSTime(300000000 + float64(i)*0.0001)
		}
		if p.HasRGB() {
			p.SetRGB(uint16(rng.Intn(65536)), uint16(rng.Intn(65536)), uint16(rng.Intn(65536)))
		}
		if err := w.WritePoint(p); err != nil {
			t.Fatalf("write point %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close %s: %v", name, err)
	}
	return path
}

// readAllPoints drains r and fails the test on anything but a clean end.
func readAllPoints(t *testing.T, r *GoLasReader) []geom.Point64 {
	t.Helper()
	pts := make([]geom.Point64, 0, r.NumberOfPoints())
	for {
		pt, err := r.GetNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("point %d: %v", len(pts), err)
		}
		pts = append(pts, pt)
	}
	if _, err := r.GetNext(); err != io.EOF {
		t.Fatalf("read after the end: %v, want io.EOF", err)
	}
	return pts
}

func assertSamePoints(t *testing.T, got, want []geom.Point64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d points, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Vector != w.Vector || g.R != w.R || g.G != w.G || g.B != w.B || !bytes.Equal(g.Attributes, w.Attributes) {
			t.Fatalf("point %d: got %+v, want %+v", i, g, w)
		}
	}
}

func TestGoLasReaderParallelMatchesSequential(t *testing.T) {
	pf7Laz, _ := embeddedLaz14Pair(t, "las14_pf7_v3_multichannel_1000pts")
	pf8Laz, _ := embeddedLaz14Pair(t, "las14_pf8_v4_multichannel_1000pts")
	files := map[string]string{
		"LAS 1.2 pf3 LAZ, 250-point chunks":  writeTestLasFile(t, "pf3.laz", 3, 3100, 250),
		"LAS 1.4 pf7 LAZ, 250-point chunks":  writeTestLasFile(t, "pf7.laz", 7, 3100, 250),
		"LAS 1.2 pf2 uncompressed, 120k pts": writeTestLasFile(t, "pf2.las", 2, 120000, 0),
		"LASzip pf7 v3 multichannel":         pf7Laz,
		"LASzip pf8 v4 multichannel":         pf8Laz,
	}
	attrSets := map[string]model.Attributes{
		"no attributes": nil,
		"default":       model.NewAttributes(model.AttrIntensity, model.AttrClassification, model.AttrWithheld),
		"many": model.NewAttributes(model.AttrGpsTime, model.AttrScanAngle, model.AttrReturnNumber,
			model.AttrUserData, model.AttrPointSourceId, model.AttrOverlap, model.AttrNir, "GridID"),
	}
	for fileName, path := range files {
		for attrName, attrs := range attrSets {
			for _, eightBit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/8bit=%v", fileName, attrName, eightBit), func(t *testing.T) {
					seq, err := newGoLasReader(path, "EPSG:32633", eightBit, attrs, 1)
					if err != nil {
						t.Fatal(err)
					}
					defer seq.Close()
					par, err := newGoLasReader(path, "EPSG:32633", eightBit, attrs, 4)
					if err != nil {
						t.Fatal(err)
					}
					defer par.Close()
					if seq.chunked != nil {
						t.Fatal("a reader limited to one worker must read sequentially")
					}
					if par.chunked == nil {
						t.Fatal("the fixture has several chunks but is not read in parallel: the test would prove nothing")
					}
					want := readAllPoints(t, seq)
					if len(want) != seq.NumberOfPoints() || len(want) == 0 {
						t.Fatalf("sequential read returned %d points, header says %d", len(want), seq.NumberOfPoints())
					}
					assertSamePoints(t, readAllPoints(t, par), want)
				})
			}
		}
	}
}

func TestGoLasReaderParallelReset(t *testing.T) {
	path := writeTestLasFile(t, "pf3.laz", 3, 3100, 250)
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification)
	seq, err := newGoLasReader(path, "EPSG:32633", false, attrs, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer seq.Close()
	want := readAllPoints(t, seq)

	par, err := newGoLasReader(path, "EPSG:32633", false, attrs, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer par.Close()
	for i := 0; i < 1234; i++ { // stop inside a chunk
		if _, err := par.GetNext(); err != nil {
			t.Fatal(err)
		}
	}
	if err := par.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	assertSamePoints(t, readAllPoints(t, par), want)
	// and again after the end
	if err := par.Reset(); err != nil {
		t.Fatalf("Reset after the end: %v", err)
	}
	assertSamePoints(t, readAllPoints(t, par), want)
}

func TestGoLasReaderSingleChunkFileIsSequential(t *testing.T) {
	r, err := newGoLasReader(embeddedLasFile(t, "las-12-pf1.las"), "EPSG:32633", false, nil, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.chunked != nil {
		t.Fatal("a file with a single chunk gains nothing from parallel decoding")
	}
	if got := len(readAllPoints(t, r)); got != r.NumberOfPoints() {
		t.Fatalf("read %d points, header says %d", got, r.NumberOfPoints())
	}
}

// NewGoLasReader, the constructor the tiler uses, reads in parallel when the
// machine has the cores for it.
func TestNewGoLasReaderUsesParallelDecoding(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least two CPUs")
	}
	r, err := NewGoLasReader(writeTestLasFile(t, "pf3.laz", 3, 3100, 250), "EPSG:32633", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.chunked == nil {
		t.Fatal("NewGoLasReader reads a multi-chunk LAZ file sequentially")
	}
}

// Closing a reader in the middle of a file must return, with its workers gone.
func TestGoLasReaderParallelCloseEarly(t *testing.T) {
	path := writeTestLasFile(t, "pf3.laz", 3, 3100, 250)
	before := runtime.NumGoroutine()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			r, err := newGoLasReader(path, "EPSG:32633", false, nil, 4)
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 10; j++ {
				if _, err := r.GetNext(); err != nil {
					t.Error(err)
				}
			}
			r.Close()
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("%d goroutines before, %d after", before, after)
	}
}

// lasReadFails reports whether draining the reader ends in an error.
func lasReadFails(r *GoLasReader) bool {
	for {
		if _, err := r.GetNext(); err == io.EOF {
			return false
		} else if err != nil {
			return true
		}
	}
}

// A damaged chunk must not go unnoticed just because the file is read in
// parallel: whenever sequential reading fails on a file, parallel reading
// fails too. Sequential reading spots part of the damage by a chunk not
// ending where the next one starts; chunk decoders make the same check since
// golaz v0.3.1, and with v0.3.0 this test fails.
//
// The file is point format 2 on purpose. Without GPS time in the stream a
// damaged chunk usually decodes all its points and only its end gives it
// away (about a third of single-byte corruptions here); with GPS time the
// decoder nearly always fails outright, in either mode.
func TestGoLasReaderParallelDetectsDamagedChunks(t *testing.T) {
	path := writeTestLasFile(t, "pf2.laz", 2, 1000, 100)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Damage chunk bytes only, so the chunk table stays usable: they run
	// from just after the 8-byte chunk table pointer, which opens the point
	// data, to the chunk table itself.
	dataStart := int(binary.LittleEndian.Uint32(orig[96:100]))
	first := dataStart + 8
	end := int(binary.LittleEndian.Uint64(orig[dataStart:]))
	if end <= first || end > len(orig) {
		t.Fatalf("unexpected chunk table position %d", end)
	}

	rng := rand.New(rand.NewSource(3))
	damaged := filepath.Join(t.TempDir(), "damaged.laz")
	data := make([]byte, len(orig))
	const trials = 150
	var seqFails, parFails int
	for i := 0; i < trials; i++ {
		copy(data, orig)
		pos := first + rng.Intn(end-first)
		data[pos] ^= byte(1 + rng.Intn(255))
		if err := os.WriteFile(damaged, data, 0644); err != nil {
			t.Fatal(err)
		}
		seq, err := newGoLasReader(damaged, "EPSG:32633", false, nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		par, err := newGoLasReader(damaged, "EPSG:32633", false, nil, 4)
		if err != nil {
			t.Fatal(err)
		}
		if par.chunked == nil {
			t.Fatal("the damaged file is not read in parallel: the test would prove nothing")
		}
		seqFailed, parFailed := lasReadFails(seq), lasReadFails(par)
		seq.Close()
		par.Close()
		if seqFailed {
			seqFails++
		}
		if parFailed {
			parFails++
		}
		if seqFailed && !parFailed {
			t.Errorf("byte %d damaged: sequential reading fails, parallel reading reports nothing", pos)
		}
	}
	t.Logf("%d single-byte corruptions: sequential reading fails on %d, parallel reading on %d", trials, seqFails, parFails)
	if seqFails < trials/4 {
		t.Errorf("sequential reading noticed only %d of %d corruptions: the test proves little", seqFails, trials)
	}
}
