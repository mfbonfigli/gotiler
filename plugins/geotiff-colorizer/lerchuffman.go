package geotiffcolorizer

// Huffman decoder for LERC byte/char rasters. Ported from Esri's LERC C++
// implementation (Apache-2.0), decode paths only. The bit stream is consumed
// MSB-first within little-endian 32-bit words.

import (
	"encoding/binary"
	"fmt"
)

const (
	lercHuffmanMaxHistoSize  = 1 << 15
	lercHuffmanMaxNumBitsLUT = 12
)

type lercHuffCode struct {
	length int
	code   uint32
}

type lercHuffLUTEntry struct {
	length int16
	value  int16
}

type lercHuffNode struct {
	value  int16
	child0 *lercHuffNode
	child1 *lercHuffNode
}

type lercHuffman struct {
	codeTable  []lercHuffCode
	lut        []lercHuffLUTEntry
	root       *lercHuffNode
	skipBits   int
	numBitsLUT int
}

func lercHuffIndexWrapAround(i, size int) int {
	if i < size {
		return i
	}
	return i - size
}

func (h *lercHuffman) readCodeTable(r *lercReader, lerc2Version int) error {
	var ints [4]int32
	for i := range ints {
		v, err := r.i32()
		if err != nil {
			return err
		}
		ints[i] = v
	}
	version, size, i0, i1 := int(ints[0]), int(ints[1]), int(ints[2]), int(ints[3])
	if version < 2 {
		return fmt.Errorf("unsupported LERC Huffman table version %d", version)
	}
	if i0 >= i1 || i0 < 0 || size < 0 || size > lercHuffmanMaxHistoSize {
		return fmt.Errorf("invalid LERC Huffman table range [%d,%d) of %d", i0, i1, size)
	}
	if lercHuffIndexWrapAround(i0, size) >= size || lercHuffIndexWrapAround(i1-1, size) >= size {
		return fmt.Errorf("LERC Huffman table range outside histogram")
	}
	lengths, err := lercBitStufferDecode(r, i1-i0, lerc2Version)
	if err != nil {
		return err
	}
	if len(lengths) != i1-i0 {
		return fmt.Errorf("LERC Huffman code length count mismatch")
	}
	h.codeTable = make([]lercHuffCode, size)
	for i := i0; i < i1; i++ {
		k := lercHuffIndexWrapAround(i, size)
		h.codeTable[k].length = int(lengths[i-i0])
	}
	return h.bitUnstuffCodes(r, i0, i1)
}

// bitUnstuffCodes reads the variable-length canonical codes, MSB-first in
// little-endian 32-bit words.
func (h *lercHuffman) bitUnstuffCodes(r *lercReader, i0, i1 int) error {
	size := len(h.codeTable)
	pos0 := r.pos
	pos := r.pos
	bitPos := 0
	word := func(p int) (uint32, error) {
		if p+4 > len(r.buf) {
			return 0, fmt.Errorf("LERC Huffman codes truncated")
		}
		return binary.LittleEndian.Uint32(r.buf[p:]), nil
	}
	for i := i0; i < i1; i++ {
		k := lercHuffIndexWrapAround(i, size)
		length := h.codeTable[k].length
		if length == 0 {
			continue
		}
		if length > 32 {
			return fmt.Errorf("LERC Huffman code longer than 32 bits")
		}
		temp, err := word(pos)
		if err != nil {
			return err
		}
		h.codeTable[k].code = (temp << bitPos) >> (32 - length)
		if 32-bitPos >= length {
			bitPos += length
			if bitPos == 32 {
				bitPos = 0
				pos += 4
			}
		} else {
			bitPos += length - 32
			pos += 4
			temp, err = word(pos)
			if err != nil {
				return err
			}
			h.codeTable[k].code |= temp >> (32 - bitPos)
		}
	}
	consumed := pos - pos0
	if bitPos > 0 {
		consumed += 4
	}
	if r.remaining() < consumed {
		return fmt.Errorf("LERC Huffman codes truncated")
	}
	r.pos += consumed
	return nil
}

func (h *lercHuffman) getRange() (i0, i1, maxCodeLength int, err error) {
	size := len(h.codeTable)
	if size == 0 || size >= lercHuffmanMaxHistoSize {
		return 0, 0, 0, fmt.Errorf("invalid LERC Huffman table size %d", size)
	}
	i := 0
	for i < size && h.codeTable[i].length == 0 {
		i++
	}
	i0 = i
	i = size - 1
	for i >= 0 && h.codeTable[i].length == 0 {
		i--
	}
	i1 = i + 1
	if i1 <= i0 {
		return 0, 0, 0, fmt.Errorf("empty LERC Huffman table")
	}
	// cover the common case that the peak wraps around 0: find the largest
	// stretch of zero-length codes and, if smaller, wrap around it.
	segmStart, segmLen := 0, 0
	j := 0
	for j < size {
		for j < size && h.codeTable[j].length > 0 {
			j++
		}
		k0 := j
		for j < size && h.codeTable[j].length == 0 {
			j++
		}
		if j-k0 > segmLen {
			segmStart, segmLen = k0, j-k0
		}
	}
	if size-segmLen < i1-i0 {
		i0 = segmStart + segmLen
		i1 = segmStart + size // wrap around
	}
	if i1 <= i0 {
		return 0, 0, 0, fmt.Errorf("empty LERC Huffman table")
	}
	maxLen := 0
	for i := i0; i < i1; i++ {
		k := lercHuffIndexWrapAround(i, size)
		if l := h.codeTable[k].length; l > maxLen {
			maxLen = l
		}
	}
	if maxLen <= 0 || maxLen > 32 {
		return 0, 0, 0, fmt.Errorf("invalid LERC Huffman max code length %d", maxLen)
	}
	return i0, i1, maxLen, nil
}

func (h *lercHuffman) buildTreeFromCodes() error {
	i0, i1, maxLen, err := h.getRange()
	if err != nil {
		return err
	}
	size := len(h.codeTable)
	minNumZeroBits := 32

	needTree := maxLen > lercHuffmanMaxNumBitsLUT
	h.numBitsLUT = maxLen
	if h.numBitsLUT > lercHuffmanMaxNumBitsLUT {
		h.numBitsLUT = lercHuffmanMaxNumBitsLUT
	}
	sizeLUT := 1 << h.numBitsLUT
	h.lut = make([]lercHuffLUTEntry, sizeLUT)
	for i := range h.lut {
		h.lut[i] = lercHuffLUTEntry{length: -1, value: -1}
	}
	for i := i0; i < i1; i++ {
		k := lercHuffIndexWrapAround(i, size)
		length := h.codeTable[k].length
		if length == 0 {
			continue
		}
		code := h.codeTable[k].code
		if length <= h.numBitsLUT {
			code <<= uint(h.numBitsLUT - length)
			numEntries := 1 << uint(h.numBitsLUT-length)
			entry := lercHuffLUTEntry{length: int16(length), value: int16(k)}
			for j := 0; j < numEntries; j++ {
				h.lut[int(code)|j] = entry
			}
		} else {
			// large canonical codes start with zeros; count the leading zeros
			shift := 1
			for code>>1 != 0 {
				code >>= 1
				shift++
			}
			if zeros := length - shift; zeros < minNumZeroBits {
				minNumZeroBits = zeros
			}
		}
	}
	h.skipBits = 0
	if needTree {
		h.skipBits = minNumZeroBits
		h.root = &lercHuffNode{value: -1}
		for i := i0; i < i1; i++ {
			k := lercHuffIndexWrapAround(i, size)
			length := h.codeTable[k].length
			if length == 0 || length <= h.numBitsLUT {
				continue
			}
			code := h.codeTable[k].code
			node := h.root
			for j := length - h.skipBits - 1; j >= 0; j-- {
				if code&(1<<uint(j)) != 0 {
					if node.child1 == nil {
						node.child1 = &lercHuffNode{value: -1}
					}
					node = node.child1
				} else {
					if node.child0 == nil {
						node.child0 = &lercHuffNode{value: -1}
					}
					node = node.child0
				}
				if j == 0 {
					node.value = int16(k)
				}
			}
		}
	}
	return nil
}

// lercHuffState tracks the bit position of a Huffman-coded stream.
type lercHuffState struct {
	pos    int
	bitPos int
}

func (h *lercHuffman) decodeOneValue(buf []byte, st *lercHuffState) (int, error) {
	if st.bitPos < 0 || st.bitPos >= 32 || st.pos+4 > len(buf) {
		return 0, fmt.Errorf("LERC Huffman stream truncated")
	}
	temp := binary.LittleEndian.Uint32(buf[st.pos:])
	valTmp := int((temp << st.bitPos) >> (32 - h.numBitsLUT))
	if 32-st.bitPos < h.numBitsLUT {
		if st.pos+8 > len(buf) {
			return 0, fmt.Errorf("LERC Huffman stream truncated")
		}
		temp = binary.LittleEndian.Uint32(buf[st.pos+4:])
		valTmp |= int(temp >> (64 - st.bitPos - h.numBitsLUT))
	}
	if h.lut[valTmp].length >= 0 {
		value := int(h.lut[valTmp].value)
		st.bitPos += int(h.lut[valTmp].length)
		if st.bitPos >= 32 {
			st.bitPos -= 32
			st.pos += 4
		}
		return value, nil
	}
	if h.root == nil {
		return 0, fmt.Errorf("LERC Huffman code outside LUT without tree")
	}
	st.bitPos += h.skipBits
	if st.bitPos >= 32 {
		st.bitPos -= 32
		st.pos += 4
	}
	node := h.root
	for {
		if st.pos+4 > len(buf) {
			return 0, fmt.Errorf("LERC Huffman stream truncated")
		}
		temp = binary.LittleEndian.Uint32(buf[st.pos:])
		bit := (temp << st.bitPos) >> 31
		st.bitPos++
		if st.bitPos == 32 {
			st.bitPos = 0
			st.pos += 4
		}
		if bit != 0 {
			node = node.child1
		} else {
			node = node.child0
		}
		if node == nil {
			return 0, fmt.Errorf("invalid LERC Huffman code")
		}
		if node.value >= 0 {
			return int(node.value), nil
		}
	}
}
