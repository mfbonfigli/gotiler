package compression

// The TRIANGLES mode of the meshopt bitstream, defined by the
// EXT_meshopt_compression extension: one code byte per triangle, then the
// data bytes that some codes need, then a table of 16 bytes.
//
// The decoder keeps the 16 edges and the 16 vertices it saw last, and the
// number of vertices it met so far, next. A code names the triangle by an
// edge it shares with a recent one and a third vertex that is the next new
// one, a recent one, or an index stored in the data. A triangle that shares
// no recent edge names its three vertices that way.

const (
	meshoptIndexHeader = 0xe1
	meshoptFifoSize    = 16
	// meshoptTableSize is the size of the table at the end of the stream,
	// which gives a meaning to the codes 0xf0 to 0xfd. Only its first entry
	// is used here: 0, a triangle of three new vertices.
	meshoptTableSize = 16
	// noIndex fills the lists of recent edges and vertices before any is
	// seen.
	noIndex = ^uint32(0)
)

// meshoptFifo is the list of the values seen last, the most recent first.
type meshoptFifo[T comparable] struct {
	values [meshoptFifoSize]T
	offset int
}

func (f *meshoptFifo[T]) push(v T) {
	f.values[f.offset] = v
	f.offset = (f.offset + 1) % meshoptFifoSize
}

// find returns the position of v among the first limit values, or -1.
func (f *meshoptFifo[T]) find(v T, limit int) int {
	for i := range limit {
		if f.values[(f.offset-1-i+2*meshoptFifoSize)%meshoptFifoSize] == v {
			return i
		}
	}
	return -1
}

// EncodeMeshoptTriangles appends the EXT_meshopt_compression encoding, in
// the TRIANGLES mode, of the triangles whose corners indices lists, three
// each, to dst, and returns the extended slice.
//
// The decoder may give the corners of a triangle from another of them: the
// winding stays the same. Triangles compress best when each shares an edge
// with one of the few before it and new vertices appear in increasing
// order.
func EncodeMeshoptTriangles(dst []byte, indices []uint32) []byte {
	var edges meshoptFifo[[2]uint32]
	var vertices meshoptFifo[uint32]
	for i := range edges.values {
		edges.values[i] = [2]uint32{noIndex, noIndex}
		vertices.values[i] = noIndex
	}
	var next, last uint32
	dst = append(dst, meshoptIndexHeader)
	codes := len(dst)
	dst = append(dst, make([]byte, len(indices)/3)...)
	// index appends an index to the data, as its difference from the last
	// one stored.
	index := func(v uint32) {
		d := int32(v - last)
		z := uint32(d<<1) ^ uint32(d>>31)
		for z > 0x7f {
			dst = append(dst, byte(z)|0x80)
			z >>= 7
		}
		dst = append(dst, byte(z))
		last = v
	}
	for t := 0; t < len(indices)/3; t++ {
		a, b, c := indices[3*t], indices[3*t+1], indices[3*t+2]
		// Start from an edge the decoder holds, if any: the codes 0x0 to 0xe
		// name the 15 most recent.
		found := -1
		for range 3 {
			if found = edges.find([2]uint32{a, b}, meshoptFifoSize-1); found >= 0 {
				break
			}
			a, b, c = b, c, a
		}
		if found >= 0 {
			code := byte(found << 4)
			recent := vertices.find(c, 13)
			switch {
			case c == next:
				next++
				vertices.push(c)
			case recent >= 1:
				code |= byte(recent)
			case c == last-1:
				code |= 0xd
				last = c
				vertices.push(c)
			case c == last+1:
				code |= 0xe
				last = c
				vertices.push(c)
			default:
				code |= 0xf
				index(c)
				vertices.push(c)
			}
			dst[codes+t] = code
			edges.push([2]uint32{c, b})
			edges.push([2]uint32{a, c})
			continue
		}
		// No edge to start from: name all three vertices, the first a new
		// one if the triangle has any.
		for range 3 {
			if a == next {
				break
			}
			a, b, c = b, c, a
		}
		// describe returns how the decoder finds v, after the corners before
		// it: 0 for the next new vertex, 1 to 14 for a recent one, 15 for an
		// index in the data.
		upcoming := next
		if a == next {
			upcoming++
		}
		describe := func(v uint32) byte {
			if v == upcoming {
				upcoming++
				return 0
			}
			if recent := vertices.find(v, 14); recent >= 0 {
				return byte(recent + 1)
			}
			return 15
		}
		z := describe(b)
		w := describe(c)
		switch {
		case a == next && z == 0 && w == 0:
			// Three new vertices: the first entry of the table.
			dst[codes+t] = 0xf0
		case a == next:
			dst[codes+t] = 0xfe
			dst = append(dst, z<<4|w)
		default:
			dst[codes+t] = 0xff
			dst = append(dst, z<<4|w)
			index(a)
		}
		if z == 15 {
			index(b)
		}
		if w == 15 {
			index(c)
		}
		next = upcoming
		edges.push([2]uint32{b, a})
		edges.push([2]uint32{c, b})
		edges.push([2]uint32{a, c})
		vertices.push(a)
		if z == 0 || z == 15 {
			vertices.push(b)
		}
		if w == 0 || w == 15 {
			vertices.push(c)
		}
	}
	return append(dst, make([]byte, meshoptTableSize)...)
}
