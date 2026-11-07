package ply

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

const vertexElementName = "vertex"

var errDataEndsEarly = errors.New("data ends early")

// vertexDecoder reads the records of the vertex element. Every scalar value
// is returned as float64, which represents all PLY scalar types exactly.
type vertexDecoder struct {
	r         *bufio.Reader
	format    format
	byteOrder binary.ByteOrder

	properties        []property
	hasListProperties bool
	count             int
	index             int

	// scalars lists the scalar vertex properties in declaration order: the
	// slice returned by next holds the value of scalars[i] at index i.
	scalars []property
	// vertexDataOffset is the file offset of the first vertex record.
	vertexDataOffset int64

	values        []float64
	record        []byte
	recordSize    int
	scalarOffsets []int
	fields        [][]byte
}

// newVertexDecoder skips the data of the elements declared before the vertex
// element, leaving r at the first vertex record. r must be positioned right
// after the header, as readHeader leaves it.
func newVertexDecoder(r *bufio.Reader, h header) (*vertexDecoder, error) {
	vertexIndex := h.elementIndex(vertexElementName)
	if vertexIndex < 0 {
		return nil, errors.New("PLY file has no vertex element")
	}
	vertex := h.elements[vertexIndex]
	d := &vertexDecoder{
		r:          r,
		format:     h.format,
		properties: vertex.properties,
		count:      vertex.count,
	}
	switch h.format {
	case formatBinaryLittleEndian:
		d.byteOrder = binary.LittleEndian
	case formatBinaryBigEndian:
		d.byteOrder = binary.BigEndian
	}
	for _, p := range vertex.properties {
		if p.isList() {
			d.hasListProperties = true
			continue
		}
		d.scalars = append(d.scalars, p)
		d.scalarOffsets = append(d.scalarOffsets, d.recordSize)
		d.recordSize += p.valueType.size()
	}
	d.values = make([]float64, len(d.scalars))
	d.record = make([]byte, max(d.recordSize, float64Type.size()))

	skipped, err := d.skipElements(h.elements[:vertexIndex])
	if err != nil {
		return nil, err
	}
	d.vertexDataOffset = h.dataOffset + skipped
	return d, nil
}

// next returns the scalar values of the next vertex, or io.EOF once all the
// vertices declared in the header have been read. The returned slice is
// reused by the following call.
func (d *vertexDecoder) next() ([]float64, error) {
	if d.index >= d.count {
		return nil, io.EOF
	}
	var err error
	switch {
	case d.format == formatASCII:
		err = d.readASCIIRecord()
	case d.hasListProperties:
		_, err = d.readBinaryRecord(d.properties, d.values)
	default:
		err = d.readFixedBinaryRecord()
	}
	if err != nil {
		err = normalizeEOF(err)
		if errors.Is(err, errDataEndsEarly) {
			return nil, fmt.Errorf("PLY vertex %d: %w, the header declares %d vertices", d.index, err, d.count)
		}
		return nil, fmt.Errorf("PLY vertex %d: %w", d.index, err)
	}
	d.index++
	return d.values, nil
}

// restart rewinds the decoder to the first vertex. r must be positioned at
// vertexDataOffset.
func (d *vertexDecoder) restart(r *bufio.Reader) {
	d.r = r
	d.index = 0
}

func (d *vertexDecoder) skipElements(elements []element) (int64, error) {
	var skipped int64
	for _, e := range elements {
		var n int64
		var err error
		if d.format == formatASCII {
			n, err = skipLines(d.r, e.count)
		} else {
			n, err = d.skipBinaryElement(e)
		}
		skipped += n
		if err != nil {
			return skipped, fmt.Errorf("PLY element %q: %w", e.name, normalizeEOF(err))
		}
	}
	return skipped, nil
}

func (d *vertexDecoder) skipBinaryElement(e element) (int64, error) {
	recordSize := 0
	for _, p := range e.properties {
		if p.isList() {
			return d.skipBinaryRecords(e)
		}
		recordSize += p.valueType.size()
	}
	if recordSize > 0 && e.count > math.MaxInt64/recordSize {
		return 0, errors.New("element data is too large")
	}
	n := int64(e.count) * int64(recordSize)
	return n, discard(d.r, n)
}

func (d *vertexDecoder) skipBinaryRecords(e element) (int64, error) {
	var skipped int64
	for range e.count {
		n, err := d.readBinaryRecord(e.properties, nil)
		skipped += n
		if err != nil {
			return skipped, err
		}
	}
	return skipped, nil
}

func (d *vertexDecoder) readFixedBinaryRecord() error {
	record := d.record[:d.recordSize]
	if _, err := io.ReadFull(d.r, record); err != nil {
		return err
	}
	for i, p := range d.scalars {
		d.values[i] = decodeScalar(record[d.scalarOffsets[i]:], p.valueType, d.byteOrder)
	}
	return nil
}

// readBinaryRecord reads one record property by property, skipping list
// values. Scalar values are stored in values, in order, or skipped when values
// is nil. It returns the number of bytes consumed.
func (d *vertexDecoder) readBinaryRecord(properties []property, values []float64) (int64, error) {
	var consumed int64
	slot := 0
	for _, p := range properties {
		if p.isList() {
			length, err := d.readBinaryScalar(p.countType)
			if err != nil {
				return consumed, err
			}
			consumed += int64(p.countType.size())
			if length < 0 {
				return consumed, fmt.Errorf("property %q: negative list length %v", p.name, length)
			}
			listSize := int64(length) * int64(p.valueType.size())
			if err := discard(d.r, listSize); err != nil {
				return consumed, err
			}
			consumed += listSize
			continue
		}
		size := p.valueType.size()
		if values == nil {
			if err := discard(d.r, int64(size)); err != nil {
				return consumed, err
			}
		} else {
			v, err := d.readBinaryScalar(p.valueType)
			if err != nil {
				return consumed, err
			}
			values[slot] = v
			slot++
		}
		consumed += int64(size)
	}
	return consumed, nil
}

func (d *vertexDecoder) readBinaryScalar(t scalarType) (float64, error) {
	buf := d.record[:t.size()]
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return 0, err
	}
	return decodeScalar(buf, t, d.byteOrder), nil
}

func decodeScalar(b []byte, t scalarType, order binary.ByteOrder) float64 {
	switch t {
	case int8Type:
		return float64(int8(b[0]))
	case uint8Type:
		return float64(b[0])
	case int16Type:
		return float64(int16(order.Uint16(b)))
	case uint16Type:
		return float64(order.Uint16(b))
	case int32Type:
		return float64(int32(order.Uint32(b)))
	case uint32Type:
		return float64(order.Uint32(b))
	case float32Type:
		return float64(math.Float32frombits(order.Uint32(b)))
	case float64Type:
		return math.Float64frombits(order.Uint64(b))
	default:
		return 0
	}
}

func (d *vertexDecoder) readASCIIRecord() error {
	line, err := d.readASCIILine()
	if err != nil {
		return err
	}
	d.fields = appendFields(d.fields[:0], line)
	if !d.hasListProperties && len(d.fields) != len(d.properties) {
		return fmt.Errorf("found %d values, expected %d", len(d.fields), len(d.properties))
	}
	next, slot := 0, 0
	for _, p := range d.properties {
		if next >= len(d.fields) {
			return fmt.Errorf("found %d values, too few for the declared properties", len(d.fields))
		}
		field := d.fields[next]
		next++
		if p.isList() {
			length, err := strconv.Atoi(string(field))
			if err != nil || length < 0 {
				return fmt.Errorf("property %q: invalid list length %q", p.name, field)
			}
			if length > len(d.fields)-next {
				return fmt.Errorf("property %q: list length %d exceeds the values on the line", p.name, length)
			}
			next += length
			continue
		}
		v, err := strconv.ParseFloat(string(field), 64)
		if err != nil {
			return fmt.Errorf("property %q: invalid number %q", p.name, field)
		}
		d.values[slot] = v
		slot++
	}
	if next != len(d.fields) {
		return fmt.Errorf("found %d values, expected %d", len(d.fields), next)
	}
	return nil
}

// readASCIILine returns the next non-blank line. A record line must fit in the
// reader's buffer.
func (d *vertexDecoder) readASCIILine() ([]byte, error) {
	for {
		line, err := d.r.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			return nil, errors.New("line too long")
		case len(line) == 0 && err != nil:
			return nil, err
		case err != nil && !errors.Is(err, io.EOF):
			return nil, err
		}
		if !isBlank(line) {
			return line, nil
		}
	}
}

// skipLines skips count non-blank lines of any length and returns the number
// of bytes consumed.
func skipLines(r *bufio.Reader, count int) (int64, error) {
	var skipped int64
	lineHasContent := false
	for remaining := count; remaining > 0; {
		chunk, err := r.ReadSlice('\n')
		skipped += int64(len(chunk))
		if len(chunk) == 0 && err != nil {
			return skipped, err
		}
		lineHasContent = lineHasContent || !isBlank(chunk)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return skipped, err
		}
		if lineHasContent {
			remaining--
		}
		lineHasContent = false
	}
	return skipped, nil
}

func discard(r *bufio.Reader, n int64) error {
	const maxStep = 1 << 30
	for n > 0 {
		skipped, err := r.Discard(int(min(n, maxStep)))
		n -= int64(skipped)
		if err != nil {
			return err
		}
	}
	return nil
}

func normalizeEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errDataEndsEarly
	}
	return err
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f'
}

func isBlank(line []byte) bool {
	for _, c := range line {
		if !isSpace(c) {
			return false
		}
	}
	return true
}

func appendFields(dst [][]byte, line []byte) [][]byte {
	start := -1
	for i, c := range line {
		switch {
		case isSpace(c) && start >= 0:
			dst = append(dst, line[start:i])
			start = -1
		case !isSpace(c) && start < 0:
			start = i
		}
	}
	if start >= 0 {
		dst = append(dst, line[start:])
	}
	return dst
}
