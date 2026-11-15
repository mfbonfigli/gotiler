package compression

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// decodeMeshoptTriangles decodes a TRIANGLES stream of count indices, as the
// EXT_meshopt_compression extension describes its decoder.
func decodeMeshoptTriangles(t *testing.T, stream []byte, count int) []uint32 {
	t.Helper()
	if len(stream) < 1+count/3+meshoptTableSize || stream[0] != meshoptIndexHeader {
		t.Fatalf("stream of %d bytes for %d indices, header %#x", len(stream), count, stream[0])
	}
	codes := stream[1 : 1+count/3]
	data := stream[1+count/3 : len(stream)-meshoptTableSize]
	table := stream[len(stream)-meshoptTableSize:]
	var edges [16][2]uint32
	var vertices [16]uint32
	edgeOffset, vertexOffset := 0, 0
	pushEdge := func(a, b uint32) {
		edges[edgeOffset] = [2]uint32{a, b}
		edgeOffset = (edgeOffset + 1) & 15
	}
	pushVertex := func(v uint32) {
		vertices[vertexOffset] = v
		vertexOffset = (vertexOffset + 1) & 15
	}
	var next, last uint32
	readIndex := func() uint32 {
		var v uint32
		for shift := 0; ; shift += 7 {
			b := data[0]
			data = data[1:]
			v |= uint32(b&0x7f) << shift
			if b < 0x80 {
				break
			}
		}
		if v&1 != 0 {
			last += ^(v >> 1)
		} else {
			last += v >> 1
		}
		return last
	}
	out := make([]uint32, 0, count)
	for _, code := range codes {
		if code < 0xf0 {
			e := edges[(edgeOffset-1-int(code>>4))&15]
			a, b := e[0], e[1]
			var c uint32
			push := true
			switch low := code & 0xf; {
			case low == 0:
				c = next
				next++
			case low < 0xd:
				c = vertices[(vertexOffset-1-int(low))&15]
				push = false
			case low == 0xd:
				last--
				c = last
			case low == 0xe:
				last++
				c = last
			default:
				c = readIndex()
			}
			pushEdge(c, b)
			pushEdge(a, c)
			if push {
				pushVertex(c)
			}
			out = append(out, a, b, c)
			continue
		}
		var zw byte
		var a uint32
		explicit := code >= 0xfe
		if explicit {
			zw = data[0]
			data = data[1:]
			if zw == 0 {
				next = 0
			}
		} else {
			zw = table[code&0xf]
		}
		if code == 0xff {
			a = readIndex()
		} else {
			a = next
			next++
		}
		corner := func(nibble byte) (uint32, bool) {
			switch {
			case nibble == 0:
				next++
				return next - 1, true
			case nibble == 15 && explicit:
				return readIndex(), true
			}
			return vertices[(vertexOffset-int(nibble))&15], false
		}
		b, newB := corner(zw >> 4)
		c, newC := corner(zw & 0xf)
		pushEdge(b, a)
		pushEdge(c, b)
		pushEdge(a, c)
		pushVertex(a)
		if newB {
			pushVertex(b)
		}
		if newC {
			pushVertex(c)
		}
		out = append(out, a, b, c)
	}
	if len(data) != 0 {
		t.Fatalf("%d bytes of data left unread", len(data))
	}
	return out
}

// sameTriangles reports whether got lists the triangles of want, each from
// any of its corners.
func sameTriangles(got, want []uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := 0; i < len(want); i += 3 {
		a, b, c := want[i], want[i+1], want[i+2]
		g := [3]uint32{got[i], got[i+1], got[i+2]}
		if g != [3]uint32{a, b, c} && g != [3]uint32{b, c, a} && g != [3]uint32{c, a, b} {
			return false
		}
	}
	return true
}

func TestEncodeMeshoptTrianglesRoundTrips(t *testing.T) {
	// A grid of quads, row by row: neighbors share edges and vertices come
	// in increasing order, as in the tiles.
	var grid []uint32
	const side = 40
	for y := range side {
		for x := range side {
			v := uint32(y*(side+1) + x)
			grid = append(grid, v, v+1, v+side+2, v, v+side+2, v+side+1)
		}
	}
	random := rand.New(rand.NewPCG(7, 9))
	scattered := make([]uint32, 3*500)
	for i := 0; i < len(scattered); i += 3 {
		for {
			a, b, c := random.Uint32N(300), random.Uint32N(300), random.Uint32N(300)
			if a != b && b != c && a != c {
				scattered[i], scattered[i+1], scattered[i+2] = a, b, c
				break
			}
		}
	}
	shuffled := slices.Clone(grid)
	for i := len(shuffled)/3 - 1; i > 0; i-- {
		j := random.IntN(i + 1)
		for k := range 3 {
			shuffled[3*i+k], shuffled[3*j+k] = shuffled[3*j+k], shuffled[3*i+k]
		}
	}
	for name, indices := range map[string][]uint32{
		"grid":      grid,
		"scattered": scattered,
		"shuffled":  shuffled,
		"single":    {5, 3, 4},
		"large":     {0, 1, 2, 70000, 2, 1, 4000000000, 70000, 1},
		"empty":     nil,
	} {
		stream := EncodeMeshoptTriangles(nil, indices)
		if got := decodeMeshoptTriangles(t, stream, len(indices)); !sameTriangles(got, indices) {
			t.Errorf("%s: the triangles do not survive a round trip", name)
		}
		// Plain 16 bit indices take 6 bytes a triangle.
		if name == "grid" && len(stream) > len(indices)/3*5/2 {
			t.Errorf("grid: %d bytes for %d triangles, want at most 2.5 each", len(stream), len(indices)/3)
		}
	}
	table := EncodeMeshoptTriangles(nil, grid)
	if !slices.Equal(table[len(table)-meshoptTableSize:], make([]byte, meshoptTableSize)) {
		t.Error("the table at the end of the stream is not zero")
	}
}
