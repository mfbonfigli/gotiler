package compression

import (
	"encoding/binary"
	"math/bits"
)

// Version 1 of the meshopt ATTRIBUTES bitstream, defined by the
// KHR_meshopt_compression extension. Compared to version 0 (the only one
// EXT_meshopt_compression allows) it adds:
//
//   - a control header per attribute block, selecting for each byte position
//     either the {0,1,2,4} or the {1,2,4,8} set of group bit widths, an
//     all-zero encoding, or literal storage;
//   - channel modes, stored in the tail, selecting for each 4-byte channel
//     byte deltas, 16-bit deltas or rotated 32-bit XOR deltas.
//
// The encoder, including its mode selection heuristics, is a Go port of the
// vertex codec of meshoptimizer (src/vertexcodec.cpp, MIT License, Copyright
// (c) 2016-2026 Arseny Kapoulkine); see THIRD-PARTY-LICENSES.md.

const (
	meshoptVertexBlockSizeBytes = 8192
	meshoptVertexBlockMaxSize   = 256
	meshoptTailMinSizeV1        = 24

	// channel modes, stored in the low 4 bits of each tail channel byte; the
	// high 4 bits hold the rotation of the XOR mode
	meshoptChannelBytes = 0
	meshoptChannelShort = 1
	meshoptChannelXor   = 2

	// control modes, 2 bits per byte position in each block control header
	meshoptControlZero    = 2
	meshoptControlLiteral = 3
)

// meshoptControlBits lists, for control modes 0 and 1, the bit widths that
// the 2-bit group headers of a data block select.
var meshoptControlBits = [2][4]int{{0, 1, 2, 4}, {1, 2, 4, 8}}

// encodeMeshoptAttributesV1Into appends the version 1 ATTRIBUTES encoding of
// count elements of stride bytes each from data to dst, and returns the
// extended slice. Preconditions: stride%4==0, stride<=256,
// len(data)==count*stride.
//
// The output can be decoded by any conformant KHR_meshopt_compression loader.
func encodeMeshoptAttributesV1Into(dst []byte, data []byte, count, stride int) []byte {
	if count == 0 {
		return dst
	}
	var channelsBuf [64]byte
	channels := channelsBuf[:stride/4]
	if count > 1 {
		blockElements := meshoptBlockElements(stride)
		for k := 0; k < stride; k += 4 {
			rot := estimateMeshoptRotate(data, count, stride, k)
			channels[k/4] = estimateMeshoptChannel(data, count, stride, k, blockElements, rot)
		}
	}
	return encodeMeshoptAttributesV1WithChannels(dst, data, count, stride, channels)
}

// meshoptBlockElements returns the number of elements of an attribute block,
// which is part of the format.
func meshoptBlockElements(stride int) int {
	return min((meshoptVertexBlockSizeBytes/stride)&^15, meshoptVertexBlockMaxSize)
}

// encodeMeshoptAttributesV1WithChannels is encodeMeshoptAttributesV1Into with
// the channel modes already chosen, one per 4-byte channel.
func encodeMeshoptAttributesV1WithChannels(dst []byte, data []byte, count, stride int, channels []byte) []byte {
	if count == 0 {
		return dst
	}
	blockElements := meshoptBlockElements(stride)

	dst = append(dst, 0xa1) // stream header byte, version 1

	// The baseline element is the first element itself, so its deltas are zero.
	var lastBuf [256]byte
	last := lastBuf[:stride]
	copy(last, data[:stride])

	for offset := 0; offset < count; {
		n := min(count-offset, blockElements)
		block := data[offset*stride : (offset+n)*stride]
		dst = encodeAttributeBlockV1(dst, block, n, stride, last, channels)
		copy(last, block[(n-1)*stride:])
		offset += n
	}

	// Tail: zero padding up to the minimum tail size, the baseline element and
	// the channel modes.
	var zeroes [meshoptTailMinSizeV1]byte
	if tailSize := stride + stride/4; tailSize < meshoptTailMinSizeV1 {
		dst = append(dst, zeroes[:meshoptTailMinSizeV1-tailSize]...)
	}
	dst = append(dst, data[:stride]...)
	return append(dst, channels...)
}

func encodeAttributeBlockV1(out []byte, block []byte, n, stride int, last []byte, channels []byte) []byte {
	aligned := (n + 15) &^ 15

	var zeroes [64]byte
	controlStart := len(out)
	out = append(out, zeroes[:stride/4]...)

	// deltas past n stay zero: they pad the last group of the block
	var deltas [meshoptVertexBlockMaxSize]byte
	for k := 0; k < stride; k++ {
		meshoptDeltasV1(deltas[:], block, n, stride, last, k, channels[k/4])
		ctrl := chooseMeshoptControl(deltas[:aligned], n)
		out[controlStart+k/4] |= byte(ctrl << ((k % 4) * 2))
		switch ctrl {
		case meshoptControlZero:
			// no data stored
		case meshoptControlLiteral:
			out = append(out, deltas[:n]...)
		default:
			out = appendDataBlockV1(out, deltas[:aligned], ctrl)
		}
	}
	return out
}

// meshoptDeltasV1 writes to dst the n delta bytes of byte position k, computed
// with the mode of the channel the byte belongs to.
func meshoptDeltasV1(dst []byte, block []byte, n, stride int, last []byte, k int, channel byte) {
	switch channel & 3 {
	case meshoptChannelShort:
		k0 := k &^ 1
		shift := uint(k&1) * 8
		prev := binary.LittleEndian.Uint16(last[k0:])
		for i := 0; i < n; i++ {
			cur := binary.LittleEndian.Uint16(block[i*stride+k0:])
			dst[i] = byte(zigzag16(cur-prev) >> shift)
			prev = cur
		}
	case meshoptChannelXor:
		k0 := k &^ 3
		shift := uint(k&3) * 8
		rot := int(channel >> 4)
		prev := binary.LittleEndian.Uint32(last[k0:])
		for i := 0; i < n; i++ {
			cur := binary.LittleEndian.Uint32(block[i*stride+k0:])
			dst[i] = byte(bits.RotateLeft32(cur^prev, rot) >> shift)
			prev = cur
		}
	default:
		prev := last[k]
		for i := 0; i < n; i++ {
			cur := block[i*stride+k]
			dst[i] = zigzagByte(cur - prev)
			prev = cur
		}
	}
}

// chooseMeshoptControl picks the control mode of a byte position from its
// deltas (zero-padded to a multiple of 16): all-zero when possible, otherwise
// the cheaper of the two bit width sets, or literal storage when neither beats
// storing the n deltas as they are.
func chooseMeshoptControl(deltas []byte, n int) int {
	if allZeroBytes(deltas) {
		return meshoptControlZero
	}
	headerSize := (len(deltas)/16 + 3) / 4
	est0, est1 := headerSize, headerSize
	var group [16]uint8
	for g := 0; g < len(deltas); g += 16 {
		copy(group[:], deltas[g:])
		size124 := min(meshoptGroupSize(&group, 1), meshoptGroupSize(&group, 2), meshoptGroupSize(&group, 4))
		est0 += min(size124, meshoptGroupSize(&group, 0))
		est1 += min(size124, 16)
	}
	if est0 < n || est1 < n {
		if est0 < est1 {
			return 0
		}
		return 1
	}
	return meshoptControlLiteral
}

// appendDataBlockV1 appends the data block of one byte position: the 2-bit
// group headers followed by each group encoded with the smallest bit width
// available to the control mode.
func appendDataBlockV1(out []byte, deltas []byte, ctrl int) []byte {
	widths := meshoptControlBits[ctrl]
	groupCount := len(deltas) / 16

	var zeroes [16]byte
	headerStart := len(out)
	out = append(out, zeroes[:(groupCount+3)/4]...)

	lastBits := -1
	var group [16]uint8
	for g := 0; g < groupCount; g++ {
		copy(group[:], deltas[g*16:])
		best := 3
		bestSize := meshoptGroupSize(&group, widths[best])
		for w := 0; w < 3; w++ {
			size := meshoptGroupSize(&group, widths[w])
			// favor consistent bit widths across groups, but never replace
			// literal groups, like the reference encoder
			if size < bestSize || (size == bestSize && widths[w] == lastBits && widths[best] != 8) {
				best, bestSize = w, size
			}
		}
		out[headerStart+g/4] |= byte(best << ((g % 4) * 2))
		out = appendMeshoptGroup(out, &group, widths[best])
		lastBits = widths[best]
	}
	return out
}

// meshoptGroupSize returns the encoded size of a group of 16 deltas with the
// given bit width; 0 bits can only encode all-zero groups, so otherwise it
// returns a size larger than any real encoding.
func meshoptGroupSize(group *[16]uint8, width int) int {
	switch width {
	case 0:
		if allZeroBytes(group[:]) {
			return 0
		}
		return 1 << 30
	case 8:
		return 16
	}
	size := 16 * width / 8
	sentinel := uint8(1<<width - 1)
	for _, d := range group {
		if d >= sentinel {
			size++
		}
	}
	return size
}

func appendMeshoptGroup(out []byte, group *[16]uint8, width int) []byte {
	switch width {
	case 0:
		return out
	case 1:
		return append1BitSentinel(out, group)
	case 2:
		return append2BitSentinel(out, group)
	case 4:
		return append4BitSentinel(out, group)
	default:
		return append(out, group[:]...)
	}
}

// append1BitSentinel writes the 1-bit sentinel encoding of a group: one bit
// per delta, least significant bit first, where a set bit means the delta is
// stored as a full byte after the two packed bytes.
func append1BitSentinel(out []byte, deltas *[16]uint8) []byte {
	var packed [2]byte
	var extrasBuf [16]byte
	extrasCount := 0
	for i, d := range deltas {
		if d != 0 {
			packed[i/8] |= 1 << (i % 8)
			extrasBuf[extrasCount] = d
			extrasCount++
		}
	}
	out = append(out, packed[:]...)
	return append(out, extrasBuf[:extrasCount]...)
}

// estimateMeshoptRotate picks, for the 4-byte channel starting at byte k, the
// rotation that concentrates the changing bits of XOR deltas into as few
// bytes as possible.
func estimateMeshoptRotate(data []byte, count, stride, k int) int {
	var sizes [8]int
	last := binary.LittleEndian.Uint32(data[k:])
	for i := 0; i < count; i += 16 {
		// bits changing anywhere in the group
		var changed uint32
		for j := i; j < i+16 && j < count; j++ {
			v := binary.LittleEndian.Uint32(data[j*stride+k:])
			changed |= v ^ last
			last = v
		}
		for r := range sizes {
			rotated := bits.RotateLeft32(changed, r)
			sizes[r] += estimateMeshoptBits(byte(rotated)) + estimateMeshoptBits(byte(rotated>>8)) +
				estimateMeshoptBits(byte(rotated>>16)) + estimateMeshoptBits(byte(rotated>>24))
		}
	}
	best := 0
	for r := 1; r < len(sizes); r++ {
		if sizes[r] < sizes[best] {
			best = r
		}
	}
	return best
}

func estimateMeshoptBits(v byte) int {
	switch {
	case v == 0:
		return 0
	case v <= 3:
		return 2
	case v <= 15:
		return 4
	default:
		return 8
	}
}

// estimateMeshoptChannel picks the channel mode of the 4-byte channel starting
// at byte k by measuring every third block with each mode.
func estimateMeshoptChannel(data []byte, count, stride, k, blockElements, rot int) byte {
	const blockSkip = 3
	var sizes [3]int
	var lastBuf [256]byte
	last := lastBuf[:stride]
	var deltas [meshoptVertexBlockMaxSize]byte
	var group [16]uint8
	for i := 0; i < count; i += blockElements * blockSkip {
		n := min(blockElements, count-i)
		aligned := (n + 15) &^ 15
		src := max(i-1, 0)
		copy(last, data[src*stride:(src+1)*stride])
		clear(deltas[n:aligned])
		block := data[i*stride : (i+n)*stride]
		for mode := range sizes {
			channel := byte(mode)
			if mode == meshoptChannelXor {
				channel |= byte(rot << 4)
			}
			for j := 0; j < 4; j++ {
				meshoptDeltasV1(deltas[:], block, n, stride, last, k+j, channel)
				for g := 0; g < n; g += 16 {
					copy(group[:], deltas[g:])
					sizes[mode] += min(meshoptGroupSize(&group, 1), meshoptGroupSize(&group, 2),
						meshoptGroupSize(&group, 4), 16)
				}
			}
		}
	}
	best := meshoptChannelBytes
	for mode := 1; mode < len(sizes); mode++ {
		if sizes[mode] < sizes[best] {
			best = mode
		}
	}
	if best == meshoptChannelXor {
		return byte(meshoptChannelXor | rot<<4)
	}
	return byte(best)
}

// zigzag16 maps signed 16-bit deltas to unsigned values, small magnitudes to
// small values: encode(v) = (v&0x8000!=0) ? ^(v<<1) : (v<<1).
func zigzag16(v uint16) uint16 {
	return (v << 1) ^ uint16(int16(v)>>15)
}

func allZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
