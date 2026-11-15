// Package mesh holds the data model of textured triangle meshes that the mesh
// tiler works on.
package mesh

import (
	"encoding/binary"
	"math"
)

// Triangle is a textured triangle that stands on its own: it carries the
// position and texture coordinates of its three corners, so that triangles
// can be stored, split and moved between files without a shared vertex list.
type Triangle struct {
	// P holds the corners, relative to the origin of the mesh.
	P [3][3]float32
	// UV holds the texture coordinates of the corners in the image of
	// Material, with V growing upward as in OBJ files.
	UV [3][2]float32
	// Material indexes the image the texture coordinates refer to.
	Material uint32
}

// TriangleSize is the size in bytes of an encoded Triangle.
const TriangleSize = 64

// Encode writes t to dst, which must hold TriangleSize bytes.
func (t *Triangle) Encode(dst []byte) {
	o := 0
	for c := range 3 {
		for a := range 3 {
			binary.LittleEndian.PutUint32(dst[o:], math.Float32bits(t.P[c][a]))
			o += 4
		}
	}
	for c := range 3 {
		for a := range 2 {
			binary.LittleEndian.PutUint32(dst[o:], math.Float32bits(t.UV[c][a]))
			o += 4
		}
	}
	binary.LittleEndian.PutUint32(dst[o:], t.Material)
}

// Decode reads t from src, which must hold TriangleSize bytes.
func (t *Triangle) Decode(src []byte) {
	o := 0
	for c := range 3 {
		for a := range 3 {
			t.P[c][a] = math.Float32frombits(binary.LittleEndian.Uint32(src[o:]))
			o += 4
		}
	}
	for c := range 3 {
		for a := range 2 {
			t.UV[c][a] = math.Float32frombits(binary.LittleEndian.Uint32(src[o:]))
			o += 4
		}
	}
	t.Material = binary.LittleEndian.Uint32(src[o:])
}

// Degenerate reports whether two corners of t coincide, so that it covers no
// surface.
func (t *Triangle) Degenerate() bool {
	return t.P[0] == t.P[1] || t.P[1] == t.P[2] || t.P[0] == t.P[2]
}

// Area returns the surface of t.
func (t *Triangle) Area() float64 {
	return Area(t.P[0], t.P[1], t.P[2])
}

// Area returns the surface of the triangle a, b, c.
func Area(a, b, c [3]float32) float64 {
	n := Normal(a, b, c)
	return 0.5 * math.Sqrt(n[0]*n[0]+n[1]*n[1]+n[2]*n[2])
}

// Normal returns the cross product of the edges a→b and a→c: a vector along
// the normal of the triangle, twice its surface long.
func Normal(a, b, c [3]float32) [3]float64 {
	ux, uy, uz := float64(b[0])-float64(a[0]), float64(b[1])-float64(a[1]), float64(b[2])-float64(a[2])
	vx, vy, vz := float64(c[0])-float64(a[0]), float64(c[1])-float64(a[1]), float64(c[2])-float64(a[2])
	return [3]float64{uy*vz - uz*vy, uz*vx - ux*vz, ux*vy - uy*vx}
}

// Material describes the surface of the triangles that refer to it.
type Material struct {
	Name string
	// Texture is the path of the color image, or empty for a plain color.
	Texture string
	// Color is the plain color, used when there is no image.
	Color [3]uint8
}

// Vertex is a corner of a triangle of an Indexed mesh: a position with the
// texture coordinates it has in one image. A position on a texture seam has
// one vertex for each side of the seam.
type Vertex struct {
	// Position indexes the positions of the mesh.
	Position int32
	UV       [2]float32
	Material uint32
}

// Indexed is a mesh whose triangles share their vertices.
type Indexed struct {
	Positions [][3]float32
	Vertices  []Vertex
	// Triangles index Vertices.
	Triangles [][3]int32
}

// Weld returns the mesh of triangles, whose corners share a position when
// they are at the same point, and a vertex when they also have the same
// texture coordinates and material.
func Weld(triangles []Triangle) *Indexed {
	m := &Indexed{Triangles: make([][3]int32, 0, len(triangles))}
	positions := make(map[[3]float32]int32, len(triangles)/2)
	vertices := make(map[Vertex]int32, len(triangles))
	for i := range triangles {
		t := &triangles[i]
		var corners [3]int32
		for c := range 3 {
			position, ok := positions[t.P[c]]
			if !ok {
				position = int32(len(m.Positions))
				positions[t.P[c]] = position
				m.Positions = append(m.Positions, t.P[c])
			}
			v := Vertex{Position: position, UV: t.UV[c], Material: t.Material}
			index, ok := vertices[v]
			if !ok {
				index = int32(len(m.Vertices))
				vertices[v] = index
				m.Vertices = append(m.Vertices, v)
			}
			corners[c] = index
		}
		m.Triangles = append(m.Triangles, corners)
	}
	return m
}

// WeldPositions returns the surface of triangles without its texture: a
// mesh whose corners share a vertex when they are at the same point,
// whatever their texture coordinates and material, which it leaves out.
func WeldPositions(triangles []Triangle) *Indexed {
	m := &Indexed{Triangles: make([][3]int32, 0, len(triangles))}
	positions := make(map[[3]float32]int32, len(triangles)/2)
	for i := range triangles {
		var corners [3]int32
		for c, p := range triangles[i].P {
			position, ok := positions[p]
			if !ok {
				position = int32(len(m.Positions))
				positions[p] = position
				m.Positions = append(m.Positions, p)
				m.Vertices = append(m.Vertices, Vertex{Position: position})
			}
			corners[c] = position
		}
		m.Triangles = append(m.Triangles, corners)
	}
	return m
}

// Soup returns the triangles of m as triangles that stand on their own.
func (m *Indexed) Soup() []Triangle {
	out := make([]Triangle, len(m.Triangles))
	for i, corners := range m.Triangles {
		t := &out[i]
		for c, index := range corners {
			v := m.Vertices[index]
			t.P[c], t.UV[c] = m.Positions[v.Position], v.UV
			t.Material = v.Material
		}
	}
	return out
}
