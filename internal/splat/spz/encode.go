// Package spz encodes Gaussian splats as SPZ, the compressed splat format
// that the KHR_gaussian_splatting_compression_spz_2 glTF extension embeds.
//
// Splats are written in the axes they are given in. The draft extension asks
// writers to convert glTF axes to the SPZ ones, but the CesiumJS loader reads
// the stored values as glTF axes, so the caller passes glTF axes and no
// conversion happens here.
package spz

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/utils"
)

// Version is an SPZ file version. The versions differ only in how they store
// rotations.
type Version uint32

const (
	// Version2 stores the x, y and z components of each rotation in one byte
	// each. It is the version the draft extension names.
	Version2 Version = 2
	// Version3 stores each rotation in four bytes, as its three smallest
	// components and the index of the largest one, which is about five times
	// finer than Version2.
	Version3 Version = 3

	DefaultVersion = Version2
)

const (
	magic      = 0x5053474e // "NGSP" in little-endian order
	headerSize = 16

	// Positions are signed 24-bit fixed-point numbers. 12 fractional bits,
	// the most used here, give a resolution of about a quarter millimeter.
	positionBytes     = 3
	maxPositionValue  = 1<<23 - 1
	maxFractionalBits = 12

	// Scales are stored as (log(scale) + scaleLogOffset) · scaleLogFactor.
	scaleLogOffset = 10
	scaleLogFactor = 16

	// Colors are stored as SH0 · colorScale · 255 + 127.5.
	colorScale = 0.15

	// Spherical-harmonic coefficients are stored as x · 128 + 128, rounded
	// to a multiple of 8 (5 bits) for degree 1 and of 16 (4 bits) for the
	// higher degrees, as the reference encoder does.
	shDegree1Coefficients = 3
	shDegree1Step         = 8
	shHigherStep          = 16
)

// pooledSplats sizes the pooled uncompressed payloads: 20,000 splats of
// degree 3, the default tile size of gosplattiler, which the splat tiler is
// ported from. Larger payloads are allocated and then left to the garbage
// collector.
const pooledSplats = 20_000

// Encode runs once per tile, so its two large buffers are pooled: the
// uncompressed payload, and the gzip writer, whose compressor state takes
// about 750 KB however small the tile is.
var (
	rawPool     = utils.NewSlicePool[byte](headerSize + pooledSplats*splatSize(4, 15))
	gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}
)

// Encode writes the gzip-compressed SPZ payload of splats to w. All the
// splats must have spherical harmonics of the same degree. Nothing is written
// when the splats or the version are invalid. Encode is safe for concurrent
// use.
func Encode(w io.Writer, splats []splat.Splat, version Version) error {
	rotationBytes, err := rotationSize(version)
	if err != nil {
		return err
	}
	if err := validate(splats); err != nil {
		return err
	}
	fractionalBits, err := chooseFractionalBits(splats)
	if err != nil {
		return err
	}

	n := len(splats)
	shCount := len(splats[0].SHRest)
	rawPtr := rawPool.GetWithMinCapacity(headerSize + n*splatSize(rotationBytes, shCount))
	defer rawPool.Put(rawPtr)
	// Pooled memory is not zeroed: every byte of raw is written below.
	raw := *rawPtr
	writeHeader(raw, version, n, shDegree(shCount), fractionalBits)
	positions := raw[headerSize:]
	alphas := positions[n*3*positionBytes:]
	colors := alphas[n:]
	scales := colors[n*3:]
	rotations := scales[n*3:]
	sh := rotations[n*rotationBytes:]

	positionFactor := float64(int(1) << fractionalBits)
	for i, s := range splats {
		for c, v := range components(s.Center) {
			putInt24(positions[(3*i+c)*positionBytes:], int32(math.Round(v*positionFactor)))
		}
		alphas[i] = toUint8(s.Opacity * 255)
		for c, v := range components(s.SH0) {
			colors[3*i+c] = toUint8(v*colorScale*255 + 127.5)
		}
		for c, v := range components(s.Scale) {
			scales[3*i+c] = toUint8((math.Log(v) + scaleLogOffset) * scaleLogFactor)
		}
		rotation := rotations[i*rotationBytes : (i+1)*rotationBytes]
		if version == Version2 {
			packRotationFirstThree(rotation, s.Rotation)
		} else {
			packRotationSmallestThree(rotation, s.Rotation)
		}
		for j, coefficient := range s.SHRest {
			step := shHigherStep
			if j < shDegree1Coefficients {
				step = shDegree1Step
			}
			for c, v := range components(coefficient) {
				sh[(i*shCount+j)*3+c] = quantizeSH(v, step)
			}
		}
	}
	return compress(w, raw)
}

// splatSize returns the bytes one splat takes in the uncompressed payload.
func splatSize(rotationBytes, shCount int) int {
	return 3*positionBytes + 1 + 3 + 3 + rotationBytes + 3*shCount
}

func rotationSize(version Version) (int, error) {
	switch version {
	case Version2:
		return 3, nil
	case Version3:
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported SPZ version %d: use version 2 or 3", version)
	}
}

func validate(splats []splat.Splat) error {
	if len(splats) == 0 {
		return errors.New("no splats to encode")
	}
	shCount := len(splats[0].SHRest)
	for i, s := range splats {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("splat %d: %w", i, err)
		}
		if len(s.SHRest) != shCount {
			return fmt.Errorf("splat %d has %d spherical-harmonic coefficients, but splat 0 has %d: all the splats must have the same degree", i, len(s.SHRest), shCount)
		}
	}
	return nil
}

// chooseFractionalBits returns the finest position precision, up to
// maxFractionalBits, at which every center fits the 24-bit position values.
func chooseFractionalBits(splats []splat.Splat) (int, error) {
	largest := 0.0
	for _, s := range splats {
		largest = max(largest, math.Abs(s.Center.X), math.Abs(s.Center.Y), math.Abs(s.Center.Z))
	}
	for bits := maxFractionalBits; bits >= 0; bits-- {
		if math.Round(largest*float64(int(1)<<bits)) <= maxPositionValue {
			return bits, nil
		}
	}
	return 0, fmt.Errorf("a splat center lies %g units from the origin, beyond the %d units SPZ positions can hold", largest, maxPositionValue)
}

func writeHeader(raw []byte, version Version, count, shDegree, fractionalBits int) {
	binary.LittleEndian.PutUint32(raw[0:], magic)
	binary.LittleEndian.PutUint32(raw[4:], uint32(version))
	binary.LittleEndian.PutUint32(raw[8:], uint32(count))
	raw[12] = byte(shDegree)
	raw[13] = byte(fractionalBits)
	raw[14] = 0 // flags: the splats are not marked as antialiased
	raw[15] = 0 // reserved
}

func shDegree(coefficients int) int {
	switch coefficients {
	case 3:
		return 1
	case 8:
		return 2
	case 15:
		return 3
	default:
		return 0
	}
}

func components(v geom.Vec3) [3]float64 {
	return [3]float64{v.X, v.Y, v.Z}
}

func putInt24(b []byte, v int32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}

// packRotationFirstThree writes the x, y and z components of the rotation,
// negated if needed so that the dropped w is not negative.
func packRotationFirstThree(b []byte, q geom.Quat) {
	q = q.Normalize()
	sign := 1.0
	if q.W < 0 {
		sign = -1
	}
	for c, v := range [3]float64{q.X, q.Y, q.Z} {
		b[c] = toUint8(sign*v*127.5 + 127.5)
	}
}

// packRotationSmallestThree writes the rotation as a little-endian uint32
// holding, from the top, the 2-bit index of its largest component and then,
// for each other component in X, Y, Z, W order, a sign bit and a 9-bit
// magnitude scaled from [0, √½]. The components are negated if needed so that
// the dropped one is not negative.
func packRotationSmallestThree(b []byte, q geom.Quat) {
	q = q.Normalize()
	values := [4]float64{q.X, q.Y, q.Z, q.W}
	largest := 0
	for i := 1; i < 4; i++ {
		if math.Abs(values[i]) > math.Abs(values[largest]) {
			largest = i
		}
	}
	flip := values[largest] < 0
	packed := uint32(largest)
	for i, v := range values {
		if i == largest {
			continue
		}
		var negative uint32
		if (v < 0) != flip {
			negative = 1
		}
		magnitude := min(uint32(math.Round(math.Abs(v)*math.Sqrt2*511)), 511)
		packed = packed<<10 | negative<<9 | magnitude
	}
	binary.LittleEndian.PutUint32(b, packed)
}

// toUint8 rounds x and clamps it to a byte. NaN cannot occur: the splats are
// validated, and infinities, such as the log of a zero scale, are clamped.
func toUint8(x float64) byte {
	return byte(min(max(math.Round(x), 0), 255))
}

func quantizeSH(x float64, step int) byte {
	q := int(math.Round(x*128) + 128)
	q = (q + step/2) / step * step
	return byte(min(max(q, 0), 255))
}

// compress gzips raw into w at the default level, which the reference encoder
// also uses.
func compress(w io.Writer, raw []byte) error {
	zw := gzipWriters.Get().(*gzip.Writer)
	defer gzipWriters.Put(zw)
	zw.Reset(w)
	if _, err := zw.Write(raw); err != nil {
		return err
	}
	return zw.Close()
}
