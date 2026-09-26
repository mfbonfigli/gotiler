package writer

import (
	"embed"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

//go:embed testdata
var testdataContent embed.FS

// update is a comma-separated list of golden file basenames to regenerate, or "all".
// Usage: go test -run TestConsume ./internal/writer/ -update all
var update = flag.String("update", "", "comma-separated list of golden files to update, or 'all'")

// shouldUpdate reports whether the named golden file should be regenerated.
func shouldUpdate(name string) bool {
	if *update == "" {
		return false
	}
	if *update == "all" {
		return true
	}
	for _, n := range strings.Split(*update, ",") {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	return false
}

func TestConsume(t *testing.T) {
	c := NewStandardConsumer()
	wc := make(chan *WorkUnit)
	ec := make(chan error, 1)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go c.Consume(wc, ec, wg)

	attrs := model.DefaultAttributes()
	pts := withDefaultPointAttributes(attrs, 7, 3, []model.Point{
		{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203},
		{X: 1, Y: 3, Z: 4, R: 186, G: 200, B: 237},
		{X: 2, Y: 6, Z: 8, R: 156, G: 167, B: 204},
	})

	pt1 := &geom.LinkedPoint{
		Pt: pts[0],
	}
	pt2 := &geom.LinkedPoint{
		Pt: pts[1],
	}
	pt3 := &geom.LinkedPoint{
		Pt: pts[2],
	}
	pt1.Next = pt2
	pt2.Next = pt3

	stream := geom.NewLinkedPointStream(pt1, 3)
	tr := geom.LocalToGlobalTransformFromPoint(1000, 1000, 1000)
	n := &testtree.MockNode{
		TotalNumPts: 3,
		Pts:         stream,
		Bounds: geom.NewBoundingBox(
			0,
			4,
			0,
			6,
			0,
			8,
		),
		Root:      true,
		Leaf:      true,
		GeomError: 20,
		Transform: &tr,
		Summaries: makeStandardSummaries(attrs),
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "tst")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	tmpPath := filepath.Join(tmp, "tst")
	os.Mkdir(tmpPath, 0755)
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})

	wc <- &WorkUnit{
		Node:           n,
		WriterProvider: NewDiskWriterProvider(tmpPath),
	}
	close(wc)
	wg.Wait()
	select {
	case err := <-ec:
		t.Fatalf("consumer error: %v", err)
	default:
	}

	actualPnts, err := os.ReadFile(filepath.Join(tmpPath, "d.pnts"))
	if err != nil {
		t.Fatalf("unable to read d.pnts: %v", err)
	}
	if shouldUpdate("content.pnts") {
		if err := os.WriteFile("./testdata/content.pnts", actualPnts, 0644); err != nil {
			t.Fatalf("unable to update golden file: %v", err)
		}
		t.Log("updated testdata/content.pnts")
		return
	}
	expectedPnts, err := testdataContent.ReadFile("testdata/content.pnts")
	if err != nil {
		t.Fatalf("unable to read embedded testdata/content.pnts: %v", err)
	}
	if !reflect.DeepEqual(actualPnts, expectedPnts) {
		t.Errorf("expected pnts:\n%v\n\ngot:\n\n%v\n", expectedPnts, actualPnts)
	}
}

func TestConsumeGltf(t *testing.T) {
	attrs := model.DefaultAttributes()
	c := NewStandardConsumer(WithGeometryEncoder(NewGltfEncoder("d.glb", attrs)))
	wc := make(chan *WorkUnit)
	ec := make(chan error, 1)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go c.Consume(wc, ec, wg)

	pts := withDefaultPointAttributes(attrs, 7, 3, []model.Point{
		{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203},
		{X: 1, Y: 1, Z: 1, R: 186, G: 200, B: 237},
		{X: 2, Y: 2, Z: 2, R: 156, G: 167, B: 204},
	})

	pt1 := &geom.LinkedPoint{
		Pt: pts[0],
	}
	pt2 := &geom.LinkedPoint{
		Pt: pts[1],
	}
	pt3 := &geom.LinkedPoint{
		Pt: pts[2],
	}
	pt1.Next = pt2
	pt2.Next = pt3

	tr := geom.LocalToGlobalTransformFromPoint(2000, 1000, 1000)
	stream := geom.NewLinkedPointStream(pt1, 3)
	n := &testtree.MockNode{
		TotalNumPts: 3,
		Pts:         stream,
		Bounds: geom.NewBoundingBox(
			0,
			4,
			0,
			6,
			0,
			8,
		),
		Root:      true,
		Leaf:      true,
		GeomError: 20,
		Transform: &tr,
		Summaries: makeStandardSummaries(attrs),
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "tst")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	tmpPath := filepath.Join(tmp, "tst")
	os.Mkdir(tmpPath, 0755)
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})

	wc <- &WorkUnit{
		Node:           n,
		WriterProvider: NewDiskWriterProvider(tmpPath),
	}
	close(wc)
	wg.Wait()
	select {
	case err := <-ec:
		t.Fatalf("consumer error: %v", err)
	default:
	}
	actualGlb, err := os.ReadFile(filepath.Join(tmpPath, "d.glb"))
	if err != nil {
		t.Fatalf("unable to read d.glb: %v", err)
	}
	if shouldUpdate("content.glb") {
		if err := os.WriteFile("./testdata/content.glb", actualGlb, 0644); err != nil {
			t.Fatalf("unable to update golden file: %v", err)
		}
		t.Log("updated testdata/content.glb")
		return
	}
	expectedGlb, err := testdataContent.ReadFile("testdata/content.glb")
	if err != nil {
		t.Fatalf("unable to read embedded testdata/content.glb: %v", err)
	}
	if !reflect.DeepEqual(actualGlb, expectedGlb) {
		t.Errorf("expected glb:\n%v\n\ngot:\n\n%v\n", expectedGlb, actualGlb)
	}
}

func withDefaultPointAttributes(attrs model.Attributes, intensity uint16, classification uint8, points []model.Point) []model.Point {
	summaries := makeStandardSummaries(attrs)
	out := make([]model.Point, len(points))
	for i, pt := range points {
		out[i] = pt
		values := packSummaryValues(summaries, map[string]any{
			model.AttrIntensity:       intensity,
			model.AttrClassification:  classification,
			model.AttrReturnNumber:    uint8(0),
			model.AttrNumberOfReturns: uint8(0),
		})
		if len(values) > 0 {
			out[i].Attributes = values
		}
	}
	return out
}
