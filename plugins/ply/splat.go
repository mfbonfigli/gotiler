package ply

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// splatProperties are the vertex properties of a 3D Gaussian Splatting PLY
// file, as the reference implementation writes them: the center, the
// degree-0 spherical-harmonic color, the opacity logit, the log scale and the
// rotation quaternion in W, X, Y, Z order.
var splatProperties = []string{
	"x", "y", "z",
	"f_dc_0", "f_dc_1", "f_dc_2",
	"opacity",
	"scale_0", "scale_1", "scale_2",
	"rot_0", "rot_1", "rot_2", "rot_3",
}

// shRestPrefix starts the names of the higher-degree spherical-harmonic
// properties, f_rest_0 to f_rest_44, which hold all the coefficients of the
// red channel, then of the green and of the blue one.
const shRestPrefix = "f_rest_"

// splatMapper converts the scalar values of a vertex, as returned by
// vertexDecoder.next, into a splat.
type splatMapper struct {
	// value indices of the properties in splatProperties order
	center, sh0, scale [3]int
	opacity            int
	rotation           [4]int
	// shRest holds the value index of f_rest_i at index i.
	shRest []int
}

func newSplatMapper(properties []property) (*splatMapper, error) {
	names := make([]string, len(properties))
	for i, p := range properties {
		names[i] = model.CanonicalAttributeName(p.name)
	}
	if slices.Contains(names, "packed_position") {
		return nil, errors.New("the file is a compressed Gaussian splat PLY, which is not supported yet: export it as a standard splat PLY")
	}

	indices := make([]int, len(splatProperties))
	var missing []string
	for i, name := range splatProperties {
		indices[i] = slices.Index(names, name)
		if indices[i] < 0 {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the file is not a Gaussian splat PLY: its vertex element is missing %s", strings.Join(missing, ", "))
	}
	shRest, err := shRestIndices(names)
	if err != nil {
		return nil, err
	}
	return &splatMapper{
		center:   [3]int{indices[0], indices[1], indices[2]},
		sh0:      [3]int{indices[3], indices[4], indices[5]},
		opacity:  indices[6],
		scale:    [3]int{indices[7], indices[8], indices[9]},
		rotation: [4]int{indices[10], indices[11], indices[12], indices[13]},
		shRest:   shRest,
	}, nil
}

// shRestIndices returns the value indices of the f_rest properties, ordered
// by their number, which must run from 0 without gaps and fill complete
// spherical-harmonic degrees.
func shRestIndices(names []string) ([]int, error) {
	byNumber := map[int]int{}
	for i, name := range names {
		suffix, ok := strings.CutPrefix(name, shRestPrefix)
		if !ok {
			continue
		}
		number, err := strconv.Atoi(suffix)
		if err != nil || number < 0 || strconv.Itoa(number) != suffix {
			return nil, fmt.Errorf("the property %s is not a spherical-harmonic coefficient: expected a name like %s7", name, shRestPrefix)
		}
		byNumber[number] = i
	}
	switch len(byNumber) {
	case 0, 9, 24, 45:
	default:
		return nil, fmt.Errorf("the file has %d %s properties: spherical harmonics of degree 1, 2 or 3 need 9, 24 or 45", len(byNumber), shRestPrefix)
	}
	indices := make([]int, len(byNumber))
	for number := range indices {
		index, ok := byNumber[number]
		if !ok {
			return nil, fmt.Errorf("the file has no %s%d property, but has higher ones", shRestPrefix, number)
		}
		indices[number] = index
	}
	return indices, nil
}

// shDegree returns the degree of the spherical harmonics of the splats.
func (m *splatMapper) shDegree() int {
	switch len(m.shRest) {
	case 9:
		return 1
	case 24:
		return 2
	case 45:
		return 3
	default:
		return 0
	}
}

// splat returns the splat of a vertex, with its scale and opacity activated,
// or false when the vertex holds non-finite or out-of-range values.
func (m *splatMapper) splat(values []float64, sourceIndex int) (splat.Splat, bool) {
	rotation := geom.Quat{
		X: values[m.rotation[1]],
		Y: values[m.rotation[2]],
		Z: values[m.rotation[3]],
		W: values[m.rotation[0]],
	}
	if !rotation.IsFinite() {
		return splat.Splat{}, false
	}
	logScale := m.vec3(values, m.scale)
	s := splat.Splat{
		Center:      m.vec3(values, m.center),
		Rotation:    rotation.Normalize(),
		Scale:       geom.Vec3{X: math.Exp(logScale.X), Y: math.Exp(logScale.Y), Z: math.Exp(logScale.Z)},
		Opacity:     sigmoid(values[m.opacity]),
		SH0:         m.vec3(values, m.sh0),
		SHRest:      m.shRestCoefficients(values),
		SourceIndex: sourceIndex,
	}
	return s, s.Validate() == nil
}

func (m *splatMapper) vec3(values []float64, indices [3]int) geom.Vec3 {
	return geom.Vec3{X: values[indices[0]], Y: values[indices[1]], Z: values[indices[2]]}
}

// shRestCoefficients regroups the f_rest values, stored channel by channel,
// into one RGB triplet per coefficient.
func (m *splatMapper) shRestCoefficients(values []float64) []geom.Vec3 {
	perChannel := len(m.shRest) / 3
	if perChannel == 0 {
		return nil
	}
	coefficients := make([]geom.Vec3, perChannel)
	for i := range coefficients {
		coefficients[i] = geom.Vec3{
			X: values[m.shRest[i]],
			Y: values[m.shRest[i+perChannel]],
			Z: values[m.shRest[i+2*perChannel]],
		}
	}
	return coefficients
}

// sigmoid returns the logistic function of x, computed without overflowing
// for large negative x.
func sigmoid(x float64) float64 {
	if x >= 0 {
		return 1 / (1 + math.Exp(-x))
	}
	z := math.Exp(x)
	return z / (1 + z)
}

// SplatReader reads the vertices of a 3D Gaussian Splatting PLY file as
// splats. It is not safe for concurrent use.
type SplatReader struct {
	filename string
	file     *os.File
	decoder  *vertexDecoder
	mapper   *splatMapper
	skipped  int
}

// NewSplatReader opens a 3D Gaussian Splatting PLY file.
func NewSplatReader(filename string) (*SplatReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	r := &SplatReader{filename: filename, file: file}
	if err := r.open(); err != nil {
		file.Close() //nolint:errcheck
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return r, nil
}

func (r *SplatReader) open() error {
	buffer := bufio.NewReaderSize(r.file, readBufferSize)
	h, err := readHeader(buffer)
	if err != nil {
		return err
	}
	if r.decoder, err = newVertexDecoder(buffer, h); err != nil {
		return err
	}
	if r.mapper, err = newSplatMapper(r.decoder.scalars); err != nil {
		return err
	}
	return checkBinaryDataIsComplete(r.file, r.decoder)
}

// NumberOfSplats returns the vertex count declared in the header. Invalid
// splats are skipped while reading, so fewer splats may be returned.
func (r *SplatReader) NumberOfSplats() int {
	return r.decoder.count
}

// SHDegree returns the degree of the spherical harmonics of the splats, from
// 0 to 3.
func (r *SplatReader) SHDegree() int {
	return r.mapper.shDegree()
}

// Next returns the next splat, or io.EOF once all the vertices are read.
// Vertices holding non-finite or out-of-range values are skipped.
func (r *SplatReader) Next() (splat.Splat, error) {
	for {
		values, err := r.decoder.next()
		if errors.Is(err, io.EOF) {
			return splat.Splat{}, io.EOF
		}
		if err != nil {
			return splat.Splat{}, fmt.Errorf("%s: %w", r.filename, err)
		}
		if s, ok := r.mapper.splat(values, r.decoder.index-1); ok {
			return s, nil
		}
		r.skipped++
	}
}

// Skipped returns how many of the vertices read so far were skipped as
// invalid splats.
func (r *SplatReader) Skipped() int {
	return r.skipped
}

// Close closes the file.
func (r *SplatReader) Close() error {
	return r.file.Close()
}
