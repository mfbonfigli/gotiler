package geotiffcolorizer

// Bit-level primitives for the LERC (Lerc2) decoder: byte reader, validity
// bit mask, RLE mask codec and the BitStuffer2 integer unpacker. Ported from
// Esri's LERC C++ implementation (Apache-2.0), decode paths only.

import (
	"encoding/binary"
	"fmt"
	"math"
)

type lercReader struct {
	buf []byte
	pos int
}

func (r *lercReader) remaining() int {
	return len(r.buf) - r.pos
}

func (r *lercReader) u8() (byte, error) {
	if r.remaining() < 1 {
		return 0, fmt.Errorf("LERC blob truncated")
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *lercReader) u16() (uint16, error) {
	if r.remaining() < 2 {
		return 0, fmt.Errorf("LERC blob truncated")
	}
	v := binary.LittleEndian.Uint16(r.buf[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *lercReader) u32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, fmt.Errorf("LERC blob truncated")
	}
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *lercReader) i32() (int32, error) {
	v, err := r.u32()
	return int32(v), err
}

func (r *lercReader) u64() (uint64, error) {
	if r.remaining() < 8 {
		return 0, fmt.Errorf("LERC blob truncated")
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *lercReader) f64() (float64, error) {
	v, err := r.u64()
	return math.Float64frombits(v), err
}

func (r *lercReader) f32() (float32, error) {
	v, err := r.u32()
	return math.Float32frombits(v), err
}

// lercBitMask is the per-pixel validity mask, one bit per pixel, MSB first.
type lercBitMask struct {
	bits  []byte
	nCols int
	nRows int
}

func newLercBitMask(nCols, nRows int) *lercBitMask {
	return &lercBitMask{
		bits:  make([]byte, (nCols*nRows+7)>>3),
		nCols: nCols,
		nRows: nRows,
	}
}

func (m *lercBitMask) size() int {
	return (m.nCols*m.nRows + 7) >> 3
}

func (m *lercBitMask) isValid(k int) bool {
	return m.bits[k>>3]&(0x80>>(k&7)) != 0
}

func (m *lercBitMask) setAllValid() {
	for i := range m.bits {
		m.bits[i] = 0xff
	}
}

func (m *lercBitMask) countValid() int {
	cnt := 0
	total := m.nCols * m.nRows
	for k := 0; k < total; k++ {
		if m.isValid(k) {
			cnt++
		}
	}
	return cnt
}

// lercRLEDecompress expands the RLE stream into dst. Counts are little-endian
// int16: positive = literal run, negative = repeat one byte, -32768 = end.
func lercRLEDecompress(src []byte, dst []byte) error {
	if len(src) < 2 {
		return fmt.Errorf("LERC RLE mask truncated")
	}
	pos := 0
	remaining := len(src) - 2
	readCount := func() (int, error) {
		if pos+2 > len(src) {
			return 0, fmt.Errorf("LERC RLE mask truncated")
		}
		v := int(int16(binary.LittleEndian.Uint16(src[pos:])))
		pos += 2
		return v, nil
	}
	arrIdx := 0
	cnt, err := readCount()
	if err != nil {
		return err
	}
	for cnt != -32768 {
		n := cnt
		payload := 1
		if cnt > 0 {
			payload = cnt
		} else {
			n = -cnt
		}
		if remaining < payload+2 || arrIdx+n > len(dst) {
			return fmt.Errorf("LERC RLE mask outside stream")
		}
		if cnt > 0 {
			copy(dst[arrIdx:arrIdx+n], src[pos:pos+n])
			pos += n
			arrIdx += n
		} else {
			b := src[pos]
			pos++
			for i := 0; i < n; i++ {
				dst[arrIdx] = b
				arrIdx++
			}
		}
		remaining -= payload + 2
		if cnt, err = readCount(); err != nil {
			return err
		}
	}
	return nil
}

func lercNumTailBytesNotNeeded(numElements, numBits int) int {
	numBitsTail := (numElements * numBits) & 31
	numBytesTail := (numBitsTail + 7) >> 3
	if numBytesTail > 0 {
		return 4 - numBytesTail
	}
	return 0
}

func lercDecodeUInt(r *lercReader, numBytes int) (int, error) {
	switch numBytes {
	case 1:
		v, err := r.u8()
		return int(v), err
	case 2:
		v, err := r.u16()
		return int(v), err
	case 4:
		v, err := r.u32()
		return int(v), err
	default:
		return 0, fmt.Errorf("invalid LERC element count width %d", numBytes)
	}
}

// lercBitUnstuff unpacks numElements values of numBits each, packed LSB-first
// into little-endian 32-bit words (Lerc2 version >= 3 layout).
func lercBitUnstuff(r *lercReader, numElements, numBits int) ([]uint32, error) {
	if numElements <= 0 || numBits <= 0 || numBits >= 32 {
		return nil, fmt.Errorf("invalid LERC bit stuffing (%d elements, %d bits)", numElements, numBits)
	}
	numUInts := (numElements*numBits + 31) / 32
	numBytesUsed := numUInts*4 - lercNumTailBytesNotNeeded(numElements, numBits)
	if r.remaining() < numBytesUsed {
		return nil, fmt.Errorf("LERC bit-stuffed data truncated")
	}
	words := lercLoadWords(r.buf[r.pos:r.pos+numBytesUsed], numUInts)
	out := make([]uint32, numElements)
	bitPos := 0
	src := 0
	nb := 32 - numBits
	for i := range out {
		if nb-bitPos >= 0 {
			out[i] = (words[src] << (nb - bitPos)) >> nb
			bitPos += numBits
			if bitPos == 32 {
				src++
				bitPos = 0
			}
		} else {
			v := words[src] >> bitPos
			src++
			v |= (words[src] << (64 - numBits - bitPos)) >> nb
			out[i] = v
			bitPos -= nb
		}
	}
	r.pos += numBytesUsed
	return out, nil
}

// lercBitUnstuffBeforeV3 unpacks the MSB-first layout used by Lerc2 < 3.
func lercBitUnstuffBeforeV3(r *lercReader, numElements, numBits int) ([]uint32, error) {
	if numElements <= 0 || numBits <= 0 || numBits >= 32 {
		return nil, fmt.Errorf("invalid LERC bit stuffing (%d elements, %d bits)", numElements, numBits)
	}
	numUInts := (numElements*numBits + 31) / 32
	nBytesToCopy := (numElements*numBits + 7) / 8
	if r.remaining() < nBytesToCopy {
		return nil, fmt.Errorf("LERC bit-stuffed data truncated")
	}
	words := lercLoadWords(r.buf[r.pos:r.pos+nBytesToCopy], numUInts)
	// The dropped tail bytes are the high-order bytes of the last word in the
	// MSB-first layout; shift the available bytes up to their place.
	words[numUInts-1] <<= 8 * uint(lercNumTailBytesNotNeeded(numElements, numBits))
	out := make([]uint32, numElements)
	bitPos := 0
	src := 0
	for i := range out {
		if 32-bitPos >= numBits {
			out[i] = (words[src] << bitPos) >> (32 - numBits)
			bitPos += numBits
			if bitPos == 32 {
				bitPos = 0
				src++
			}
		} else {
			n := words[src] << bitPos
			src++
			out[i] = n >> (32 - numBits)
			bitPos -= 32 - numBits
			out[i] |= words[src] >> (32 - bitPos)
		}
	}
	r.pos += nBytesToCopy
	return out, nil
}

func lercLoadWords(src []byte, numUInts int) []uint32 {
	words := make([]uint32, numUInts)
	full := len(src) / 4
	for i := 0; i < full; i++ {
		words[i] = binary.LittleEndian.Uint32(src[i*4:])
	}
	if tail := len(src) - full*4; tail > 0 {
		var w uint32
		for i := 0; i < tail; i++ {
			w |= uint32(src[full*4+i]) << (8 * uint(i))
		}
		words[full] = w
	}
	return words
}

func lercUnstuff(r *lercReader, numElements, numBits, version int) ([]uint32, error) {
	if version >= 3 {
		return lercBitUnstuff(r, numElements, numBits)
	}
	return lercBitUnstuffBeforeV3(r, numElements, numBits)
}

// lercBitStufferDecode is BitStuffer2::Decode: an optional LUT plus
// bit-stuffed values or LUT indexes.
func lercBitStufferDecode(r *lercReader, maxElementCount, version int) ([]uint32, error) {
	numBitsByte, err := r.u8()
	if err != nil {
		return nil, err
	}
	bits67 := int(numBitsByte >> 6)
	nb := 4
	if bits67 != 0 {
		nb = 3 - bits67
	}
	doLut := numBitsByte&(1<<5) != 0
	numBits := int(numBitsByte & 31)

	numElements, err := lercDecodeUInt(r, nb)
	if err != nil {
		return nil, err
	}
	if numElements < 0 || numElements > maxElementCount {
		return nil, fmt.Errorf("LERC element count %d exceeds tile capacity %d", numElements, maxElementCount)
	}

	if !doLut {
		if numBits == 0 {
			return make([]uint32, numElements), nil
		}
		return lercUnstuff(r, numElements, numBits, version)
	}

	if numBits == 0 {
		return nil, fmt.Errorf("LERC LUT with zero bit width")
	}
	nLutByte, err := r.u8()
	if err != nil {
		return nil, err
	}
	nLut := int(nLutByte) - 1
	lut, err := lercUnstuff(r, nLut, numBits, version)
	if err != nil {
		return nil, err
	}
	nBitsLut := 0
	for nLut>>uint(nBitsLut) != 0 {
		nBitsLut++
	}
	if nBitsLut == 0 {
		return nil, fmt.Errorf("LERC LUT is empty")
	}
	indexes, err := lercUnstuff(r, numElements, nBitsLut, version)
	if err != nil {
		return nil, err
	}
	fullLut := make([]uint32, 1, len(lut)+1) // index 0 decodes to value 0
	fullLut = append(fullLut, lut...)
	out := make([]uint32, numElements)
	for i, idx := range indexes {
		if int(idx) >= len(fullLut) {
			return nil, fmt.Errorf("LERC LUT index %d outside table of %d", idx, len(fullLut))
		}
		out[i] = fullLut[idx]
	}
	return out, nil
}
