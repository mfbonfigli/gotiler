package e57

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	libe57 "github.com/mfbonfigli/golibe57/e57"
	"github.com/mfbonfigli/golibe57/types"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

func init() {
	plugin.RegisterPointCloudReader(".e57", func(filename, crs string, opts plugin.ReaderOptions) (pointcloud.Reader, error) {
		return NewReader(filename, crs, opts.RequestedAttributes)
	})
}

var e57AttributeAliases = map[string]string{}

// e57StandardSourceNames are the canonical source names emitted by the
// standard-field encode path. Prototype extension fields whose canonical name
// collides with one of these are excluded from the extras plan so each name is
// emitted by exactly one path with a stable type (e.g. the common
// "classification" extension is consumed by the standard path).
var e57StandardSourceNames = map[string]struct{}{
	model.AttrIntensity:       {},
	model.AttrClassification:  {},
	model.AttrReturnNumber:    {},
	model.AttrNumberOfReturns: {},
	"raw_intensity":           {},
	"intensity_invalid":       {},
	"return_index":            {},
	"return_count":            {},
	"cartesian_invalid":       {},
	"spherical_invalid":       {},
	"raw_color_red":           {},
	"raw_color_green":         {},
	"raw_color_blue":          {},
	"color_invalid":           {},
	"row_index":               {},
	"column_index":            {},
	"timestamp":               {},
	"timestamp_invalid":       {},
	"normal_x":                {},
	"normal_y":                {},
	"normal_z":                {},
}

// e57Avail records which fields at least one scan of the file carries (the
// union across scans), resolved once at open time to build a fixed schema.
type e57Avail struct {
	intensity      bool
	intInvalid     bool
	returnIdx      bool
	returnCount    bool
	cartInvalid    bool
	sphInvalid     bool
	color          bool
	colorInvalid   bool
	rowIdx         bool
	colIdx         bool
	timestamp      bool
	timeInvalid    bool
	normalX        bool
	normalY        bool
	normalZ        bool
	classification bool
	// extras is the typed union of requested non-standard prototype fields
	// across scans, keyed by canonical output name; extrasOrder preserves
	// first-appearance order.
	extras      map[string]model.AttributeType
	extrasOrder []string
}

// e57EncodePlan holds the packed-value byte offset of every schema attribute,
// or -1 when the attribute is not part of the schema. advanceScan derives a
// per-scan copy with fields the scan does not carry reset to -1, so the
// per-point path is straight-line binary encoding with no map lookups.
type e57EncodePlan struct {
	size             int
	intensity        int
	rawIntensity     int
	intensityInvalid int
	classification   int
	returnNumber     int
	returnIndex      int
	numberOfReturns  int
	returnCount      int
	cartesianInvalid int
	sphericalInvalid int
	rawColorRed      int
	rawColorGreen    int
	rawColorBlue     int
	colorInvalid     int
	rowIndex         int
	columnIndex      int
	timestamp        int
	timestampInvalid int
	normalX          int
	normalY          int
	normalZ          int
}

// e57ExtraTarget is the packed destination of one extension field for the
// current scan.
type e57ExtraTarget struct {
	off  int
	size int
	typ  model.AttributeType
}

// E57Reader reads an E57 point cloud file implementing PointCloudReader.
// E57 stores points in scanner-local coordinates; each scan's pose transforms
// them to world frame. Color (uint16 raw) and intensity (float64) are normalised
// to uint8 and uint16 respectively using per-scan limit metadata.
// E57 files carry no embedded CRS so the caller must supply one.
//
// Requested per-point attributes are emitted as packed values laid out per the
// reader schema (the union of the fields carried by the file's scans): fields
// a scan does not carry, and per-point invalid values, are zero.
type E57Reader struct {
	r         *libe57.Reader
	crs       string
	numPoints int
	scanCount int64
	requested map[string]string
	schema    []model.AttributeDescriptor
	plan      e57EncodePlan // schema offsets; -1 = not in schema
	arena     model.AttributeValuesArena

	mu sync.Mutex

	// per-scan iteration state, reset by advanceScan
	curScan    int64
	pr         *libe57.PointReader
	pose       *libe57.PoseTransform
	act        e57EncodePlan             // plan masked by the current scan's field presence
	scanExtras map[string]e57ExtraTarget // extension fields of the current scan, keyed by prototype name

	// which standard fields are present in the current scan
	hasCartesian    bool
	hasCartInvalid  bool
	hasSpherical    bool
	hasSphInvalid   bool
	hasColor        bool
	hasColorInvalid bool
	hasIntensity    bool
	hasIntInvalid   bool
	hasTimeInvalid  bool

	// per-scan normalisation ranges, precomputed in advanceScan
	colorRedMin, colorRedRange     float64
	colorGreenMin, colorGreenRange float64
	colorBlueMin, colorBlueRange   float64
	intensityMin, intensityRange   float64
}

// NewE57Reader opens an E57 file for reading. crs must be non-empty; E57 files
// do not embed coordinate reference system information.
// attrs lists the optional per-point attributes to emit; nil means none.
func NewReader(fileName string, crs string, attrs model.Attributes) (*E57Reader, error) {
	if crs == "" {
		return nil, fmt.Errorf("CRS must be provided for E57 file %s: embedded CRS information is not supported for E57 files", fileName)
	}
	r, err := libe57.OpenReader(fileName, libe57.DefaultReaderOptions())
	if err != nil {
		return nil, fmt.Errorf("open E57 %s: %w", fileName, err)
	}
	e := &E57Reader{
		r:         r,
		crs:       crs,
		numPoints: int(r.TotalPointCount()),
		scanCount: r.GetData3DCount(),
		requested: buildRequestedMap(attrs),
	}
	e.buildSchema()
	return e, nil
}

func NewE57Reader(fileName string, crs string) (*E57Reader, error) {
	return NewReader(fileName, crs, nil)
}

func (e *E57Reader) NumberOfPoints() int { return e.numPoints }
func (e *E57Reader) GetCRS() string      { return e.crs }

// AttributeSchema implements pointcloud.Reader.
func (e *E57Reader) AttributeSchema() []model.AttributeDescriptor {
	return e.schema
}

func (e *E57Reader) Close() {
	if e.pr != nil {
		e.pr.Close() //nolint:errcheck
		e.pr = nil
	}
	e.r.Close() //nolint:errcheck
}

func (e *E57Reader) Reset() error {
	if e.pr != nil {
		e.pr.Close() //nolint:errcheck
		e.pr = nil
	}
	e.curScan = 0
	return nil
}

func buildRequestedMap(attrs model.Attributes) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	requested := make(map[string]string, len(attrs))
	for _, name := range attrs {
		canonical := model.CanonicalAttributeName(name)
		if canonical == "" {
			continue
		}
		source := canonical
		if alias, ok := e57AttributeAliases[canonical]; ok {
			source = model.CanonicalAttributeName(alias)
		}
		requested[source] = canonical
	}
	if len(requested) == 0 {
		return nil
	}
	return requested
}

// detectAvailability inspects every scan's header and prototype and returns
// the union of the fields the file carries.
func (e *E57Reader) detectAvailability() e57Avail {
	av := e57Avail{extras: map[string]model.AttributeType{}}
	for scan := int64(0); scan < e.scanCount; scan++ {
		hdr, err := e.r.ScanInfo(scan)
		if err != nil {
			continue
		}
		pf := hdr.PointFields
		av.intensity = av.intensity || pf.IntensityField
		av.intInvalid = av.intInvalid || pf.IsIntensityInvalidField
		av.returnIdx = av.returnIdx || pf.ReturnIndexField
		av.returnCount = av.returnCount || pf.ReturnCountField
		av.cartInvalid = av.cartInvalid || pf.CartesianInvalidStateField
		av.sphInvalid = av.sphInvalid || pf.SphericalInvalidStateField
		av.color = av.color || pf.ColorRedField
		av.colorInvalid = av.colorInvalid || pf.IsColorInvalidField
		av.rowIdx = av.rowIdx || pf.RowIndexField
		av.colIdx = av.colIdx || pf.ColumnIndexField
		av.timestamp = av.timestamp || pf.TimeStampField
		av.timeInvalid = av.timeInvalid || pf.IsTimeStampInvalidField
		av.normalX = av.normalX || pf.NormalXField
		av.normalY = av.normalY || pf.NormalYField
		av.normalZ = av.normalZ || pf.NormalZField

		fields, err := e.r.PointPrototype(scan)
		if err != nil {
			continue
		}
		for _, f := range fields {
			canonical := model.CanonicalAttributeName(f.Name)
			if canonical == model.AttrClassification {
				av.classification = true
				continue
			}
			if _, std := e57StandardSourceNames[canonical]; std {
				continue
			}
			output, ok := e.requested[canonical]
			if !ok {
				continue
			}
			typ, ok := e57ExtraType(f)
			if !ok {
				continue
			}
			if _, seen := av.extras[output]; seen {
				continue // first typed occurrence wins across scans
			}
			av.extras[output] = typ
			av.extrasOrder = append(av.extrasOrder, output)
		}
	}
	return av
}

// e57ExtraType maps a prototype extension field to the attribute type it is
// emitted as: exact integer widths from the declared range, physical float64
// for scaled integers (the iterator delivers raw*scale+offset), the declared
// precision for floats. String fields have no attribute representation.
func e57ExtraType(f libe57.PointField) (model.AttributeType, bool) {
	switch f.Type {
	case libe57.FieldInteger:
		return integerAttrType(f.Min, f.Max), true
	case libe57.FieldScaledInteger:
		return model.AttributeFloat64, true
	case libe57.FieldFloat:
		if f.Precision == types.PrecisionSingle {
			return model.AttributeFloat32, true
		}
		return model.AttributeFloat64, true
	default:
		return "", false
	}
}

// buildSchema resolves the reader's attribute schema and packed layout from
// the requested names and the file-wide field availability.
func (e *E57Reader) buildSchema() {
	p := &e.plan
	markAllAbsent(p)
	if len(e.requested) == 0 {
		return
	}
	av := e.detectAvailability()

	type stdField struct {
		name string
		typ  model.AttributeType
		ok   bool
		off  *int
	}
	fields := []stdField{
		{model.AttrIntensity, model.AttributeUint16, av.intensity, &p.intensity},
		{"raw_intensity", model.AttributeFloat64, av.intensity, &p.rawIntensity},
		{"intensity_invalid", model.AttributeBool, av.intInvalid, &p.intensityInvalid},
		{model.AttrClassification, model.AttributeUint8, av.classification, &p.classification},
		{model.AttrReturnNumber, model.AttributeUint8, av.returnIdx, &p.returnNumber},
		{"return_index", model.AttributeInt8, av.returnIdx, &p.returnIndex},
		{model.AttrNumberOfReturns, model.AttributeUint8, av.returnCount, &p.numberOfReturns},
		{"return_count", model.AttributeInt8, av.returnCount, &p.returnCount},
		{"cartesian_invalid", model.AttributeBool, av.cartInvalid, &p.cartesianInvalid},
		{"spherical_invalid", model.AttributeBool, av.sphInvalid, &p.sphericalInvalid},
		{"raw_color_red", model.AttributeUint16, av.color, &p.rawColorRed},
		{"raw_color_green", model.AttributeUint16, av.color, &p.rawColorGreen},
		{"raw_color_blue", model.AttributeUint16, av.color, &p.rawColorBlue},
		{"color_invalid", model.AttributeBool, av.colorInvalid, &p.colorInvalid},
		{"row_index", model.AttributeInt32, av.rowIdx, &p.rowIndex},
		{"column_index", model.AttributeInt32, av.colIdx, &p.columnIndex},
		{"timestamp", model.AttributeFloat64, av.timestamp, &p.timestamp},
		{"timestamp_invalid", model.AttributeBool, av.timeInvalid, &p.timestampInvalid},
		{"normal_x", model.AttributeFloat32, av.normalX, &p.normalX},
		{"normal_y", model.AttributeFloat32, av.normalY, &p.normalY},
		{"normal_z", model.AttributeFloat32, av.normalZ, &p.normalZ},
	}

	cursor := 0
	for i := range fields {
		fd := &fields[i]
		if !fd.ok {
			continue
		}
		if _, req := e.requested[fd.name]; !req {
			continue
		}
		size, _ := model.AttributeTypeSize(fd.typ)
		e.schema = append(e.schema, model.AttributeDescriptor{Name: fd.name, Type: fd.typ})
		*fd.off = cursor
		cursor += size
	}
	for _, name := range av.extrasOrder {
		typ := av.extras[name]
		size, _ := model.AttributeTypeSize(typ)
		e.schema = append(e.schema, model.AttributeDescriptor{Name: name, Type: typ})
		cursor += size
	}
	p.size = cursor
}

func markAllAbsent(p *e57EncodePlan) {
	p.intensity, p.rawIntensity, p.intensityInvalid = -1, -1, -1
	p.classification = -1
	p.returnNumber, p.returnIndex, p.numberOfReturns, p.returnCount = -1, -1, -1, -1
	p.cartesianInvalid, p.sphericalInvalid = -1, -1
	p.rawColorRed, p.rawColorGreen, p.rawColorBlue, p.colorInvalid = -1, -1, -1, -1
	p.rowIndex, p.columnIndex = -1, -1
	p.timestamp, p.timestampInvalid = -1, -1
	p.normalX, p.normalY, p.normalZ = -1, -1, -1
}

// schemaOffsets returns the packed offset and size of every schema attribute
// by canonical name.
func (e *E57Reader) schemaOffsets() map[string]e57ExtraTarget {
	out := make(map[string]e57ExtraTarget, len(e.schema))
	cursor := 0
	for _, desc := range e.schema {
		size, _ := model.AttributeTypeSize(desc.Type)
		out[desc.Name] = e57ExtraTarget{off: cursor, size: size, typ: desc.Type}
		cursor += size
	}
	return out
}

// advanceScan opens the next scan, caches per-scan normalisation parameters
// and derives the scan's encode plan (schema offsets masked by field presence).
func (e *E57Reader) advanceScan() error {
	for e.curScan < e.scanCount {
		hdr, err := e.r.ScanInfo(e.curScan)
		if err != nil {
			e.curScan++
			continue
		}
		pr, err := e.r.Points(e.curScan)
		if err != nil {
			e.curScan++
			continue
		}
		scanIdx := e.curScan
		e.curScan++
		e.pr = pr
		e.pose = libe57.NewPoseTransform(hdr.Pose)

		pf := hdr.PointFields
		e.hasCartesian = pf.CartesianXField
		e.hasCartInvalid = pf.CartesianInvalidStateField
		e.hasSpherical = pf.SphericalRangeField
		e.hasSphInvalid = pf.SphericalInvalidStateField
		e.hasColor = pf.ColorRedField
		e.hasColorInvalid = pf.IsColorInvalidField
		e.hasIntensity = pf.IntensityField
		e.hasIntInvalid = pf.IsIntensityInvalidField
		e.hasTimeInvalid = pf.IsTimeStampInvalidField

		// Mask the schema plan by this scan's field presence: masked fields
		// keep their zero values in the packed output.
		act := e.plan
		if !pf.IntensityField {
			act.intensity, act.rawIntensity = -1, -1
		}
		if !pf.IsIntensityInvalidField {
			act.intensityInvalid = -1
		}
		if !pf.ReturnIndexField {
			act.returnNumber, act.returnIndex = -1, -1
		}
		if !pf.ReturnCountField {
			act.numberOfReturns, act.returnCount = -1, -1
		}
		if !pf.CartesianInvalidStateField {
			act.cartesianInvalid = -1
		}
		if !pf.SphericalInvalidStateField {
			act.sphericalInvalid = -1
		}
		if !pf.ColorRedField {
			act.rawColorRed, act.rawColorGreen, act.rawColorBlue = -1, -1, -1
		}
		if !pf.IsColorInvalidField {
			act.colorInvalid = -1
		}
		if !pf.RowIndexField {
			act.rowIndex = -1
		}
		if !pf.ColumnIndexField {
			act.columnIndex = -1
		}
		if !pf.TimeStampField {
			act.timestamp = -1
		}
		if !pf.IsTimeStampInvalidField {
			act.timestampInvalid = -1
		}
		if !pf.NormalXField {
			act.normalX = -1
		}
		if !pf.NormalYField {
			act.normalY = -1
		}
		if !pf.NormalZField {
			act.normalZ = -1
		}

		// Resolve this scan's extension fields (and the classification extra)
		// against the schema layout, keyed by prototype spelling.
		e.scanExtras = nil
		hasClassificationExtra := false
		if e.plan.size > 0 {
			offsets := e.schemaOffsets()
			if fields, err := e.r.PointPrototype(scanIdx); err == nil {
				for _, f := range fields {
					canonical := model.CanonicalAttributeName(f.Name)
					if canonical == model.AttrClassification {
						hasClassificationExtra = true
						continue
					}
					if _, std := e57StandardSourceNames[canonical]; std {
						continue
					}
					output, ok := e.requested[canonical]
					if !ok {
						continue
					}
					target, ok := offsets[output]
					if !ok {
						continue
					}
					typ, ok := e57ExtraType(f)
					if !ok || typ != target.typ {
						continue // type differs from the schema: stays zero
					}
					if e.scanExtras == nil {
						e.scanExtras = make(map[string]e57ExtraTarget)
					}
					e.scanExtras[f.Name] = target
				}
			}
		}
		if !hasClassificationExtra {
			act.classification = -1
		}
		e.act = act

		cl := hdr.ColorLimits
		e.colorRedMin = cl.ColorRedMinimum
		e.colorRedRange = cl.ColorRedMaximum - cl.ColorRedMinimum
		e.colorGreenMin = cl.ColorGreenMinimum
		e.colorGreenRange = cl.ColorGreenMaximum - cl.ColorGreenMinimum
		e.colorBlueMin = cl.ColorBlueMinimum
		e.colorBlueRange = cl.ColorBlueMaximum - cl.ColorBlueMinimum

		il := hdr.IntensityLimits
		e.intensityMin = il.IntensityMinimum
		e.intensityRange = il.IntensityMaximum - il.IntensityMinimum

		return nil
	}
	return io.EOF
}

func (e *E57Reader) GetNext() (geom.Point64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		if e.pr == nil {
			if err := e.advanceScan(); err != nil {
				return geom.Point64{}, err
			}
		}

		pt, err := e.pr.Next()
		if err == io.EOF {
			e.pr.Close() //nolint:errcheck
			e.pr = nil
			continue
		}
		if err != nil {
			return geom.Point64{}, err
		}

		// --- Coordinates ---
		var x, y, z float64
		if e.hasCartesian && (!e.hasCartInvalid || pt.CartesianInvalid == 0) {
			x, y, z = pt.X, pt.Y, pt.Z
		} else if e.hasSpherical && (!e.hasSphInvalid || pt.SphericalInvalid == 0) {
			x, y, z = libe57.SphericalToCartesian(pt.Range, pt.Azimuth, pt.Elevation)
		} else {
			continue // no valid position; skip point
		}
		if e.pose != nil {
			x, y, z = e.pose.Apply(x, y, z)
		}

		// --- Color ---
		var r, g, b uint8
		if e.hasColor && (!e.hasColorInvalid || pt.ColorInvalid == 0) {
			r = normalizeToUint8(float64(pt.R), e.colorRedMin, e.colorRedRange)
			g = normalizeToUint8(float64(pt.G), e.colorGreenMin, e.colorGreenRange)
			b = normalizeToUint8(float64(pt.B), e.colorBlueMin, e.colorBlueRange)
		}

		return geom.Point64{
			Vector:     model.Vector{X: x, Y: y, Z: z},
			R:          r,
			G:          g,
			B:          b,
			Attributes: e.encodeAttributesLocked(&pt),
		}, nil
	}
}

func b2u8(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// encodeAttributesLocked encodes the requested attributes of the point into a
// packed AttributeValues buffer laid out per e.schema. Fields the scan does
// not carry, and per-point invalid values, keep their zero value.
// Must be called with e.mu held.
func (e *E57Reader) encodeAttributesLocked(pt *libe57.Point) model.AttributeValues {
	if e.plan.size == 0 {
		return nil
	}
	act := &e.act
	blob := e.arena.Alloc(e.plan.size)

	intensityValid := !e.hasIntInvalid || pt.IntensityInvalid == 0
	if act.intensity >= 0 && intensityValid {
		binary.LittleEndian.PutUint16(blob[act.intensity:], normalizeToUint16(pt.Intensity, e.intensityMin, e.intensityRange))
	}
	if act.rawIntensity >= 0 && intensityValid {
		binary.LittleEndian.PutUint64(blob[act.rawIntensity:], math.Float64bits(pt.Intensity))
	}
	if act.intensityInvalid >= 0 {
		blob[act.intensityInvalid] = b2u8(pt.IntensityInvalid != 0)
	}
	if act.classification >= 0 {
		// Non-standard "classification" extension consumed by the standard
		// path; out-of-range values are reported as 0.
		if v, ok := pt.Extra("classification"); ok && v >= 0 && v <= 255 {
			blob[act.classification] = uint8(v)
		}
	}
	if act.returnNumber >= 0 {
		// E57 returnIndex is 0-indexed; LAS convention (used downstream) is 1-indexed.
		blob[act.returnNumber] = uint8(min(int(pt.ReturnIndex)+1, 255))
	}
	if act.returnIndex >= 0 {
		blob[act.returnIndex] = byte(pt.ReturnIndex)
	}
	if act.numberOfReturns >= 0 {
		v := int(pt.ReturnCount)
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		blob[act.numberOfReturns] = uint8(v)
	}
	if act.returnCount >= 0 {
		blob[act.returnCount] = byte(pt.ReturnCount)
	}
	if act.cartesianInvalid >= 0 {
		blob[act.cartesianInvalid] = b2u8(pt.CartesianInvalid != 0)
	}
	if act.sphericalInvalid >= 0 {
		blob[act.sphericalInvalid] = b2u8(pt.SphericalInvalid != 0)
	}
	colorValid := !e.hasColorInvalid || pt.ColorInvalid == 0
	if act.rawColorRed >= 0 && colorValid {
		binary.LittleEndian.PutUint16(blob[act.rawColorRed:], pt.R)
	}
	if act.rawColorGreen >= 0 && colorValid {
		binary.LittleEndian.PutUint16(blob[act.rawColorGreen:], pt.G)
	}
	if act.rawColorBlue >= 0 && colorValid {
		binary.LittleEndian.PutUint16(blob[act.rawColorBlue:], pt.B)
	}
	if act.colorInvalid >= 0 {
		blob[act.colorInvalid] = b2u8(pt.ColorInvalid != 0)
	}
	if act.rowIndex >= 0 {
		binary.LittleEndian.PutUint32(blob[act.rowIndex:], uint32(pt.Row))
	}
	if act.columnIndex >= 0 {
		binary.LittleEndian.PutUint32(blob[act.columnIndex:], uint32(pt.Col))
	}
	if act.timestamp >= 0 && (!e.hasTimeInvalid || pt.TimestampInvalid == 0) {
		binary.LittleEndian.PutUint64(blob[act.timestamp:], math.Float64bits(pt.Timestamp))
	}
	if act.timestampInvalid >= 0 {
		blob[act.timestampInvalid] = b2u8(pt.TimestampInvalid != 0)
	}
	if act.normalX >= 0 {
		binary.LittleEndian.PutUint32(blob[act.normalX:], math.Float32bits(pt.NX))
	}
	if act.normalY >= 0 {
		binary.LittleEndian.PutUint32(blob[act.normalY:], math.Float32bits(pt.NY))
	}
	if act.normalZ >= 0 {
		binary.LittleEndian.PutUint32(blob[act.normalZ:], math.Float32bits(pt.NZ))
	}
	for _, extra := range pt.Extras() {
		target, ok := e.scanExtras[extra.Name]
		if !ok {
			continue
		}
		value, ok := convertExtraValue(target.typ, extra.Value)
		if !ok {
			continue
		}
		_ = model.EncodeAttributeValue(blob[target.off:target.off+target.size], target.typ, value)
	}
	return blob
}

// integerAttrType returns the narrowest attribute type covering an Integer
// prototype field's declared [min, max] range.
func integerAttrType(min, max int64) model.AttributeType {
	if min >= 0 {
		switch {
		case max <= math.MaxUint8:
			return model.AttributeUint8
		case max <= math.MaxUint16:
			return model.AttributeUint16
		case max <= math.MaxUint32:
			return model.AttributeUint32
		default:
			return model.AttributeUint64
		}
	}
	switch {
	case min >= math.MinInt8 && max <= math.MaxInt8:
		return model.AttributeInt8
	case min >= math.MinInt16 && max <= math.MaxInt16:
		return model.AttributeInt16
	case min >= math.MinInt32 && max <= math.MaxInt32:
		return model.AttributeInt32
	default:
		return model.AttributeInt64
	}
}

// convertExtraValue converts the iterator's float64 representation of an
// extra field to the plan's attribute type. Integer values are exact up to
// 2^53 (a float64 limitation of the iterator API); beyond that they are
// rounded to the nearest representable value.
func convertExtraValue(t model.AttributeType, v float64) (any, bool) {
	switch t {
	case model.AttributeUint8:
		return uint8(math.Round(v)), true
	case model.AttributeUint16:
		return uint16(math.Round(v)), true
	case model.AttributeUint32:
		return uint32(math.Round(v)), true
	case model.AttributeUint64:
		return uint64(math.Round(v)), true
	case model.AttributeInt8:
		return int8(math.Round(v)), true
	case model.AttributeInt16:
		return int16(math.Round(v)), true
	case model.AttributeInt32:
		return int32(math.Round(v)), true
	case model.AttributeInt64:
		return int64(math.Round(v)), true
	case model.AttributeFloat32:
		return float32(v), true
	case model.AttributeFloat64:
		return v, true
	default:
		return nil, false
	}
}

// normalizeToUint8 maps v from [min, min+rng] to [0, 255].
// Returns 0 for degenerate (zero) range.
func normalizeToUint8(v, min, rng float64) uint8 {
	if rng <= 0 {
		return 0
	}
	out := math.Round((v - min) / rng * 255)
	if out < 0 {
		return 0
	}
	if out > 255 {
		return 255
	}
	return uint8(out)
}

// normalizeToUint16 maps v from [min, min+rng] to [0, 65535].
// Returns 0 for degenerate (zero) range.
func normalizeToUint16(v, min, rng float64) uint16 {
	if rng <= 0 {
		return 0
	}
	out := math.Round((v - min) / rng * 65535)
	if out < 0 {
		return 0
	}
	if out > 65535 {
		return 65535
	}
	return uint16(out)
}
