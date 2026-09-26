package compression

import (
	"math"
)

// QuantizedPositions holds UNSIGNED_SHORT normalized position data (stride = 8 bytes:
// 3 uint16 components + 2 bytes padding) and per-axis dequantization parameters.
type QuantizedPositions struct {
	Data   [][4]uint16 // [0..2] = x,y,z in [0,65535]; [3] = 0 (padding)
	Scale  [3]float64  // (max - min) per axis
	Offset [3]float64  // min per axis
}

// quantizePositions maps []float32 XYZ coords to UNSIGNED_SHORT normalized [0,65535].
// Returns the quantized data and per-axis dequantization parameters.
func quantizePositions(coords [][3]float32) QuantizedPositions {
	n := len(coords)
	if n == 0 {
		return QuantizedPositions{
			Data:   nil,
			Scale:  [3]float64{1, 1, 1},
			Offset: [3]float64{0, 0, 0},
		}
	}

	// Find min/max per axis
	minVal := [3]float32{coords[0][0], coords[0][1], coords[0][2]}
	maxVal := [3]float32{coords[0][0], coords[0][1], coords[0][2]}
	for i := 1; i < n; i++ {
		for axis := 0; axis < 3; axis++ {
			v := coords[i][axis]
			if v < minVal[axis] {
				minVal[axis] = v
			}
			if v > maxVal[axis] {
				maxVal[axis] = v
			}
		}
	}

	// Compute scale and offset per axis in float64.
	// Degenerate axis (all points share the same coordinate): scale=1.0 so the
	// quantize formula produces f=0.0 for every point, and the dequant matrix
	// has a well-formed diagonal (never zero).
	var scale [3]float64
	var offset [3]float64
	for axis := 0; axis < 3; axis++ {
		span := float64(maxVal[axis]) - float64(minVal[axis])
		if span <= 0 {
			scale[axis] = 1.0
		} else {
			scale[axis] = span
		}
		offset[axis] = float64(minVal[axis])
	}

	// Quantize each point
	data := make([][4]uint16, n)
	for i := 0; i < n; i++ {
		for axis := 0; axis < 3; axis++ {
			f := (float64(coords[i][axis]) - offset[axis]) / scale[axis]
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			data[i][axis] = uint16(math.Round(f * 65535.0))
		}
		// data[i][3] is zero-initialized (padding)
	}

	return QuantizedPositions{
		Data:   data,
		Scale:  scale,
		Offset: offset,
	}
}

// dequantNodeMatrix returns the 4x4 column-major glTF matrix that reconstructs
// original float positions from normalized uint16 values via: pos = (q/65535)*scale + offset.
func dequantNodeMatrix(q QuantizedPositions) [16]float64 {
	return [16]float64{
		q.Scale[0], 0, 0, 0, // col 0
		0, q.Scale[1], 0, 0, // col 1
		0, 0, q.Scale[2], 0, // col 2
		q.Offset[0], q.Offset[1], q.Offset[2], 1, // col 3
	}
}
