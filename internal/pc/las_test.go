package pc

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// lasTestVLR is one variable length record for buildTestLasFile.
type lasTestVLR struct {
	userID   string
	recordID uint16
	data     []byte
}

// buildTestLasFile writes a minimal uncompressed LAS 1.<minor> file (minor <= 3)
// with the given point data format, record length, VLRs and raw point records,
// and returns its path.
func buildTestLasFile(t *testing.T, versionMinor uint8, pointFormat uint8, recordLen uint16, vlrs []lasTestVLR, points [][]byte) string {
	t.Helper()
	headerSize := 227
	if versionMinor == 3 {
		headerSize = 235 // adds the waveform data packet record start
	}
	offset := headerSize
	for _, v := range vlrs {
		offset += 54 + len(v.data)
	}
	h := make([]byte, headerSize)
	copy(h, "LASF")
	h[24] = 1 // version major
	h[25] = versionMinor
	binary.LittleEndian.PutUint16(h[94:], uint16(headerSize))
	binary.LittleEndian.PutUint32(h[96:], uint32(offset)) // offset to point data
	binary.LittleEndian.PutUint32(h[100:], uint32(len(vlrs)))
	h[104] = pointFormat
	binary.LittleEndian.PutUint16(h[105:], recordLen)
	binary.LittleEndian.PutUint32(h[107:], uint32(len(points))) // legacy point count
	for _, off := range []int{131, 139, 147} {                  // x/y/z scale factors
		binary.LittleEndian.PutUint64(h[off:], math.Float64bits(0.001))
	}
	buf := bytes.NewBuffer(h)
	for _, v := range vlrs {
		vh := make([]byte, 54)
		copy(vh[2:18], v.userID)
		binary.LittleEndian.PutUint16(vh[18:], v.recordID)
		binary.LittleEndian.PutUint16(vh[20:], uint16(len(v.data)))
		buf.Write(vh)
		buf.Write(v.data)
	}
	for i, p := range points {
		if len(p) != int(recordLen) {
			t.Fatalf("point %d: record is %d bytes, record length says %d", i, len(p), recordLen)
		}
		buf.Write(p)
	}
	path := filepath.Join(t.TempDir(), "generated.las")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// encodeTestPointPF0 builds the 20-byte core of a point data format 0 record
// (single return, all flags zero).
func encodeTestPointPF0(x, y, z int32) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b[0:], uint32(x))
	binary.LittleEndian.PutUint32(b[4:], uint32(y))
	binary.LittleEndian.PutUint32(b[8:], uint32(z))
	b[14] = 0x09 // return number 1 of 1
	return b
}

// encodeTestPointPF4 builds a 57-byte point data format 4 record.
func encodeTestPointPF4(x, y, z int32, gps float64, waveIdx uint8, waveOffset uint64, packetSize uint32, returnLoc, xt, yt, zt float32) []byte {
	b := make([]byte, 57)
	copy(b, encodeTestPointPF0(x, y, z))
	binary.LittleEndian.PutUint64(b[20:], math.Float64bits(gps))
	b[28] = waveIdx
	binary.LittleEndian.PutUint64(b[29:], waveOffset)
	binary.LittleEndian.PutUint32(b[37:], packetSize)
	binary.LittleEndian.PutUint32(b[41:], math.Float32bits(returnLoc))
	binary.LittleEndian.PutUint32(b[45:], math.Float32bits(xt))
	binary.LittleEndian.PutUint32(b[49:], math.Float32bits(yt))
	binary.LittleEndian.PutUint32(b[53:], math.Float32bits(zt))
	return b
}

// extraByteDescriptorRecord builds one 192-byte LASF_Spec record-4 descriptor.
// scale/offset are optional; non-nil values set the corresponding option bits.
func extraByteDescriptorRecord(name string, dataType uint8, scale, offset *float64) []byte {
	b := make([]byte, 192)
	b[2] = dataType
	var opts uint8
	if scale != nil {
		opts |= 1 << 3
		binary.LittleEndian.PutUint64(b[112:], math.Float64bits(*scale))
	}
	if offset != nil {
		opts |= 1 << 4
		binary.LittleEndian.PutUint64(b[136:], math.Float64bits(*offset))
	}
	b[3] = opts
	copy(b[4:36], name)
	return b
}

func TestLasRawExtraFloat64(t *testing.T) {
	le := func(v uint64, size int) []byte {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v)
		return b[:size]
	}
	cases := []struct {
		typ  model.AttributeType
		src  []byte
		want float64
	}{
		{model.AttributeUint8, []byte{200}, 200},
		{model.AttributeInt8, []byte{0xFB}, -5}, // int8(-5)
		{model.AttributeUint16, le(60000, 2), 60000},
		{model.AttributeInt16, le(uint64(uint16(0x10000-30000)), 2), -30000},
		{model.AttributeUint32, le(4000000000, 4), 4000000000},
		{model.AttributeInt32, le(uint64(uint32(1<<32-2000000000)), 4), -2000000000},
		{model.AttributeUint64, le(1<<40, 8), 1 << 40},
		{model.AttributeInt64, le(uint64(1<<64 - 1<<40), 8), -(1 << 40)},
		{model.AttributeFloat32, le(uint64(math.Float32bits(1.5)), 4), 1.5},
		{model.AttributeFloat64, le(math.Float64bits(-2.25), 8), -2.25},
		{model.AttributeBool, []byte{1}, 0}, // unsupported raw type: zero
	}
	for _, c := range cases {
		if got := lasRawExtraFloat64(c.src, c.typ); got != c.want {
			t.Errorf("lasRawExtraFloat64(%v, %q): got %v want %v", c.src, c.typ, got, c.want)
		}
	}
}

func TestBuildRequestedMapAliases(t *testing.T) {
	cases := []struct {
		name      string
		requested []string
		want      map[string]string
	}{
		{
			name:      "plain names map to themselves",
			requested: []string{"Intensity", "gps_time"},
			want: map[string]string{
				"intensity": "intensity",
				"gps_time":  "gps_time",
			},
		},
		{
			name:      "amplitude and reflectance match via canonicalization alone",
			requested: []string{"Amplitude", "Reflectance"},
			want: map[string]string{
				"amplitude":   "amplitude",
				"reflectance": "reflectance",
			},
		},
		{
			name:      "incidence angle matches all vendor spellings",
			requested: []string{"incidence_angle"},
			want: map[string]string{
				"incidence_angle":        "incidence_angle",
				"incidenceangle":         "incidence_angle",
				"trueviewincidenceangle": "incidence_angle",
				"_incidenceangle":        "incidence_angle",
			},
		},
		{
			name:      "ASPRS spelling of incidence angle also expands",
			requested: []string{"Incidence Angle"},
			want: map[string]string{
				"incidenceangle":         "incidenceangle",
				"trueviewincidenceangle": "incidenceangle",
				"_incidenceangle":        "incidenceangle",
			},
		},
		{
			name:      "pulse width matches echo width and echo length spellings",
			requested: []string{"pulse_width"},
			want: map[string]string{
				"pulse_width": "pulse_width",
				"pulsewidth":  "pulse_width",
				"echowidth":   "pulse_width",
				"echolength":  "pulse_width",
			},
		},
		{
			name:      "colliding requests: earlier request wins the shared variants",
			requested: []string{"pulse_width", "echo_width"},
			want: map[string]string{
				"pulse_width": "pulse_width",
				"pulsewidth":  "pulse_width",
				"echowidth":   "pulse_width",
				"echolength":  "pulse_width",
				"echo_width":  "echo_width",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildRequestedMap(model.NewAttributes(c.requested...))
			if len(got) != len(c.want) {
				t.Fatalf("map size: got %d (%v) want %d (%v)", len(got), got, len(c.want), c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("requested[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// embeddedLasFile materializes the named embedded fixture into a temp dir and
// returns its path. Test binaries may run outside the package directory (the
// cross-target CI does), so fixtures must not be opened via relative paths.
func embeddedLasFile(t *testing.T, name string) string {
	t.Helper()
	files, _ := writeEmbeddedLasFiles(t)
	for _, f := range files {
		if filepath.Base(f) == name {
			return f
		}
	}
	t.Fatalf("embedded fixture %q not found", name)
	return ""
}

// TestGoLasReaderEmitsRequestedAttributes reads a real LAS fixture and checks
// that requested standard attributes are emitted with the right names/types.
func TestGoLasReaderEmitsRequestedAttributes(t *testing.T) {
	r, err := NewGoLasReader(embeddedLasFile(t, "las-12-pf1.las"), "EPSG:32633", false,
		model.NewAttributes("Intensity", "classification", "gps_time", "scan_angle", "point_source_id", "not_present_attr"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer r.Close()

	pt, err := r.GetNext()
	if err != nil {
		t.Fatalf("GetNext: %v", err)
	}
	want := map[string]model.AttributeType{
		"intensity":       model.AttributeUint16,
		"classification":  model.AttributeUint8,
		"gps_time":        model.AttributeFloat64,
		"scan_angle":      model.AttributeFloat64,
		"point_source_id": model.AttributeUint16,
	}
	schema := r.AttributeSchema()
	got := map[string]model.AttributeType{}
	for _, desc := range schema {
		got[desc.Name] = desc.Type
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("attribute %q: got type %q want %q (schema: %v)", name, got[name], typ, schema)
		}
	}
	if len(got) != len(want) {
		t.Errorf("expected %d schema attributes, got %d: %v", len(want), len(got), schema)
	}
	entries, size, err := model.AttributeSchemaLayout(schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	if len(pt.Attributes) != size {
		t.Fatalf("packed values are %d bytes, schema layout expects %d", len(pt.Attributes), size)
	}
	view := model.NewAttributeView(entries, pt.Attributes)
	for i := 0; i < view.Len(); i++ {
		if v, err := view.Value(i); err != nil || v == nil {
			t.Errorf("attribute %q: decode failed (%v, %v)", view.Name(i), v, err)
		}
	}
}

// TestGoLasReaderWaveformSchemaRegression locks in the waveform attribute
// schema for a waveform point format: requesting the waveform attributes must
// yield exactly one waveform_packet_size (uint32) and exactly one
// return_point_waveform_location (float32) descriptor, with the right values
// decoded. Regression test for a copy-paste bug where the packet size schema
// row read return_point_waveform_location, duplicating the next row.
func TestGoLasReaderWaveformSchemaRegression(t *testing.T) {
	pt := encodeTestPointPF4(1000, 2000, 3000, 42.5, 7, 99, 12345, 1.5, 0.5, 0.25, -0.75)
	file := buildTestLasFile(t, 3, 4, 57, nil, [][]byte{pt})

	r, err := NewGoLasReader(file, "EPSG:32633", false,
		model.NewAttributes(model.AttrWaveformPacketSize, model.AttrReturnPointWaveformLocation))
	if err != nil {
		t.Fatalf("open generated PF4 file: %v", err)
	}
	defer r.Close()

	schema := r.AttributeSchema()
	counts := map[string]int{}
	types := map[string]model.AttributeType{}
	for _, desc := range schema {
		counts[desc.Name]++
		types[desc.Name] = desc.Type
	}
	if counts[model.AttrWaveformPacketSize] != 1 || types[model.AttrWaveformPacketSize] != model.AttributeUint32 {
		t.Fatalf("expected exactly one %q descriptor of type uint32, schema: %v", model.AttrWaveformPacketSize, schema)
	}
	if counts[model.AttrReturnPointWaveformLocation] != 1 || types[model.AttrReturnPointWaveformLocation] != model.AttributeFloat32 {
		t.Fatalf("expected exactly one %q descriptor of type float32, schema: %v", model.AttrReturnPointWaveformLocation, schema)
	}
	if len(schema) != 2 {
		t.Fatalf("expected exactly 2 schema descriptors, got %d: %v", len(schema), schema)
	}

	p, err := r.GetNext()
	if err != nil {
		t.Fatalf("GetNext: %v", err)
	}
	entries, _, err := model.AttributeSchemaLayout(schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	view := model.NewAttributeView(entries, p.Attributes)
	if v, err := view.Uint32(view.Index(model.AttrWaveformPacketSize)); err != nil || v != 12345 {
		t.Errorf("waveform packet size: got (%v, %v), want 12345", v, err)
	}
	if v, err := view.Float32(view.Index(model.AttrReturnPointWaveformLocation)); err != nil || v != 1.5 {
		t.Errorf("return point waveform location: got (%v, %v), want 1.5", v, err)
	}
}

// TestGoLasReaderExtraBytesValues reads a generated LAS file carrying extra
// bytes and checks the emitted attribute types and values end-to-end,
// including a scaled descriptor (raw*scale+offset emitted as float64).
func TestGoLasReaderExtraBytesValues(t *testing.T) {
	scale, offset := 0.01, 100.0
	vlr := lasTestVLR{
		userID:   "LASF_Spec",
		recordID: 4,
		data: bytes.Join([][]byte{
			extraByteDescriptorRecord("temperature", 3, nil, nil),          // uint16
			extraByteDescriptorRecord("reflectance", 9, nil, nil),          // float32
			extraByteDescriptorRecord("calibrated", 6, &scale, &offset),    // int32, scaled
			extraByteDescriptorRecord("signal_deviation", 2, nil, nil),     // int8
		}, nil),
	}
	// extra bytes payload: uint16(512) | float32(-1.25) | int32(-2000) | int8(-7)
	extra := make([]byte, 11)
	binary.LittleEndian.PutUint16(extra[0:], 512)
	binary.LittleEndian.PutUint32(extra[2:], math.Float32bits(-1.25))
	var calibratedRaw int32 = -2000
	binary.LittleEndian.PutUint32(extra[6:], uint32(calibratedRaw))
	var deviationRaw int8 = -7
	extra[10] = byte(deviationRaw)
	pt := append(encodeTestPointPF0(1000, 2000, 3000), extra...)
	file := buildTestLasFile(t, 2, 0, uint16(len(pt)), []lasTestVLR{vlr}, [][]byte{pt})

	r, err := NewGoLasReader(file, "EPSG:32633", false,
		model.NewAttributes("temperature", "reflectance", "calibrated", "signal_deviation"))
	if err != nil {
		t.Fatalf("open generated extra-bytes file: %v", err)
	}
	defer r.Close()

	wantTypes := map[string]model.AttributeType{
		"temperature":      model.AttributeUint16,
		"reflectance":      model.AttributeFloat32,
		"calibrated":       model.AttributeFloat64, // scaled values are emitted as float64
		"signal_deviation": model.AttributeInt8,
	}
	schema := r.AttributeSchema()
	if len(schema) != len(wantTypes) {
		t.Fatalf("expected %d schema descriptors, got %v", len(wantTypes), schema)
	}
	for _, desc := range schema {
		if wantTypes[desc.Name] != desc.Type {
			t.Errorf("attribute %q: got type %q want %q", desc.Name, desc.Type, wantTypes[desc.Name])
		}
	}

	p, err := r.GetNext()
	if err != nil {
		t.Fatalf("GetNext: %v", err)
	}
	entries, _, err := model.AttributeSchemaLayout(schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	view := model.NewAttributeView(entries, p.Attributes)
	if v, err := view.Uint16(view.Index("temperature")); err != nil || v != 512 {
		t.Errorf("temperature: got (%v, %v), want 512", v, err)
	}
	if v, err := view.Float32(view.Index("reflectance")); err != nil || v != -1.25 {
		t.Errorf("reflectance: got (%v, %v), want -1.25", v, err)
	}
	if v, err := view.Float64(view.Index("calibrated")); err != nil || v != -2000*0.01+100 {
		t.Errorf("calibrated: got (%v, %v), want %v", v, err, -2000*0.01+100)
	}
	if v, err := view.Int8(view.Index("signal_deviation")); err != nil || v != -7 {
		t.Errorf("signal_deviation: got (%v, %v), want -7", v, err)
	}
}

// TestGoLasReaderNoAttributesRequested ensures the reader emits nothing when
// no attributes are requested.
func TestGoLasReaderNoAttributesRequested(t *testing.T) {
	r, err := NewGoLasReader(embeddedLasFile(t, "las-12-pf1.las"), "EPSG:32633", false, nil)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer r.Close()
	pt, err := r.GetNext()
	if err != nil {
		t.Fatalf("GetNext: %v", err)
	}
	if pt.Attributes != nil {
		t.Errorf("expected no attributes, got %v", pt.Attributes)
	}
}
