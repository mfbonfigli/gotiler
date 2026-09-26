package compression

// encodeMeshoptAttributesInto appends the EXT_meshopt_compression ATTRIBUTES (mode 0)
// encoding of count elements of stride bytes each from data to dst, and returns the
// extended slice. Preconditions: stride%4==0, stride<=256, len(data)==count*stride.
//
// The output is a valid meshopt ATTRIBUTES stream that can be decoded by any
// conformant EXT_meshopt_compression loader.
func encodeMeshoptAttributesInto(dst []byte, data []byte, count, stride int) []byte {
	if count == 0 {
		return dst
	}

	// maxBlockElements = min((8192/stride) &^ 15, 256)
	maxBlockElements := min((8192/stride)&^15, 256)

	dst = append(dst, 0xa0) // stream header byte

	// prevBytes: last element's bytes from the previous block, used as delta baseline.
	// Stays on the stack for all strides used in practice (≤ 256 bytes).
	var prevBytesBuf [256]byte
	prevBytes := prevBytesBuf[:stride]

	remaining := count
	offset := 0
	for remaining > 0 {
		blockElements := min(remaining, maxBlockElements)
		blockData := data[offset : offset+blockElements*stride]
		dst = encodeAttributeBlock(dst, blockData, blockElements, stride, prevBytes)
		copy(prevBytes, blockData[(blockElements-1)*stride:])
		offset += blockElements * stride
		remaining -= blockElements
	}

	// Tail block: baseline element (all zeros), padded to max(stride, 32) bytes.
	tailSize := max(stride, 32)
	var tailBuf [256]byte
	dst = append(dst, tailBuf[:tailSize]...)

	return dst
}

func encodeAttributeBlock(out []byte, data []byte, blockElements, stride int, prevBytes []byte) []byte {
	groupCount := (blockElements + 15) / 16
	for b := 0; b < stride; b++ {
		out = encodeDataBlock(out, data, b, blockElements, stride, groupCount, prevBytes[b])
	}
	return out
}

func encodeDataBlock(out []byte, data []byte, bytePos, blockElements, stride, groupCount int, prev uint8) []byte {
	// Compute zigzag-encoded deltas into a stack-allocated array (maxBlockElements ≤ 256).
	var deltasBuf [256]uint8
	deltas := deltasBuf[:blockElements]
	for i := 0; i < blockElements; i++ {
		cur := data[i*stride+bytePos]
		deltas[i] = zigzagByte(cur - prev)
		prev = cur
	}

	// Reserve space for header bytes (2 bits per group) before writing group data,
	// so group data can be appended directly without buffering.
	headerByteCount := (groupCount + 3) / 4
	headerStart := len(out)
	var zeroes [4]byte
	out = append(out, zeroes[:headerByteCount]...)

	var groupDeltas [16]uint8
	for g := 0; g < groupCount; g++ {
		start := g * 16
		end := start + 16
		if end > blockElements {
			end = blockElements
		}
		groupDeltas = [16]uint8{} // zero-fill (handles partial last group)
		copy(groupDeltas[:], deltas[start:end])

		var mode int
		out, mode = appendEncodedGroup(out, &groupDeltas)
		out[headerStart+g/4] |= byte(mode << ((g % 4) * 2))
	}

	return out
}

// appendEncodedGroup picks the best mode for 16 zigzag deltas, appends the encoded
// bytes directly to out, and returns (out, mode).
func appendEncodedGroup(out []byte, deltas *[16]uint8) ([]byte, int) {
	// Mode 0: all zeros — 0 bytes
	allZero := true
	for _, d := range deltas {
		if d != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return out, 0
	}

	// Count sentinels to determine encoded sizes without building intermediate slices.
	extras1, extras2 := 0, 0
	for _, d := range deltas {
		if d >= 3 {
			extras1++
		}
		if d >= 15 {
			extras2++
		}
	}
	size1 := 4 + extras1 // mode 1: 4 packed bytes + overflow extras
	size2 := 8 + extras2 // mode 2: 8 packed bytes + overflow extras
	const size3 = 16     // mode 3: 16 raw bytes

	if size1 <= size2 && size1 <= size3 {
		return append2BitSentinel(out, deltas), 1
	}
	if size2 <= size3 {
		return append4BitSentinel(out, deltas), 2
	}
	for _, d := range deltas {
		out = append(out, d)
	}
	return out, 3
}

// append2BitSentinel writes mode-1 (2-bit sentinel) encoding directly to out.
// Spec packing: delta0 at bits 6-7, delta3 at bits 0-1 within each packed byte.
func append2BitSentinel(out []byte, deltas *[16]uint8) []byte {
	var packed [4]byte
	var extrasBuf [16]byte
	extrasCount := 0
	for i := 0; i < 16; i++ {
		d := deltas[i]
		var v byte
		if d >= 3 {
			v = 3
			extrasBuf[extrasCount] = d
			extrasCount++
		} else {
			v = d
		}
		packed[i/4] |= v << ((3 - (i % 4)) * 2)
	}
	out = append(out, packed[:]...)
	out = append(out, extrasBuf[:extrasCount]...)
	return out
}

// append4BitSentinel writes mode-2 (4-bit sentinel) encoding directly to out.
// Spec packing: delta0 at high nibble, delta1 at low nibble within each packed byte.
func append4BitSentinel(out []byte, deltas *[16]uint8) []byte {
	var packed [8]byte
	var extrasBuf [16]byte
	extrasCount := 0
	for i := 0; i < 16; i++ {
		d := deltas[i]
		var v byte
		if d >= 15 {
			v = 15
			extrasBuf[extrasCount] = d
			extrasCount++
		} else {
			v = d
		}
		if i%2 == 0 {
			packed[i/2] |= v << 4 // high nibble
		} else {
			packed[i/2] |= v // low nibble
		}
	}
	out = append(out, packed[:]...)
	out = append(out, extrasBuf[:extrasCount]...)
	return out
}

// zigzagByte applies the spec zigzag formula: encode(v) = (v&0x80!=0) ? ^(v<<1) : (v<<1).
// Both branches operate in uint8 arithmetic, so the result is always in [0, 255].
func zigzagByte(v uint8) uint8 {
	if v&0x80 != 0 {
		return ^(v << 1)
	}
	return v << 1
}
