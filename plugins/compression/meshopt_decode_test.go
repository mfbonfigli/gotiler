package compression

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// meshoptDecodeStats counts the encoding features met while decoding.
type meshoptDecodeStats struct {
	controls [4]int // version 1 control modes, one per byte position and block
	widths   [9]int // group bit widths
	channels [3]int // version 1 channel modes
}

// decodeMeshoptAttributes decodes a meshopt ATTRIBUTES stream, version 0
// (EXT_meshopt_compression) or 1 (KHR_meshopt_compression), holding count
// elements of stride bytes. It is a straightforward, independent
// implementation of the bitstream specification, including its validity
// rules, used to check the encoders.
func decodeMeshoptAttributes(src []byte, count, stride int) ([]byte, error) {
	return decodeMeshoptAttributesStats(src, count, stride, &meshoptDecodeStats{})
}

// decodeMeshoptAttributesStats is decodeMeshoptAttributes also recording the
// features of the stream into stats.
func decodeMeshoptAttributesStats(src []byte, count, stride int, stats *meshoptDecodeStats) ([]byte, error) {
	if len(src) < 1 {
		return nil, fmt.Errorf("empty stream")
	}
	if src[0]&0xf0 != 0xa0 {
		return nil, fmt.Errorf("invalid header byte %#x", src[0])
	}
	version := int(src[0] & 0x0f)
	if version > 1 {
		return nil, fmt.Errorf("unsupported version %d", version)
	}

	tailSize, tailMin := stride, 32
	if version == 1 {
		tailSize, tailMin = stride+stride/4, 24
	}
	tailPadded := max(tailSize, tailMin)
	if len(src)-1 < tailPadded {
		return nil, fmt.Errorf("stream too short for its tail")
	}
	tail := src[len(src)-tailSize:]
	last := append([]byte(nil), tail[:stride]...)
	var channels []byte
	if version == 1 {
		channels = tail[stride:]
		for _, c := range channels {
			mode := c & 0x0f
			if mode > 2 || (mode < 2 && c>>4 != 0) {
				return nil, fmt.Errorf("invalid channel mode byte %#x", c)
			}
			stats.channels[mode]++
		}
	}

	// the attribute blocks must end exactly where the padded tail begins
	end := len(src) - tailPadded
	pos := 1
	read := func(n int) ([]byte, error) {
		if pos+n > end {
			return nil, fmt.Errorf("stream truncated at byte %d", pos)
		}
		b := src[pos : pos+n]
		pos += n
		return b, nil
	}

	out := make([]byte, count*stride)
	blockElements := min((8192/stride)&^15, 256)
	for offset := 0; offset < count; {
		n := min(count-offset, blockElements)
		aligned := (n + 15) &^ 15

		var control []byte
		if version == 1 {
			var err error
			if control, err = read(stride / 4); err != nil {
				return nil, err
			}
		}

		// decoded delta bytes of each byte position
		deltas := make([][]byte, stride)
		for k := 0; k < stride; k++ {
			ctrl := 0
			if version == 1 {
				ctrl = int(control[k/4]>>((k%4)*2)) & 3
				stats.controls[ctrl]++
			}
			switch ctrl {
			case 3:
				literal, err := read(n)
				if err != nil {
					return nil, err
				}
				deltas[k] = literal
			case 2:
				deltas[k] = make([]byte, n)
			default:
				widths := [4]int{0, 2, 4, 8}
				if version == 1 {
					widths = [2][4]int{{0, 1, 2, 4}, {1, 2, 4, 8}}[ctrl]
				}
				groups := aligned / 16
				header, err := read((groups + 3) / 4)
				if err != nil {
					return nil, err
				}
				decoded := make([]byte, aligned)
				for g := 0; g < groups; g++ {
					width := widths[int(header[g/4]>>((g%4)*2))&3]
					stats.widths[width]++
					if err := decodeMeshoptGroup(read, decoded[g*16:g*16+16], width); err != nil {
						return nil, err
					}
				}
				deltas[k] = decoded[:n]
			}
		}

		for c := 0; c < stride/4; c++ {
			channel := byte(0)
			if version == 1 {
				channel = channels[c]
			}
			for i := 0; i < n; i++ {
				element := out[(offset+i)*stride:]
				switch channel & 0x0f {
				case 0:
					for j := 0; j < 4; j++ {
						k := c*4 + j
						d := deltas[k][i]
						v := last[k] + ((d >> 1) ^ -(d & 1)) // unzigzag
						element[k] = v
						last[k] = v
					}
				case 1:
					for h := 0; h < 4; h += 2 {
						k := c*4 + h
						d := uint16(deltas[k][i]) | uint16(deltas[k+1][i])<<8
						v := binary.LittleEndian.Uint16(last[k:]) + ((d >> 1) ^ -(d & 1))
						binary.LittleEndian.PutUint16(element[k:], v)
						binary.LittleEndian.PutUint16(last[k:], v)
					}
				case 2:
					k := c * 4
					d := uint32(deltas[k][i]) | uint32(deltas[k+1][i])<<8 | uint32(deltas[k+2][i])<<16 | uint32(deltas[k+3][i])<<24
					v := bits.RotateLeft32(d, -int(channel>>4)) ^ binary.LittleEndian.Uint32(last[k:])
					binary.LittleEndian.PutUint32(element[k:], v)
					binary.LittleEndian.PutUint32(last[k:], v)
				}
			}
		}
		offset += n
	}

	if pos != end {
		return nil, fmt.Errorf("%d unprocessed bytes before the tail", end-pos)
	}
	return out, nil
}

// decodeMeshoptGroup decodes one group of 16 delta bytes stored with the
// given bit width, using read to consume the stream.
func decodeMeshoptGroup(read func(int) ([]byte, error), dst []byte, width int) error {
	switch width {
	case 0:
		clear(dst)
		return nil
	case 8:
		raw, err := read(16)
		if err != nil {
			return err
		}
		copy(dst, raw)
		return nil
	}
	packed, err := read(16 * width / 8)
	if err != nil {
		return err
	}
	sentinel := byte(1<<width - 1)
	perByte := 8 / width
	for i := 0; i < 16; i++ {
		var shift int
		if width == 1 {
			// 1-bit deltas are packed least significant bit first
			shift = i % 8
		} else {
			// 2-bit and 4-bit deltas are packed most significant bits first
			shift = 8 - width*(i%perByte+1)
		}
		v := (packed[i/perByte] >> shift) & sentinel
		if v == sentinel {
			extra, err := read(1)
			if err != nil {
				return err
			}
			v = extra[0]
		}
		dst[i] = v
	}
	return nil
}
