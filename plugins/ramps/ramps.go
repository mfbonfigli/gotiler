// Package ramps registers an extended library of color gradients with the
// tiler/mutator colorizer registry. Import it for its side effects:
//
//	import _ "github.com/mfbonfigli/gotiler/v3/plugins/ramps"
//
// The registered aliases become available to mutator.NewColorizer alongside
// the core built-ins. The library covers:
//
//   - terrain & elevation: terrain, gist-earth, batlow, oleron, nuuk, topo
//   - sequential intensity/density: cubehelix, mako, rocket, haline, amp,
//     ylgnbu, blues
//   - diverging change detection: rdbu, brbg, spectral, piyg, coolwarm,
//     seismic, balance, roma, berlin
//   - categorical classification: dark2, paired, set2, accent
//   - derived variants: viridis-pastel (viridis softened by one sRGB encode)
//
// Continuous ramps interpolate linearly between their stops. Categorical
// palettes are registered as flat gradients with one equal-width band per
// color: combine them with a fixed value range (or the "stretch=minmax"
// colorize modifier) so category codes land on stable bands.
//
// All palettes derive from freely licensed sources (matplotlib, seaborn,
// cmocean, the Scientific Colour Maps by Fabio Crameri, and ColorBrewer);
// see THIRD-PARTY-LICENSES.md for details and attributions.
package ramps

import (
	"fmt"
	"math"
	"sort"

	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

// continuous maps aliases to the color tables of linearly interpolated ramps.
var continuous = map[string][]mutator.Color{
	"terrain":    terrainColors,
	"gist-earth": gistEarthColors,
	"coolwarm":   coolwarmColors,
	"seismic":    seismicColors,
	"cubehelix":  cubehelixColors,
	"mako":       makoColors,
	"rocket":     rocketColors,
	"haline":     halineColors,
	"amp":        ampColors,
	"balance":    balanceColors,
	"topo":       topoColors,
	"batlow":     batlowColors,
	"roma":       romaColors,
	"berlin":     berlinColors,
	"nuuk":       nuukColors,
	"oleron":     oleronColors,
	"ylgnbu":     ylGnBuColors,
	"blues":      bluesColors,
	"rdbu":       rdBuColors,
	"brbg":       brBGColors,
	"spectral":   spectralColors,
	"piyg":       piYGColors,
}

// categorical maps aliases to the color tables of flat, banded palettes.
var categorical = map[string][]mutator.Color{
	"dark2":  dark2Colors,
	"paired": pairedColors,
	"set2":   set2Colors,
	"accent": accentColors,
}

// derived maps aliases to gradients computed from other registered gradients
// (see init); they participate in Names() like any authored ramp.
var derived = map[string]func() (mutator.ColorGradientScale, bool){
	// viridis brightened by one sRGB encode (c' = c^(1/2.2)): a soft, pastel
	// take on viridis that keeps its perceptual ordering.
	"viridis-pastel": func() (mutator.ColorGradientScale, bool) {
		gradient, ok := mutator.RegisteredColorGradient("viridis")
		if !ok {
			return mutator.ColorGradientScale{}, false
		}
		return pastelGradient(gradient), true
	},
}

func init() {
	for alias, colors := range continuous {
		mustRegister(alias, linearGradient(colors))
	}
	for alias, colors := range categorical {
		mustRegister(alias, categoricalGradient(colors))
	}
	for alias, build := range derived {
		if gradient, ok := build(); ok {
			mustRegister(alias, gradient)
		}
	}
}

// pastelGradient lightens a gradient by applying an sRGB-style encode
// (c' = c^(1/2.2)) to every channel, preserving stops and interpolation.
func pastelGradient(g mutator.ColorGradientScale) mutator.ColorGradientScale {
	colors := make([]mutator.Color, len(g.Colors))
	encode := func(c uint8) uint8 {
		v := math.Pow(float64(c)/255, 1/2.2) * 255
		if v >= 255 {
			return 255
		}
		return uint8(math.Round(v))
	}
	for i, c := range g.Colors {
		colors[i] = mutator.Color{R: encode(c.R), G: encode(c.G), B: encode(c.B)}
	}
	out := g
	out.Colors = colors
	return out
}

func mustRegister(alias string, gradient mutator.ColorGradientScale) {
	if err := mutator.RegisterColorGradient(alias, gradient); err != nil {
		panic(fmt.Sprintf("ramps: registering %q: %v", alias, err))
	}
}

// Names returns the gradient aliases registered by this package, sorted.
func Names() []string {
	out := make([]string, 0, len(continuous)+len(categorical)+len(derived))
	for alias := range continuous {
		out = append(out, alias)
	}
	for alias := range categorical {
		out = append(out, alias)
	}
	for alias := range derived {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// linearGradient builds a linearly interpolated gradient with uniformly
// spaced stops from a color table.
func linearGradient(colors []mutator.Color) mutator.ColorGradientScale {
	nums := make([]float64, len(colors))
	last := float64(len(colors) - 1)
	for i := range nums {
		nums[i] = float64(i) / last
	}
	return mutator.ColorGradientScale{
		Nums:              nums,
		Colors:            colors,
		InterpolationMode: mutator.GradientInterpolationLinear,
	}
}

// categoricalGradient builds a flat gradient with one equal-width band per
// color, so each category occupies a stable slice of the value range.
func categoricalGradient(colors []mutator.Color) mutator.ColorGradientScale {
	n := len(colors)
	nums := make([]float64, n+1)
	banded := make([]mutator.Color, n+1)
	for k := 0; k < n; k++ {
		nums[k] = float64(k) / float64(n)
		banded[k] = colors[k]
	}
	// flat interpolation requires a final stop at 1; it repeats the last band
	nums[n] = 1
	banded[n] = colors[n-1]
	return mutator.ColorGradientScale{
		Nums:              nums,
		Colors:            banded,
		InterpolationMode: mutator.GradientInterpolationFlat,
	}
}
