package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

const committedSwatches = "../../src/data/ramp-swatches.json"

func TestCommittedSwatchesMatchTheRamps(t *testing.T) {
	want, err := swatchesJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(committedSwatches)
	if err != nil {
		t.Fatal(err)
	}
	// the checkout may have turned the line endings into CRLF
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date: regenerate it with\n\tgo run ./docs/scripts/ramp-swatches > docs/src/data/ramp-swatches.json", committedSwatches)
	}
}

func TestSwatchesCoverEveryDocumentedRamp(t *testing.T) {
	out, err := swatchesJSON()
	if err != nil {
		t.Fatal(err)
	}
	var swatches map[string]string
	if err := json.Unmarshal(out, &swatches); err != nil {
		t.Fatal(err)
	}
	if len(swatches) != 36 {
		t.Errorf("got %d swatches, want the 36 documented ramps", len(swatches))
	}
	for _, name := range []string{"viridis", "las-classification", "viridis-pastel", "paired", "rdbu"} {
		if swatches[name] == "" {
			t.Errorf("no swatch for %q", name)
		}
	}
}

func TestLinearGradientsAreSampledEvenly(t *testing.T) {
	g := mutator.ColorGradientScale{
		Nums:              []float64{0, 1},
		Colors:            []mutator.Color{{R: 0, G: 0, B: 0}, {R: 255, G: 255, B: 255}},
		InterpolationMode: mutator.GradientInterpolationLinear,
	}
	css := cssGradient(g)
	if !bytes.HasPrefix([]byte(css), []byte("linear-gradient(90deg, #000000, #111111, ")) {
		t.Errorf("css = %s, want evenly spaced samples starting from black", css)
	}
	if !bytes.HasSuffix([]byte(css), []byte(", #eeeeee, #ffffff)")) {
		t.Errorf("css = %s, want evenly spaced samples ending at white", css)
	}
}

func TestFlatGradientsGetEqualBands(t *testing.T) {
	// a fixed-scale palette whose last interval covers most of the range,
	// like las-classification: every interval still gets an equal band
	g := mutator.ColorGradientScale{
		Nums:              []float64{0, 0.01, 1},
		Colors:            []mutator.Color{{R: 255}, {B: 255}, {B: 255}},
		InterpolationMode: mutator.GradientInterpolationFlat,
	}
	want := "linear-gradient(90deg, #ff0000 0.00% 50.00%, #0000ff 50.00% 100.00%)"
	if got := cssGradient(g); got != want {
		t.Errorf("css = %s, want %s", got, want)
	}
}
