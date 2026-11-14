package spz

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// The largest rounding errors of the stored values, slightly widened for
// floating-point noise.
const (
	slack               = 1e-9
	positionError       = 0.5/4096 + slack
	opacityError        = 0.5/255 + slack
	colorError          = 0.5/(colorScale*255) + slack
	logScaleError       = 0.5/scaleLogFactor + slack
	firstThreeError     = 0.5/127.5 + slack
	smallestThreeDegree = 0.25
	shDegree1Error      = (shDegree1Step/2+0.5)/128 + slack
	shHigherError       = (shHigherStep/2+0.5)/128 + slack
)

// encode returns the payload Encode writes.
func encode(t *testing.T, splats []splat.Splat, version Version) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Encode(&out, splats, version); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestEncodeRoundTrip(t *testing.T) {
	for _, version := range []Version{Version2, Version3} {
		for degree, shCount := range []int{0, 3, 8, 15} {
			t.Run(fmt.Sprintf("v%d degree %d", version, degree), func(t *testing.T) {
				splats := randomSplats(rand.New(rand.NewPCG(uint64(version), uint64(degree))), 300, shCount)
				d := decode(t, encode(t, splats, version))
				if d.version != version || d.shDegree != degree || d.fractionalBits != 12 || d.flags != 0 {
					t.Errorf("header: version %d, degree %d, fractional bits %d, flags %d; want %d, %d, 12, 0", d.version, d.shDegree, d.fractionalBits, d.flags, version, degree)
				}
				if len(d.splats) != len(splats) {
					t.Fatalf("decoded %d splats, want %d", len(d.splats), len(splats))
				}
				for i, want := range splats {
					got := d.splats[i]
					name := fmt.Sprintf("splat %d", i)
					assertWithin(t, name+" center", got.Center, want.Center, positionError)
					if math.Abs(got.Opacity-want.Opacity) > opacityError {
						t.Errorf("%s opacity = %v, want %v", name, got.Opacity, want.Opacity)
					}
					assertWithin(t, name+" SH0", got.SH0, want.SH0, colorError)
					assertWithin(t, name+" log scale", logOf(got.Scale), logOf(want.Scale), logScaleError)
					assertRotation(t, name, version, got.Rotation, want.Rotation)
					for j := range want.SHRest {
						step := shHigherError
						if j < 3 {
							step = shDegree1Error
						}
						assertWithin(t, fmt.Sprintf("%s SH coefficient %d", name, j), got.SHRest[j], want.SHRest[j], step)
					}
				}
			})
		}
	}
}

func logOf(v geom.Vec3) geom.Vec3 {
	return geom.Vec3{X: math.Log(v.X), Y: math.Log(v.Y), Z: math.Log(v.Z)}
}

// assertRotation checks a decoded rotation. Version 2 stores x, y and z and
// rebuilds w, whose error grows without bound as w nears zero, so only the
// stored components are bounded; version 3 bounds the whole rotation.
func assertRotation(t *testing.T, name string, version Version, got, want geom.Quat) {
	t.Helper()
	if version == Version3 {
		if angle := rotationAngle(got, want); angle > smallestThreeDegree {
			t.Errorf("%s rotation is off by %.3f degrees, want at most %v", name, angle, smallestThreeDegree)
		}
		return
	}
	want = want.Normalize()
	if want.W < 0 {
		want = geom.Quat{X: -want.X, Y: -want.Y, Z: -want.Z, W: -want.W}
	}
	assertWithin(t, name+" rotation x, y, z", geom.Vec3{X: got.X, Y: got.Y, Z: got.Z}, geom.Vec3{X: want.X, Y: want.Y, Z: want.Z}, firstThreeError)
}

// layoutSplat is a splat whose uncompressed payload, layoutBytes, is worked
// out by hand from the reference encoder's formulas.
var layoutSplat = splat.Splat{
	Center:   geom.Vec3{X: 1, Y: -2, Z: 0.5},
	Rotation: geom.Quat{Z: 1, W: 1}, // 90 degrees about Z, not normalized
	Scale:    geom.Vec3{X: 1, Y: math.E, Z: 1 / math.E},
	Opacity:  0.5,
	SH0:      geom.Vec3{X: 0, Y: 1, Z: -1},
	SHRest: []geom.Vec3{
		{X: 0.5, Y: -0.5, Z: 0},
		{X: 1, Y: -1, Z: 0.1},
		{X: 0.03, Y: -0.03, Z: 2},
	},
}

func layoutBytes(version Version) []byte {
	header := []byte{
		'N', 'G', 'S', 'P',
		byte(version), 0, 0, 0,
		1, 0, 0, 0, // splat count
		1,  // SH degree
		12, // fractional bits
		0,  // flags
		0,  // reserved
	}
	beforeRotation := []byte{
		0x00, 0x10, 0x00, 0x00, 0xe0, 0xff, 0x00, 0x08, 0x00, // 4096, -8192 and 2048 in 24 bits
		128,          // opacity
		128, 166, 89, // colors
		160, 176, 144, // log scales 0, 1 and -1
	}
	rotation := map[Version][]byte{
		Version2: {128, 128, 218},
		Version3: {0xff, 0x01, 0x00, 0x80}, // largest component Z, then x = 0, y = 0, w = 511
	}[version]
	sh := []byte{
		192, 64, 128,
		255, 0, 144,
		136, 128, 255,
	}
	return bytes.Join([][]byte{header, beforeRotation, rotation, sh}, nil)
}

func TestEncodeWritesTheSPZLayout(t *testing.T) {
	for _, version := range []Version{Version2, Version3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			got := decompress(t, encode(t, []splat.Splat{layoutSplat}, version))
			if want := layoutBytes(version); !bytes.Equal(got, want) {
				t.Errorf("payload =\n%v\nwant\n%v", got, want)
			}
		})
	}
}

func TestEncodeOverwritesReusedBuffers(t *testing.T) {
	// Encoding a larger payload first leaves its bytes in the pooled buffer
	// that the next payload is written into.
	larger := randomSplats(rand.New(rand.NewPCG(3, 4)), 500, 15)
	for range 3 {
		for _, version := range []Version{Version2, Version3} {
			encode(t, larger, Version3)
			got := decompress(t, encode(t, []splat.Splat{layoutSplat}, version))
			if want := layoutBytes(version); !bytes.Equal(got, want) {
				t.Fatalf("v%d payload after a larger one =\n%v\nwant\n%v", version, got, want)
			}
		}
	}
}

func TestEncodeQuantizesHigherSHDegreesMoreCoarsely(t *testing.T) {
	s := splat.Splat{Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 1, Y: 1, Z: 1}}
	for range 8 {
		s.SHRest = append(s.SHRest, geom.Vec3{X: 0.2, Y: 0.2, Z: 0.2})
	}
	raw := decompress(t, encode(t, []splat.Splat{s}, Version2))
	sh := raw[len(raw)-24:]
	// 0.2 · 128 + 128 rounds to 154: 152 on the degree-1 step of 8, 160 on the
	// higher-degree step of 16.
	want := append(bytes.Repeat([]byte{152}, 9), bytes.Repeat([]byte{160}, 15)...)
	if !bytes.Equal(sh, want) {
		t.Errorf("SH bytes = %v, want %v", sh, want)
	}
}

func TestEncodeClampsValuesOutsideTheStoredRanges(t *testing.T) {
	s := splat.Splat{
		Rotation: geom.IdentityQuat(),
		Scale:    geom.Vec3{X: 0, Y: 1e-9, Z: 1e9},
		Opacity:  1,
		SH0:      geom.Vec3{X: -10, Y: 10, Z: 0},
		SHRest:   []geom.Vec3{{X: -5, Y: 5, Z: 0}, {}, {}},
	}
	raw := decompress(t, encode(t, []splat.Splat{s}, Version3))
	alpha, colors, scales, sh := raw[25], raw[26:29], raw[29:32], raw[36:39]
	if alpha != 255 {
		t.Errorf("opacity byte = %d, want 255", alpha)
	}
	if !bytes.Equal(colors, []byte{0, 255, 128}) {
		t.Errorf("color bytes = %v, want [0 255 128]", colors)
	}
	if !bytes.Equal(scales, []byte{0, 0, 255}) {
		t.Errorf("scale bytes = %v, want [0 0 255]", scales)
	}
	if !bytes.Equal(sh, []byte{0, 255, 128}) {
		t.Errorf("SH bytes = %v, want [0 255 128]", sh)
	}
}

func TestEncodeTreatsOppositeQuaternionsAlike(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	splats := randomSplats(rng, 200, 0)
	opposite := make([]splat.Splat, len(splats))
	for i, s := range splats {
		q := s.Rotation
		s.Rotation = geom.Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: -q.W}
		opposite[i] = s
	}
	for _, version := range []Version{Version2, Version3} {
		if !bytes.Equal(decompress(t, encode(t, splats, version)), decompress(t, encode(t, opposite, version))) {
			t.Errorf("v%d: q and -q encode differently", version)
		}
	}
}

func TestEncodeChoosesFractionalBits(t *testing.T) {
	tests := []struct {
		farthest float64
		want     int
	}{
		{0, 12},
		{100, 12},
		{2047.9, 12},
		{2047.9999, 11}, // rounds to 2^23 at 12 bits, one past the largest value
		{-2048, 11},
		{3000, 11},
		{5000, 10},
		{8_388_607, 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.farthest), func(t *testing.T) {
			splats := []splat.Splat{
				{Center: geom.Vec3{X: 1.234, Y: -0.5, Z: 0.25}, Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 1, Y: 1, Z: 1}},
				{Center: geom.Vec3{Y: tt.farthest}, Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 1, Y: 1, Z: 1}},
			}
			d := decode(t, encode(t, splats, DefaultVersion))
			if d.fractionalBits != tt.want {
				t.Errorf("fractional bits = %d, want %d", d.fractionalBits, tt.want)
			}
			step := 0.5/float64(int(1)<<tt.want) + slack
			for i, s := range splats {
				assertWithin(t, fmt.Sprintf("splat %d center", i), d.splats[i].Center, s.Center, step)
			}
		})
	}
}

func TestEncodeIsSafeForConcurrentUse(t *testing.T) {
	const workers = 8
	inputs := make([][]splat.Splat, workers)
	want := make([][]byte, workers)
	for i := range inputs {
		inputs[i] = randomSplats(rand.New(rand.NewPCG(uint64(i), 9)), 100+50*i, []int{0, 3, 8, 15}[i%4])
		want[i] = encode(t, inputs[i], Version(2+i%2))
	}
	var wg sync.WaitGroup
	got := make([][][]byte, workers)
	errs := make([]error, workers)
	for i := range workers {
		wg.Go(func() {
			for range 10 {
				var out bytes.Buffer
				if err := Encode(&out, inputs[i], Version(2+i%2)); err != nil {
					errs[i] = err
					return
				}
				got[i] = append(got[i], out.Bytes())
			}
		})
	}
	wg.Wait()
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		for run, payload := range got[i] {
			if !bytes.Equal(payload, want[i]) {
				t.Errorf("worker %d run %d wrote a different payload than a lone Encode", i, run)
			}
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestEncodeReturnsWriteErrors(t *testing.T) {
	errDiskFull := errors.New("disk full")
	err := Encode(failingWriter{errDiskFull}, randomSplats(rand.New(rand.NewPCG(5, 6)), 100, 3), DefaultVersion)
	if !errors.Is(err, errDiskFull) {
		t.Errorf("error = %v, want %v", err, errDiskFull)
	}
}

func TestEncodeErrors(t *testing.T) {
	valid := func(shCount int) splat.Splat {
		return splat.Splat{Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 1, Y: 1, Z: 1}, SHRest: make([]geom.Vec3, shCount)}
	}
	withCenter := func(center geom.Vec3) splat.Splat {
		s := valid(0)
		s.Center = center
		return s
	}
	tests := []struct {
		name    string
		splats  []splat.Splat
		version Version
		want    string
	}{
		{"version 0", []splat.Splat{valid(0)}, 0, "unsupported SPZ version 0: use version 2 or 3"},
		{"version 1", []splat.Splat{valid(0)}, 1, "unsupported SPZ version 1"},
		{"version 4", []splat.Splat{valid(0)}, 4, "unsupported SPZ version 4"},
		{"no splats", nil, DefaultVersion, "no splats to encode"},
		{"invalid splat", []splat.Splat{valid(0), withCenter(geom.Vec3{X: math.NaN()})}, DefaultVersion, "splat 1: the splat center is not finite"},
		{"mixed SH degrees", []splat.Splat{valid(3), valid(3), valid(0)}, DefaultVersion, "splat 2 has 0 spherical-harmonic coefficients, but splat 0 has 3"},
		{"center too far", []splat.Splat{withCenter(geom.Vec3{Z: -8_388_608})}, DefaultVersion, "a splat center lies 8.388608e+06 units from the origin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := Encode(&out, tt.splats, tt.version)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
			if out.Len() != 0 {
				t.Errorf("Encode wrote %d bytes before failing, want none", out.Len())
			}
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	splats := randomSplats(rand.New(rand.NewPCG(1, 1)), pooledSplats, 15)
	var out bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		out.Reset()
		if err := Encode(&out, splats, DefaultVersion); err != nil {
			b.Fatal(err)
		}
	}
}
