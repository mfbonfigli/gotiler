package spz

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// decoded is an SPZ payload read back the way the reference decoder reads
// it, with activated scales and opacities.
type decoded struct {
	version        Version
	shDegree       int
	fractionalBits int
	flags          byte
	splats         []splat.Splat
}

// decompress returns the uncompressed bytes of a payload.
func decompress(t *testing.T, payload []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("the payload is not gzip data: %v", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading the gzip data: %v", err)
	}
	return raw
}

func decode(t *testing.T, payload []byte) decoded {
	t.Helper()
	raw := decompress(t, payload)
	if len(raw) < headerSize {
		t.Fatalf("the payload holds %d bytes, fewer than a header", len(raw))
	}
	if got := binary.LittleEndian.Uint32(raw); got != magic {
		t.Fatalf("magic = %#x, want %#x", got, magic)
	}
	d := decoded{
		version:        Version(binary.LittleEndian.Uint32(raw[4:])),
		shDegree:       int(raw[12]),
		fractionalBits: int(raw[13]),
		flags:          raw[14],
	}
	if raw[15] != 0 {
		t.Errorf("reserved byte = %d, want 0", raw[15])
	}
	rotationBytes := map[Version]int{Version2: 3, Version3: 4}[d.version]
	if rotationBytes == 0 {
		t.Fatalf("version = %d, want 2 or 3", d.version)
	}
	shCount := map[int]int{0: 0, 1: 3, 2: 8, 3: 15}[d.shDegree]
	n := int(binary.LittleEndian.Uint32(raw[8:]))
	if want := headerSize + n*(9+1+3+3+rotationBytes+3*shCount); len(raw) != want {
		t.Fatalf("the payload holds %d bytes, want %d for %d splats of degree %d", len(raw), want, n, d.shDegree)
	}

	positions := raw[headerSize:]
	alphas := positions[9*n:]
	colors := alphas[n:]
	scales := colors[3*n:]
	rotations := scales[3*n:]
	sh := rotations[rotationBytes*n:]
	positionStep := 1 / float64(int(1)<<d.fractionalBits)
	d.splats = make([]splat.Splat, n)
	for i := range d.splats {
		s := &d.splats[i]
		s.Center = vec3(func(c int) float64 {
			b := positions[9*i+3*c:]
			v := int32(uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16) << 8 >> 8
			return float64(v) * positionStep
		})
		s.Opacity = float64(alphas[i]) / 255
		s.SH0 = vec3(func(c int) float64 { return (float64(colors[3*i+c])/255 - 0.5) / colorScale })
		s.Scale = vec3(func(c int) float64 { return math.Exp(float64(scales[3*i+c])/scaleLogFactor - scaleLogOffset) })
		rotation := rotations[rotationBytes*i:]
		if d.version == Version2 {
			s.Rotation = unpackRotationFirstThree(rotation)
		} else {
			s.Rotation = unpackRotationSmallestThree(rotation)
		}
		for j := range shCount {
			s.SHRest = append(s.SHRest, vec3(func(c int) float64 {
				return (float64(sh[(i*shCount+j)*3+c]) - 128) / 128
			}))
		}
	}
	return d
}

func vec3(component func(c int) float64) geom.Vec3 {
	return geom.Vec3{X: component(0), Y: component(1), Z: component(2)}
}

func unpackRotationFirstThree(b []byte) geom.Quat {
	x := float64(b[0])/127.5 - 1
	y := float64(b[1])/127.5 - 1
	z := float64(b[2])/127.5 - 1
	return geom.Quat{X: x, Y: y, Z: z, W: math.Sqrt(max(0, 1-x*x-y*y-z*z))}
}

func unpackRotationSmallestThree(b []byte) geom.Quat {
	packed := binary.LittleEndian.Uint32(b)
	largest := int(packed >> 30)
	var values [4]float64
	sumOfSquares := 0.0
	for i := 3; i >= 0; i-- {
		if i == largest {
			continue
		}
		values[i] = float64(packed&511) / 511 / math.Sqrt2
		if packed>>9&1 == 1 {
			values[i] = -values[i]
		}
		packed >>= 10
		sumOfSquares += values[i] * values[i]
	}
	values[largest] = math.Sqrt(max(0, 1-sumOfSquares))
	return geom.Quat{X: values[0], Y: values[1], Z: values[2], W: values[3]}
}

// randomSplats returns splats whose values all lie inside the ranges SPZ can
// represent, so that only rounding separates them from their decoded copies.
func randomSplats(rng *rand.Rand, count, shCount int) []splat.Splat {
	uniform := func(low, high float64) float64 { return low + (high-low)*rng.Float64() }
	randomVec3 := func(low, high float64) geom.Vec3 {
		return geom.Vec3{X: uniform(low, high), Y: uniform(low, high), Z: uniform(low, high)}
	}
	splats := make([]splat.Splat, count)
	for i := range splats {
		splats[i] = splat.Splat{
			Center:   randomVec3(-100, 100),
			Rotation: geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize(),
			Scale:    randomVec3(0.001, 1),
			Opacity:  rng.Float64(),
			SH0:      randomVec3(-2, 2),
		}
		for range shCount {
			splats[i].SHRest = append(splats[i].SHRest, randomVec3(-0.9, 0.9))
		}
	}
	return splats
}

// rotationAngle returns the angle in degrees of the rotation between a and b.
func rotationAngle(a, b geom.Quat) float64 {
	a, b = a.Normalize(), b.Normalize()
	dot := math.Abs(a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W)
	return 2 * math.Acos(min(1, dot)) * 180 / math.Pi
}

func assertWithin(t *testing.T, name string, got, want geom.Vec3, step float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > step || math.Abs(got.Y-want.Y) > step || math.Abs(got.Z-want.Z) > step {
		t.Errorf("%s = %+v, want %+v within %g", name, got, want, step)
	}
}
