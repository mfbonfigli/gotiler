package tiler

import (
	"fmt"
	"math"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Axis identifies which axis of the input model points up.
type Axis string

const (
	// AxisZ treats the input Z axis as up (the default).
	AxisZ Axis = "z"
	// AxisY treats the input Y axis as up.
	AxisY Axis = "y"
	// AxisX treats the input X axis as up.
	AxisX Axis = "x"
)

// Placement georeferences an ungeoreferenced point cloud: the input
// coordinates are treated as a local up-oriented cartesian system in meters
// and placed on the WGS84 ellipsoid at the given position and orientation.
// The zero value places the model origin on the ellipsoid surface at
// longitude 0, latitude 0, height 0, axes aligned east-north-up, scale 1.
//
// Heading, pitch and roll follow the CesiumJS convention: heading is the
// rotation from local north, positive increasing eastward; pitch is the
// rotation from the local east-north plane, positive above the plane; roll is
// the rotation applied about the local east axis.
type Placement struct {
	// Longitude of the model origin, in EPSG:4326 degrees.
	Longitude float64
	// Latitude of the model origin, in EPSG:4326 degrees.
	Latitude float64
	// Height of the model origin in meters above the WGS84 ellipsoid.
	Height float64
	// Heading in degrees from local north, positive eastward.
	Heading float64
	// Pitch in degrees from the local east-north plane, positive above.
	Pitch float64
	// Roll in degrees about the local east axis.
	Roll float64
	// Scale is the uniform scale applied to the model. Zero means 1.
	Scale float64
	// UpAxis is the input model's up axis. Empty means AxisZ.
	UpAxis Axis
}

// wgs84SemiMajor and wgs84Flattening define the WGS84 ellipsoid.
const (
	wgs84SemiMajor  = 6378137.0
	wgs84Flattening = 1.0 / 298.257223563
)

// Transform returns the local-to-EPSG:4978 transform matrix realizing the
// placement, or an error when the placement parameters are invalid.
func (p Placement) Transform() (model.Transform, error) {
	if err := p.validate(); err != nil {
		return model.Transform{}, err
	}
	scale := p.Scale
	if scale == 0 {
		scale = 1
	}

	lon := p.Longitude * math.Pi / 180
	lat := p.Latitude * math.Pi / 180
	heading := p.Heading * math.Pi / 180
	pitch := p.Pitch * math.Pi / 180
	roll := p.Roll * math.Pi / 180

	// geodetic to ECEF on the WGS84 ellipsoid
	e2 := wgs84Flattening * (2 - wgs84Flattening)
	sinLat, cosLat := math.Sin(lat), math.Cos(lat)
	sinLon, cosLon := math.Sin(lon), math.Cos(lon)
	n := wgs84SemiMajor / math.Sqrt(1-e2*sinLat*sinLat)
	origin := model.Vector{
		X: (n + p.Height) * cosLat * cosLon,
		Y: (n + p.Height) * cosLat * sinLon,
		Z: (n*(1-e2) + p.Height) * sinLat,
	}

	// local east-north-up axes as columns of the ENU-to-ECEF rotation
	enu := mat3{
		{-sinLon, -sinLat * cosLon, cosLat * cosLon},
		{cosLon, -sinLat * sinLon, cosLat * sinLon},
		{0, cosLat, sinLat},
	}

	// heading-pitch-roll in the local east-north-up frame, following the
	// CesiumJS convention: rotation about -z by heading, about -y by pitch,
	// about +x by roll
	hpr := rotZ(-heading).mul(rotY(-pitch)).mul(rotX(roll))

	rotation := enu.mul(hpr).mul(upAxisRotation(p.UpAxis))

	// forward: scaled rotation plus translation; inverse: transposed rotation
	// divided by the scale (valid because the rotation part is orthonormal)
	var fwd, inv [4][4]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			fwd[r][c] = rotation[r][c] * scale
			inv[r][c] = rotation[c][r] / scale
		}
	}
	fwd[0][3], fwd[1][3], fwd[2][3] = origin.X, origin.Y, origin.Z
	fwd[3][3] = 1
	inv[0][3] = -(inv[0][0]*origin.X + inv[0][1]*origin.Y + inv[0][2]*origin.Z)
	inv[1][3] = -(inv[1][0]*origin.X + inv[1][1]*origin.Y + inv[1][2]*origin.Z)
	inv[2][3] = -(inv[2][0]*origin.X + inv[2][1]*origin.Y + inv[2][2]*origin.Z)
	inv[3][3] = 1

	return model.NewTransformWithInverse(fwd, inv), nil
}

func (p Placement) validate() error {
	for name, v := range map[string]float64{
		"longitude": p.Longitude, "latitude": p.Latitude, "height": p.Height,
		"heading": p.Heading, "pitch": p.Pitch, "roll": p.Roll, "scale": p.Scale,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("placement %s must be finite", name)
		}
	}
	if p.Latitude < -90 || p.Latitude > 90 {
		return fmt.Errorf("placement latitude %v out of range [-90, 90]", p.Latitude)
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		return fmt.Errorf("placement longitude %v out of range [-180, 180]", p.Longitude)
	}
	if p.Scale < 0 {
		return fmt.Errorf("placement scale %v must be positive", p.Scale)
	}
	switch p.UpAxis {
	case "", AxisZ, AxisY, AxisX:
	default:
		return fmt.Errorf("placement up axis %q must be one of x, y, z", p.UpAxis)
	}
	return nil
}

// upAxisRotation maps the input model's up axis to the local Z axis while
// keeping the frame right-handed.
func upAxisRotation(axis Axis) mat3 {
	switch axis {
	case AxisY:
		// (x, y, z) -> (x, -z, y): the glTF-style Y-up to Z-up rotation
		return mat3{
			{1, 0, 0},
			{0, 0, -1},
			{0, 1, 0},
		}
	case AxisX:
		// (x, y, z) -> (-z, y, x)
		return mat3{
			{0, 0, -1},
			{0, 1, 0},
			{1, 0, 0},
		}
	default:
		return mat3{
			{1, 0, 0},
			{0, 1, 0},
			{0, 0, 1},
		}
	}
}

// mat3 is a row-major 3x3 matrix.
type mat3 [3][3]float64

func (a mat3) mul(b mat3) mat3 {
	var out mat3
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			out[r][c] = a[r][0]*b[0][c] + a[r][1]*b[1][c] + a[r][2]*b[2][c]
		}
	}
	return out
}

func rotX(angle float64) mat3 {
	s, c := math.Sin(angle), math.Cos(angle)
	return mat3{
		{1, 0, 0},
		{0, c, -s},
		{0, s, c},
	}
}

func rotY(angle float64) mat3 {
	s, c := math.Sin(angle), math.Cos(angle)
	return mat3{
		{c, 0, s},
		{0, 1, 0},
		{-s, 0, c},
	}
}

func rotZ(angle float64) mat3 {
	s, c := math.Sin(angle), math.Cos(angle)
	return mat3{
		{c, -s, 0},
		{s, c, 0},
		{0, 0, 1},
	}
}
