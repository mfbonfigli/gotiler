package model

import (
	"strings"
	"unicode"
)

// Standard optional per-point attribute names.
// These are modeled on the common LAS/LAZ file attributes
const (
	AttrIntensity                   = "intensity"
	AttrClassification              = "classification"
	AttrReturnNumber                = "return_number"
	AttrNumberOfReturns             = "number_of_returns"
	AttrScanDirectionFlag           = "scan_direction_flag"
	AttrEdgeOfFlightLine            = "edge_of_flight_line"
	AttrClassificationFlags         = "classification_flags"
	AttrSynthetic                   = "synthetic"
	AttrKeyPoint                    = "key_point"
	AttrWithheld                    = "withheld"
	AttrOverlap                     = "overlap"
	AttrUserData                    = "user_data"
	AttrPointSourceId               = "point_source_id"
	AttrScanAngle                   = "scan_angle"
	AttrGpsTime                     = "gps_time"
	AttrNir                         = "nir"
	AttrScannerChannel              = "scanner_channel"
	AttrWavePacketDescriptorIndex   = "wave_packet_descriptor_index"
	AttrWaveformDataOffset          = "waveform_data_offset"
	AttrWaveformPacketSize          = "waveform_packet_size"
	AttrReturnPointWaveformLocation = "return_point_waveform_location"
	AttrWaveformXT                  = "waveform_x_t"
	AttrWaveformYT                  = "waveform_y_t"
	AttrWaveformZT                  = "waveform_z_t"
)

// AttributeType identifies the scalar data type of a per-point attribute.
type AttributeType string

const (
	AttributeInt8    AttributeType = "int8"
	AttributeUint8   AttributeType = "uint8"
	AttributeInt16   AttributeType = "int16"
	AttributeUint16  AttributeType = "uint16"
	AttributeInt32   AttributeType = "int32"
	AttributeUint32  AttributeType = "uint32"
	AttributeInt64   AttributeType = "int64"
	AttributeUint64  AttributeType = "uint64"
	AttributeBool    AttributeType = "bool"
	AttributeFloat32 AttributeType = "float32"
	AttributeFloat64 AttributeType = "float64"
)

// AttributeSummary describes one requested attribute that the tree stored.
// SkipIncomplete is true when at least one point was missing the attribute, so
// encoders should omit it.
type AttributeSummary struct {
	RequestedName  string
	Name           string
	Type           AttributeType
	SkipIncomplete bool
	Min            any
	Max            any
}

// Attributes is an ordered list of optional per-point attribute names to include in output tiles.
// The zero value (nil slice) means no optional attributes are exported.
// Use DefaultAttributes() to get the default set with all supported attributes enabled.
type Attributes []string

// NewAttributes creates an Attributes set containing the given names.
func NewAttributes(names ...string) Attributes {
	a := make(Attributes, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		name := CanonicalAttributeName(n)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		a = append(a, name)
	}
	return a
}

// DefaultAttributes returns an Attributes set with all currently supported optional attributes.
func DefaultAttributes() Attributes {
	return NewAttributes(AttrIntensity, AttrClassification)
}

// Has reports whether the named attribute is in the set.
func (a Attributes) Has(name string) bool {
	name = CanonicalAttributeName(name)
	for _, attr := range a {
		if CanonicalAttributeName(attr) == name {
			return true
		}
	}
	return false
}

// Names returns a copy of the ordered attribute names.
func (a Attributes) Names() []string {
	out := make([]string, len(a))
	copy(out, a)
	return out
}

// ParseAttributes converts a slice of attribute name strings into an Attributes set.
// A value of "none" returns an empty set.
func ParseAttributes(attrs []string) (Attributes, error) {
	for _, a := range attrs {
		if strings.TrimSpace(strings.ToLower(a)) == "none" {
			return Attributes{}, nil
		}
	}
	return NewAttributes(attrs...), nil
}

// CanonicalAttributeName normalizes an attribute name for tolerant matching.
// Whitespace is removed and casing is folded.
func CanonicalAttributeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// AttributeTypeSize returns the byte size used to serialize one scalar value.
func AttributeTypeSize(t AttributeType) (int, bool) {
	switch t {
	case AttributeInt8, AttributeUint8, AttributeBool:
		return 1, true
	case AttributeInt16, AttributeUint16:
		return 2, true
	case AttributeInt32, AttributeUint32, AttributeFloat32:
		return 4, true
	case AttributeInt64, AttributeUint64, AttributeFloat64:
		return 8, true
	default:
		return 0, false
	}
}

// AttributeLayoutEntry describes where one attribute value lives inside a
// point's packed AttributeValues.
type AttributeLayoutEntry struct {
	Name   string
	Type   AttributeType
	Offset int
	Size   int
	// SourceIndex is the index of the originating entry in the list the
	// layout was computed from (attribute summaries or a reader schema).
	SourceIndex int
}

// AttributeLayout computes the packed value layout for the given summaries.
// Summaries whose type has no defined size (e.g. requested attributes that were
// never resolved against the source data) are excluded. The returned total is
// the byte size of one point's packed AttributeValues.
func AttributeLayout(summaries []AttributeSummary) (entries []AttributeLayoutEntry, total int) {
	for i, summary := range summaries {
		size, ok := AttributeTypeSize(summary.Type)
		if !ok {
			continue
		}
		entries = append(entries, AttributeLayoutEntry{
			Name:        summary.Name,
			Type:        summary.Type,
			Offset:      total,
			Size:        size,
			SourceIndex: i,
		})
		total += size
	}
	return entries, total
}
