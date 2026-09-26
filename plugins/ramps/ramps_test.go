package ramps

import (
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

func TestAllRampsAreRegistered(t *testing.T) {
	names := Names()
	if len(names) != len(continuous)+len(categorical)+len(derived) {
		t.Fatalf("Names() returned %d aliases, expected %d", len(names), len(continuous)+len(categorical)+len(derived))
	}
	for _, alias := range names {
		g, ok := mutator.RegisteredColorGradient(alias)
		if !ok {
			t.Fatalf("expected gradient %q to be registered", alias)
		}
		if len(g.Nums) < 2 || len(g.Nums) != len(g.Colors) {
			t.Fatalf("gradient %q has inconsistent stops: %d nums, %d colors", alias, len(g.Nums), len(g.Colors))
		}
	}
}

func TestCoreBuiltinsAreNotShadowed(t *testing.T) {
	// core registers its own viridis etc.; this package must not collide
	for _, alias := range Names() {
		for _, core := range []string{"viridis", "magma", "inferno", "plasma", "cividis", "turbo", "grayscale", "heat", "las-classification"} {
			if alias == core {
				t.Fatalf("alias %q collides with a core built-in gradient", alias)
			}
		}
	}
}

func TestContinuousRampsMatchAuthoritativeEndpoints(t *testing.T) {
	// spot-check first/last colors against the canonical published values
	cases := []struct {
		alias       string
		stops       int
		first, last mutator.Color
	}{
		{"terrain", 256, mutator.Color{R: 51, G: 51, B: 153}, mutator.Color{R: 255, G: 255, B: 255}},
		{"gist-earth", 256, mutator.Color{R: 0, G: 0, B: 0}, mutator.Color{R: 253, G: 251, B: 251}},
		{"coolwarm", 256, mutator.Color{R: 59, G: 76, B: 192}, mutator.Color{R: 180, G: 4, B: 38}},
		{"seismic", 256, mutator.Color{R: 0, G: 0, B: 76}, mutator.Color{R: 128, G: 0, B: 0}},
		{"cubehelix", 256, mutator.Color{R: 0, G: 0, B: 0}, mutator.Color{R: 255, G: 255, B: 255}},
		{"mako", 256, mutator.Color{R: 11, G: 4, B: 5}, mutator.Color{R: 222, G: 245, B: 229}},
		{"rocket", 256, mutator.Color{R: 3, G: 5, B: 26}, mutator.Color{R: 250, G: 235, B: 221}},
		{"haline", 256, mutator.Color{R: 42, G: 24, B: 108}, mutator.Color{R: 253, G: 239, B: 154}},
		{"amp", 256, mutator.Color{R: 241, G: 237, B: 236}, mutator.Color{R: 60, G: 9, B: 18}},
		{"balance", 256, mutator.Color{R: 24, G: 28, B: 67}, mutator.Color{R: 60, G: 9, B: 18}},
		{"topo", 256, mutator.Color{R: 40, G: 26, B: 44}, mutator.Color{R: 249, G: 253, B: 228}},
		{"batlow", 256, mutator.Color{R: 1, G: 25, B: 89}, mutator.Color{R: 250, G: 204, B: 250}},
		{"roma", 256, mutator.Color{R: 126, G: 23, B: 0}, mutator.Color{R: 3, G: 49, B: 152}},
		{"berlin", 256, mutator.Color{R: 158, G: 176, B: 255}, mutator.Color{R: 255, G: 173, B: 173}},
		{"nuuk", 256, mutator.Color{R: 5, G: 89, B: 140}, mutator.Color{R: 254, G: 254, B: 178}},
		{"oleron", 256, mutator.Color{R: 26, G: 38, B: 89}, mutator.Color{R: 253, G: 253, B: 230}},
		{"ylgnbu", 9, mutator.Color{R: 255, G: 255, B: 217}, mutator.Color{R: 8, G: 29, B: 88}},
		{"blues", 9, mutator.Color{R: 247, G: 251, B: 255}, mutator.Color{R: 8, G: 48, B: 107}},
		{"rdbu", 11, mutator.Color{R: 103, G: 0, B: 31}, mutator.Color{R: 5, G: 48, B: 97}},
		{"brbg", 11, mutator.Color{R: 84, G: 48, B: 5}, mutator.Color{R: 0, G: 60, B: 48}},
		{"spectral", 11, mutator.Color{R: 158, G: 1, B: 66}, mutator.Color{R: 94, G: 79, B: 162}},
		{"piyg", 11, mutator.Color{R: 142, G: 1, B: 82}, mutator.Color{R: 39, G: 100, B: 25}},
	}
	if len(cases) != len(continuous) {
		t.Fatalf("expected %d continuous ramps to be covered, got %d", len(continuous), len(cases))
	}
	for _, tc := range cases {
		g, ok := mutator.RegisteredColorGradient(tc.alias)
		if !ok {
			t.Fatalf("%s: not registered", tc.alias)
		}
		if len(g.Colors) != tc.stops {
			t.Fatalf("%s: expected %d stops, got %d", tc.alias, tc.stops, len(g.Colors))
		}
		if g.InterpolationMode != mutator.GradientInterpolationLinear {
			t.Fatalf("%s: expected linear interpolation", tc.alias)
		}
		if g.Colors[0] != tc.first || g.Colors[len(g.Colors)-1] != tc.last {
			t.Fatalf("%s: endpoints %v ... %v do not match authoritative values %v ... %v",
				tc.alias, g.Colors[0], g.Colors[len(g.Colors)-1], tc.first, tc.last)
		}
		if g.Nums[0] != 0 || g.Nums[len(g.Nums)-1] != 1 {
			t.Fatalf("%s: stops must span [0, 1]", tc.alias)
		}
	}
}

func TestCategoricalRampsAreFlatBanded(t *testing.T) {
	cases := []struct {
		alias       string
		classes     int
		first, last mutator.Color
	}{
		{"dark2", 8, mutator.Color{R: 27, G: 158, B: 119}, mutator.Color{R: 102, G: 102, B: 102}},
		{"paired", 12, mutator.Color{R: 166, G: 206, B: 227}, mutator.Color{R: 177, G: 89, B: 40}},
		{"set2", 8, mutator.Color{R: 102, G: 194, B: 165}, mutator.Color{R: 179, G: 179, B: 179}},
		{"accent", 8, mutator.Color{R: 127, G: 201, B: 127}, mutator.Color{R: 102, G: 102, B: 102}},
	}
	if len(cases) != len(categorical) {
		t.Fatalf("expected %d categorical ramps to be covered, got %d", len(categorical), len(cases))
	}
	for _, tc := range cases {
		g, ok := mutator.RegisteredColorGradient(tc.alias)
		if !ok {
			t.Fatalf("%s: not registered", tc.alias)
		}
		if g.InterpolationMode != mutator.GradientInterpolationFlat {
			t.Fatalf("%s: expected flat interpolation for a categorical palette", tc.alias)
		}
		// one band per class plus the required final stop repeating the last color
		if len(g.Colors) != tc.classes+1 {
			t.Fatalf("%s: expected %d stops, got %d", tc.alias, tc.classes+1, len(g.Colors))
		}
		if g.Colors[0] != tc.first || g.Colors[tc.classes-1] != tc.last || g.Colors[tc.classes] != tc.last {
			t.Fatalf("%s: band colors do not match authoritative values", tc.alias)
		}
	}
}

func TestViridisPastelIsDerivedFromViridis(t *testing.T) {
	pastel, ok := mutator.RegisteredColorGradient("viridis-pastel")
	if !ok {
		t.Fatal("viridis-pastel is not registered")
	}
	viridis, ok := mutator.RegisteredColorGradient("viridis")
	if !ok {
		t.Fatal("core viridis is not registered")
	}
	if len(pastel.Colors) != len(viridis.Colors) {
		t.Fatalf("expected %d stops, got %d", len(viridis.Colors), len(pastel.Colors))
	}
	if pastel.InterpolationMode != mutator.GradientInterpolationLinear {
		t.Fatal("expected linear interpolation")
	}
	// the sRGB encode brightens: every channel must be >= the viridis one
	for i := range pastel.Colors {
		p, v := pastel.Colors[i], viridis.Colors[i]
		if p.R < v.R || p.G < v.G || p.B < v.B {
			t.Fatalf("stop %d: pastel %v is darker than viridis %v", i, p, v)
		}
	}
	// spot-check the endpoints against the c^(1/2.2) encode
	if first := (mutator.Color{R: 140, G: 21, B: 154}); pastel.Colors[0] != first {
		t.Fatalf("first stop %v, expected %v", pastel.Colors[0], first)
	}
	if last := (mutator.Color{R: 254, G: 244, B: 106}); pastel.Colors[len(pastel.Colors)-1] != last {
		t.Fatalf("last stop %v, expected %v", pastel.Colors[len(pastel.Colors)-1], last)
	}
}

func TestRampsWorkWithTheColorizer(t *testing.T) {
	for _, alias := range Names() {
		if _, err := mutator.NewColorizer("intensity", alias); err != nil {
			t.Fatalf("NewColorizer with %q: %v", alias, err)
		}
	}
}
