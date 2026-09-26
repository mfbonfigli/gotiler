package kd

import (
	"encoding/binary"
	"math"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// initializeAttributeSummaries resolves the requested attributes against the
// reader's declared schema. Summaries are laid out in SCHEMA order so a
// point's reader-provided packed values are byte-identical to the tree's
// storage layout and flow through the pipeline without re-encoding. Requested
// attributes absent from the schema are appended with an empty type and
// SkipIncomplete=true: they take no room in the packed layout and encoders
// omit them.
func initializeAttributeSummaries(requested model.Attributes, schema []model.AttributeDescriptor) []model.AttributeSummary {
	if len(requested) == 0 {
		return nil
	}
	inSchema := make(map[string]struct{}, len(schema))
	summaries := make([]model.AttributeSummary, 0, len(requested))
	for _, desc := range schema {
		inSchema[desc.Name] = struct{}{}
		summaries = append(summaries, model.AttributeSummary{
			RequestedName: desc.Name,
			Name:          desc.Name,
			Type:          desc.Type,
		})
	}
	for _, req := range requested {
		canonical := model.CanonicalAttributeName(req)
		if _, ok := inSchema[canonical]; ok {
			continue
		}
		summaries = append(summaries, model.AttributeSummary{
			RequestedName:  req,
			Name:           canonical,
			SkipIncomplete: true,
		})
	}
	return summaries
}

func markNonOutputAttributeSummaries(summaries []model.AttributeSummary, output model.Attributes) {
	for i := range summaries {
		if output.Has(summaries[i].Name) || output.Has(summaries[i].RequestedName) {
			continue
		}
		summaries[i].SkipIncomplete = true
	}
}

// attrScalar is a running min or max for one attribute, held unboxed.
type attrScalar struct {
	set bool
	i   int64
	u   uint64
	f   float64
	b   bool
}

// attrStats tracks per-attribute min/max over packed attribute values without
// boxing per-point values. It is not safe for concurrent use; observe from a
// single goroutine and call apply once done.
type attrStats struct {
	entries []model.AttributeLayoutEntry
	mins    []attrScalar
	maxs    []attrScalar
}

func newAttrStats(entries []model.AttributeLayoutEntry) *attrStats {
	return &attrStats{
		entries: entries,
		mins:    make([]attrScalar, len(entries)),
		maxs:    make([]attrScalar, len(entries)),
	}
}

func (s *attrStats) observe(values model.AttributeValues) {
	if len(values) == 0 {
		return
	}
	for i := range s.entries {
		e := &s.entries[i]
		raw := values[e.Offset : e.Offset+e.Size]
		mn, mx := &s.mins[i], &s.maxs[i]
		switch e.Type {
		case model.AttributeInt8, model.AttributeInt16, model.AttributeInt32, model.AttributeInt64:
			v := decodeAttrInt(raw, e.Type)
			if !mn.set || v < mn.i {
				mn.i, mn.set = v, true
			}
			if !mx.set || v > mx.i {
				mx.i, mx.set = v, true
			}
		case model.AttributeUint8, model.AttributeUint16, model.AttributeUint32, model.AttributeUint64:
			v := decodeAttrUint(raw, e.Type)
			if !mn.set || v < mn.u {
				mn.u, mn.set = v, true
			}
			if !mx.set || v > mx.u {
				mx.u, mx.set = v, true
			}
		case model.AttributeFloat32, model.AttributeFloat64:
			v := decodeAttrFloat(raw, e.Type)
			if !mn.set || v < mn.f {
				mn.f, mn.set = v, true
			}
			if !mx.set || v > mx.f {
				mx.f, mx.set = v, true
			}
		case model.AttributeBool:
			v := raw[0] != 0
			if !mn.set || (!v && mn.b) {
				mn.b, mn.set = v, true
			}
			if !mx.set || (v && !mx.b) {
				mx.b, mx.set = v, true
			}
		}
	}
}

// apply boxes the final min/max values into the matching summaries.
func (s *attrStats) apply(summaries []model.AttributeSummary) {
	for i := range s.entries {
		e := &s.entries[i]
		if !s.mins[i].set {
			continue
		}
		summaries[e.SourceIndex].Min = boxAttrScalar(e.Type, s.mins[i])
		summaries[e.SourceIndex].Max = boxAttrScalar(e.Type, s.maxs[i])
	}
}

func decodeAttrInt(raw []byte, t model.AttributeType) int64 {
	switch t {
	case model.AttributeInt8:
		return int64(int8(raw[0]))
	case model.AttributeInt16:
		return int64(int16(binary.LittleEndian.Uint16(raw)))
	case model.AttributeInt32:
		return int64(int32(binary.LittleEndian.Uint32(raw)))
	default:
		return int64(binary.LittleEndian.Uint64(raw))
	}
}

func decodeAttrUint(raw []byte, t model.AttributeType) uint64 {
	switch t {
	case model.AttributeUint8:
		return uint64(raw[0])
	case model.AttributeUint16:
		return uint64(binary.LittleEndian.Uint16(raw))
	case model.AttributeUint32:
		return uint64(binary.LittleEndian.Uint32(raw))
	default:
		return binary.LittleEndian.Uint64(raw)
	}
}

func decodeAttrFloat(raw []byte, t model.AttributeType) float64 {
	if t == model.AttributeFloat32 {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(raw)))
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw))
}

func boxAttrScalar(t model.AttributeType, v attrScalar) any {
	switch t {
	case model.AttributeInt8:
		return int8(v.i)
	case model.AttributeInt16:
		return int16(v.i)
	case model.AttributeInt32:
		return int32(v.i)
	case model.AttributeInt64:
		return v.i
	case model.AttributeUint8:
		return uint8(v.u)
	case model.AttributeUint16:
		return uint16(v.u)
	case model.AttributeUint32:
		return uint32(v.u)
	case model.AttributeUint64:
		return v.u
	case model.AttributeFloat32:
		return float32(v.f)
	case model.AttributeFloat64:
		return v.f
	case model.AttributeBool:
		return v.b
	default:
		return nil
	}
}
