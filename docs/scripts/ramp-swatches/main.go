// Command ramp-swatches writes the CSS gradients that the documentation site
// shows as color ramp previews, sampled from gotiler's own ramp tables so
// they match the colors the tiler writes. Run it from the repository root
// after changing a ramp:
//
//	go run ./docs/scripts/ramp-swatches > docs/src/data/ramp-swatches.json
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/plugins/ramps"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

// builtInRamps are the gradients tiler/mutator registers itself; the
// plugins/ramps package lists its own.
var builtInRamps = []string{"grayscale", "heat", "las-classification", "viridis", "magma", "inferno", "plasma", "cividis", "turbo"}

// linearSamples is how many stops sample a linear gradient: enough for a
// smooth preview, few enough to keep the CSS short.
const linearSamples = 16

func main() {
	out, err := swatchesJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(out)
}

// swatchesJSON maps every ramp name to its CSS gradient, as indented JSON.
func swatchesJSON() ([]byte, error) {
	names := append(append([]string{}, builtInRamps...), ramps.Names()...)
	swatches := make(map[string]string, len(names))
	for _, name := range names {
		gradient, ok := mutator.RegisteredColorGradient(name)
		if !ok {
			return nil, fmt.Errorf("color ramp %q is not registered", name)
		}
		swatches[name] = cssGradient(gradient)
	}
	out, err := json.MarshalIndent(swatches, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// cssGradient renders a gradient as a left-to-right CSS linear-gradient. Flat
// gradients get one equal-width band per interval, so palettes on a fixed
// scale, like las-classification's class codes, show every class; linear
// ones get evenly spaced samples.
func cssGradient(g mutator.ColorGradientScale) string {
	var stops []string
	if g.InterpolationMode == mutator.GradientInterpolationFlat {
		bands := len(g.Nums) - 1
		for i := 0; i < bands; i++ {
			stops = append(stops, fmt.Sprintf("%s %s %s", hex(g.Colors[i]), percent(i, bands), percent(i+1, bands)))
		}
	} else {
		for i := 0; i < linearSamples; i++ {
			stops = append(stops, hex(colorAt(g, float64(i)/(linearSamples-1))))
		}
	}
	return "linear-gradient(90deg, " + strings.Join(stops, ", ") + ")"
}

// colorAt interpolates a linear gradient at t, in [0, 1].
func colorAt(g mutator.ColorGradientScale, t float64) mutator.Color {
	j := sort.SearchFloat64s(g.Nums, t)
	if j == 0 {
		return g.Colors[0]
	}
	if j >= len(g.Nums) {
		return g.Colors[len(g.Colors)-1]
	}
	f := (t - g.Nums[j-1]) / (g.Nums[j] - g.Nums[j-1])
	lerp := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*f)) }
	from, to := g.Colors[j-1], g.Colors[j]
	return mutator.Color{R: lerp(from.R, to.R), G: lerp(from.G, to.G), B: lerp(from.B, to.B)}
}

func hex(c mutator.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func percent(i, n int) string {
	return fmt.Sprintf("%.2f%%", float64(i)*100/float64(n))
}
