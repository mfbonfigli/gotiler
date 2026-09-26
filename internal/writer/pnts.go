package writer

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/internal/utils"
	"github.com/mfbonfigli/gotiler/v3/tiler/encoding"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/mfbonfigli/gotiler/v3/version"
)

// heuristic to preallocate buffers capacity
const maxPointsPerPntsTileHint = 100_000

// size of write buffer
const bufferSize = 256 * 1024

// totalBytesPerPoint = 12 (xyz) + 3 (rgb) + 2 (intensity) + 1 (classification) = 18
const totalBytesPerPoint = 18
const pntsHeaderLen = 28
const pntsAlignment = 8

// pntsBufPool provides reusable byte buffers for building pnts binary content.
var pntsBufPool = utils.NewSlicePool[byte](maxPointsPerPntsTileHint * totalBytesPerPoint)

// PntsEncoder writes a node data as Pnts file (3D Tiles 1.0 specs)
type PntsEncoder struct {
	filename string
}

func (e *PntsEncoder) TilesetVersion() version.TilesetVersion {
	return version.TilesetVersion_1_0
}

func (e *PntsEncoder) ContentFilename() string {
	return e.filename
}

func NewPntsEncoder(filename string, attrs model.Attributes) *PntsEncoder {
	return &PntsEncoder{filename: filename}
}

func (e *PntsEncoder) Write(node tree.Node, wp plugin.WriterProvider, prefix string) error {
	return e.writeGeneric(node, wp, prefix, encoding.AttributeColumns(node, encoding.PntsSupportsType))
}

func (e *PntsEncoder) generateFeatureTable(numPoints int) ([]byte, int) {
	featureTableStr := e.generateFeatureTableJsonContent(numPoints)
	featureTableBytes := padJSONToAlignment([]byte(featureTableStr), pntsHeaderLen)
	return featureTableBytes, len(featureTableBytes)
}

func (e *PntsEncoder) writePntsHeader(numPoints, featureTableLen, batchTableLen, batchBodyLen int, wr io.Writer) error {
	_, err := wr.Write([]byte("pnts")) // magic
	if err != nil {
		return err
	}
	err = utils.WriteIntAs4ByteNumber(1, wr) // version number
	if err != nil {
		return err
	}
	// Total file length: header + feature table (JSON + binary) + batch table (JSON + binary)
	err = utils.WriteIntAs4ByteNumber(pntsHeaderLen+featureTableLen+featureTableBinaryLen(numPoints)+batchTableLen+batchBodyLen, wr)
	if err != nil {
		return err
	}
	err = utils.WriteIntAs4ByteNumber(featureTableLen, wr)
	if err != nil {
		return err
	}
	err = utils.WriteIntAs4ByteNumber(featureTableBinaryLen(numPoints), wr) // feature table binary: positions + colors + padding
	if err != nil {
		return err
	}
	err = utils.WriteIntAs4ByteNumber(batchTableLen, wr)
	if err != nil {
		return err
	}
	err = utils.WriteIntAs4ByteNumber(batchBodyLen, wr)
	if err != nil {
		return err
	}
	return nil
}

func writeBytes(tableBytes []byte, wr io.Writer) error {
	if len(tableBytes) == 0 {
		return nil
	}
	_, err := wr.Write(tableBytes)
	return err
}

func (e *PntsEncoder) generateFeatureTableJsonContent(pointNo int) string {
	return fmt.Sprintf(`{"POINTS_LENGTH":%d,"POSITION":{"byteOffset":0},"RGB":{"byteOffset":%d}}`,
		pointNo,
		pointNo*12,
	)
}

func (e *PntsEncoder) writeGeneric(node tree.Node, wp plugin.WriterProvider, prefix string, columns []encoding.AttributeColumn) error {
	pts, err := node.Points()
	if err != nil {
		return err
	}
	defer pts.Close()

	n := pts.Len()
	featureTableBytes, featureTableLen := e.generateFeatureTable(n)

	// Batch body layout: one contiguous column per attribute, in column order.
	attrOffsets := make([]int, len(columns))
	batchBodyLen := 0
	for i, col := range columns {
		batchBodyLen = alignTo(batchBodyLen, pntsAttributeAlignment(col.Summary.Type))
		attrOffsets[i] = batchBodyLen
		batchBodyLen += n * col.Size
	}
	batchBodyLen = alignTo(batchBodyLen, pntsAlignment)

	featureBinaryLen := featureTableBinaryLen(n)
	batchTableOffset := pntsHeaderLen + featureTableLen + featureBinaryLen
	batchTableBytes, batchTableLen := e.generateGenericBatchTable(columns, attrOffsets, batchTableOffset)

	f, err := wp(prefix + e.filename)
	if err != nil {
		return err
	}
	wr := bufio.NewWriterSize(f, bufferSize)
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()

	if err := e.writePntsHeader(n, featureTableLen, batchTableLen, batchBodyLen, wr); err != nil {
		return err
	}
	if err := writeBytes(featureTableBytes, wr); err != nil {
		return err
	}

	coordsEnd := n * 12
	colorsEnd := coordsEnd + n*3
	featurePaddingLen := featureBinaryLen - colorsEnd
	bufPtr := pntsBufPool.GetWithMinCapacity(colorsEnd + featurePaddingLen + batchBodyLen)
	defer pntsBufPool.Put(bufPtr)
	buf := *bufPtr
	clear(buf[colorsEnd : colorsEnd+featurePaddingLen])
	clear(buf[colorsEnd+featurePaddingLen : colorsEnd+featurePaddingLen+batchBodyLen])

	for i := 0; i < n; i++ {
		pt, err := pts.Next()
		if err != nil {
			return err
		}
		coordsOff := i * 12
		binary.LittleEndian.PutUint32(buf[coordsOff:], math.Float32bits(pt.X))
		binary.LittleEndian.PutUint32(buf[coordsOff+4:], math.Float32bits(pt.Y))
		binary.LittleEndian.PutUint32(buf[coordsOff+8:], math.Float32bits(pt.Z))
		colorsOff := coordsEnd + i*3
		buf[colorsOff] = pt.R
		buf[colorsOff+1] = pt.G
		buf[colorsOff+2] = pt.B

		// The batch table binary and the packed point values share the same
		// little-endian scalar encoding, so columns are filled with plain copies.
		for j, col := range columns {
			off := colorsEnd + featurePaddingLen + attrOffsets[j] + i*col.Size
			if b := encoding.ColumnBytes(pt, col); b != nil {
				copy(buf[off:off+col.Size], b)
			}
		}
	}

	if _, err := wr.Write(buf[:coordsEnd]); err != nil {
		return err
	}
	if _, err := wr.Write(buf[coordsEnd:colorsEnd]); err != nil {
		return err
	}
	if featurePaddingLen > 0 {
		if _, err := wr.Write(buf[colorsEnd : colorsEnd+featurePaddingLen]); err != nil {
			return err
		}
	}
	if err := writeBytes(batchTableBytes, wr); err != nil {
		return err
	}
	if batchBodyLen > 0 {
		batchBodyStart := colorsEnd + featurePaddingLen
		if _, err := wr.Write(buf[batchBodyStart : batchBodyStart+batchBodyLen]); err != nil {
			return err
		}
	}
	if err := wr.Flush(); err != nil {
		return err
	}
	closed = true
	return f.Close()
}

func (e *PntsEncoder) generateGenericBatchTable(columns []encoding.AttributeColumn, offsets []int, startOffset int) ([]byte, int) {
	if len(columns) == 0 {
		return nil, 0
	}
	s := e.generateGenericBatchTableJsonContent(columns, offsets)
	batchTableBytes := padJSONToAlignment([]byte(s), startOffset)
	return batchTableBytes, len(batchTableBytes)
}

func (e *PntsEncoder) generateGenericBatchTableJsonContent(columns []encoding.AttributeColumn, offsets []int) string {
	entries := make([]string, 0, len(columns))
	for i, col := range columns {
		componentType, _ := encoding.PntsComponentType(col.Summary.Type)
		entries = append(entries, fmt.Sprintf(`"%s":{"byteOffset":%d,"componentType":"%s","type":"SCALAR"}`,
			encoding.AttributeOutputName(col.Summary.Name),
			offsets[i],
			componentType,
		))
	}
	return fmt.Sprintf("{%s}", strings.Join(entries, ",\n\t"))
}

func featureTableBinaryLen(numPoints int) int {
	return alignTo(numPoints*15, pntsAlignment)
}

func alignTo(n int, alignment int) int {
	if alignment <= 1 {
		return n
	}
	if rem := n % alignment; rem != 0 {
		return n + alignment - rem
	}
	return n
}

func padJSONToAlignment(jsonBytes []byte, startOffset int) []byte {
	pad := alignTo(startOffset+len(jsonBytes), pntsAlignment) - (startOffset + len(jsonBytes))
	if pad == 0 {
		return jsonBytes
	}
	out := make([]byte, len(jsonBytes)+pad)
	copy(out, jsonBytes)
	for i := len(jsonBytes); i < len(out); i++ {
		out[i] = ' '
	}
	return out
}

func pntsAttributeAlignment(t model.AttributeType) int {
	switch t {
	case model.AttributeInt16, model.AttributeUint16:
		return 2
	case model.AttributeInt32, model.AttributeUint32, model.AttributeFloat32:
		return 4
	case model.AttributeFloat64:
		return 8
	default:
		return 1
	}
}
