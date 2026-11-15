// Package simplify reduces the triangles of textured meshes.
package simplify

import (
	"container/heap"
	"math"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

const (
	// edgeWeight scales the pull that keeps open borders and texture seams
	// on their line, against the pull of the surface.
	edgeWeight = 4
	// minNormalCosine is the least cosine between the normals of a triangle
	// before and after a collapse: sharper turns fold the surface.
	minNormalCosine = 0.25
	// minDriftCosine is the least cosine between the normal of a triangle
	// and the one it had before any collapse: 60 degrees. It keeps a run of
	// collapses, each a small turn, from tilting what was flat.
	minDriftCosine = 0.5
	// lengthBias orders collapses of equal error from the shortest edge, so
	// that flat regions keep triangles of even size.
	lengthBias = 1e-4
	// maxSeamVertices is the most vertices a position can have and still
	// move: more means that several seams meet there.
	maxSeamVertices = 2
)

// Simplify collapses edges of m until it has at most target triangles, or no
// edge is left that can collapse within maxError, and returns how far the
// surface lies from where it started, at most. A maxError of 0 sets no
// limit.
//
// moved holds, for each position of m, how far the surface around it
// already lies from where it started, as earlier simplifications left it:
// the limit holds for the total, and Simplify updates it. A nil moved
// stands for a surface that has not moved.
//
// The distance a collapse moves the surface by is measured, not averaged:
// it is the farthest that the position that takes over lies from the planes
// of the triangles it stretches, and from the border it straightens, added
// to what earlier collapses onto the moved position cost. A collapse over
// the limit is left out, so the shape of the surface is kept within
// maxError however far the target is: flat parts go down to a few
// triangles, and edges, corners and thin parts stay.
//
// A collapse moves a position onto a neighbor, never to a new point, so the
// texture coordinates that remain are those of the input and the texture
// needs no remapping. To keep it in place:
//
//   - positions on a texture seam, which have a vertex on each side, only
//     move along the seam, with both vertices;
//   - positions on an open border only move along the border;
//   - positions where locked is true do not move, though others can move
//     onto them.
//
// A collapse must not turn a triangle over, tilt it far from where it
// started, or join parts of the surface that only come near each other.
//
// It leaves the positions and vertices of m as they are, some unused, and
// replaces its triangles.
func Simplify(m *mesh.Indexed, locked []bool, moved []float64, target int, maxError float64) float64 {
	s := newSimplifier(m, locked)
	if moved != nil {
		s.moved = moved
		for _, t := range m.Triangles {
			for _, v := range t {
				s.maxMoved = math.Max(s.maxMoved, moved[m.Vertices[v].Position])
			}
		}
	}
	if s.limit = maxError; !(maxError > 0) {
		s.limit = math.Inf(1)
	}
	for s.count > target {
		collapsed := 0
		for s.count > target && s.queue.Len() > 0 {
			c := heap.Pop(&s.queue).(candidate)
			if c.stampFrom != s.stamp[c.from] || c.stampTo != s.stamp[c.to] {
				continue
			}
			if partners, moved, ok := s.partners(c.from, c.to); ok {
				s.collapse(c, partners, moved)
				collapsed++
			}
		}
		// A collapse that was refused can become sound once those around
		// it are done: try the edges that are left again, as long as some
		// collapse.
		if collapsed == 0 {
			break
		}
		s.requeue()
	}
	kept := m.Triangles[:0]
	for i, t := range m.Triangles {
		if s.alive[i] {
			kept = append(kept, t)
		}
	}
	m.Triangles = kept
	return s.maxMoved
}

// quadric is the weighted sum of the squared distances to a set of planes,
// as a function of the point: x·A·x + 2·b·x + c.
type quadric struct {
	a00, a01, a02, a11, a12, a22 float64
	b0, b1, b2                   float64
	c                            float64
	weight                       float64
}

// addPlane adds the plane through point with the unit normal n.
func (q *quadric) addPlane(n, point [3]float64, weight float64) {
	d := -(n[0]*point[0] + n[1]*point[1] + n[2]*point[2])
	q.a00 += weight * n[0] * n[0]
	q.a01 += weight * n[0] * n[1]
	q.a02 += weight * n[0] * n[2]
	q.a11 += weight * n[1] * n[1]
	q.a12 += weight * n[1] * n[2]
	q.a22 += weight * n[2] * n[2]
	q.b0 += weight * n[0] * d
	q.b1 += weight * n[1] * d
	q.b2 += weight * n[2] * d
	q.c += weight * d * d
	q.weight += weight
}

func (q *quadric) add(o *quadric) {
	q.a00 += o.a00
	q.a01 += o.a01
	q.a02 += o.a02
	q.a11 += o.a11
	q.a12 += o.a12
	q.a22 += o.a22
	q.b0 += o.b0
	q.b1 += o.b1
	q.b2 += o.b2
	q.c += o.c
	q.weight += o.weight
}

// meanSquare returns the weighted mean of the squared distances from p to
// the planes of q.
func (q *quadric) meanSquare(p [3]float64) float64 {
	if q.weight <= 0 {
		return 0
	}
	x, y, z := p[0], p[1], p[2]
	e := q.a00*x*x + q.a11*y*y + q.a22*z*z + 2*(q.a01*x*y+q.a02*x*z+q.a12*y*z) + 2*(q.b0*x+q.b1*y+q.b2*z) + q.c
	return math.Max(e, 0) / q.weight
}

// candidate is the collapse of the position from onto the position to.
type candidate struct {
	key                float64
	from, to           int32
	stampFrom, stampTo uint32
}

type queue []candidate

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].key != q[j].key {
		return q[i].key < q[j].key
	}
	if q[i].from != q[j].from {
		return q[i].from < q[j].from
	}
	return q[i].to < q[j].to
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(candidate)) }
func (q *queue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type simplifier struct {
	m *mesh.Indexed
	// origin is subtracted from the positions, so that the quadrics hold
	// small numbers.
	origin [3]float64
	alive  []bool
	count  int
	// triangles lists, for each position, the triangles around it. Dead ones
	// are dropped when the list is next read.
	triangles [][]int32
	quadrics  []quadric
	locked    []bool
	border    []bool
	// stamp counts the changes around each position: candidates made before
	// the last one are stale.
	stamp []uint32
	queue queue
	// normals holds the unit normal each triangle had at the start, or zero
	// for one without a surface.
	normals [][3]float64
	// moved holds, for each position, how far the surface around it lies
	// from where it started, at most; maxMoved is the largest of them, and
	// limit the most that a collapse can make it.
	moved           []float64
	maxMoved, limit float64
	scratch         []int32
	counts          []int32
}

type edge struct{ low, high int32 }

type edgeUse struct {
	count int
	// first is the triangle the edge was first seen in, with its vertices at
	// the low and high positions.
	first               int32
	lowVertex, highVert int32
	seam                bool
}

func newSimplifier(m *mesh.Indexed, locked []bool) *simplifier {
	s := &simplifier{
		m:         m,
		alive:     make([]bool, len(m.Triangles)),
		count:     len(m.Triangles),
		triangles: make([][]int32, len(m.Positions)),
		quadrics:  make([]quadric, len(m.Positions)),
		locked:    make([]bool, len(m.Positions)),
		border:    make([]bool, len(m.Positions)),
		stamp:     make([]uint32, len(m.Positions)),
		normals:   make([][3]float64, len(m.Triangles)),
		moved:     make([]float64, len(m.Positions)),
	}
	copy(s.locked, locked)
	if len(m.Positions) > 0 {
		p := m.Positions[m.Vertices[0].Position]
		s.origin = [3]float64{float64(p[0]), float64(p[1]), float64(p[2])}
	}
	edges := make(map[edge]*edgeUse, len(m.Triangles)*3/2)
	var order []edge
	for i, t := range m.Triangles {
		s.alive[i] = true
		a, b, c := s.point(s.position(t[0])), s.point(s.position(t[1])), s.point(s.position(t[2]))
		n, area := unit(cross(sub(b, a), sub(c, a)))
		if area > 0 {
			s.normals[i] = n
		}
		for k := range 3 {
			p := s.position(t[k])
			s.triangles[p] = append(s.triangles[p], int32(i))
			if area > 0 {
				s.quadrics[p].addPlane(n, a, area/2)
			}
			v, w := t[k], t[(k+1)%3]
			e := edge{s.position(v), s.position(w)}
			if e.low > e.high {
				e.low, e.high, v, w = e.high, e.low, w, v
			}
			use := edges[e]
			if use == nil {
				use = &edgeUse{first: int32(i), lowVertex: v, highVert: w}
				edges[e] = use
				order = append(order, e)
			} else if use.lowVertex != v || use.highVert != w {
				use.seam = true
			}
			use.count++
		}
	}
	for _, e := range order {
		use := edges[e]
		switch {
		case use.count > 2:
			// More than two triangles share the edge: leave it alone.
			s.locked[e.low], s.locked[e.high] = true, true
			continue
		case use.count == 1:
			s.border[e.low], s.border[e.high] = true, true
		case !use.seam:
			continue
		}
		// Borders and seams stay on their line: add the plane through the
		// edge, across the triangle.
		t := m.Triangles[use.first]
		a, b, c := s.point(s.position(t[0])), s.point(s.position(t[1])), s.point(s.position(t[2]))
		low, high := s.point(e.low), s.point(e.high)
		along := sub(high, low)
		n, length := unit(cross(along, cross(sub(b, a), sub(c, a))))
		if length > 0 {
			weight := edgeWeight * dot(along, along)
			s.quadrics[e.low].addPlane(n, low, weight)
			s.quadrics[e.high].addPlane(n, low, weight)
		}
	}
	for _, e := range order {
		s.push(e.low, e.high)
		s.push(e.high, e.low)
	}
	heap.Init(&s.queue)
	return s
}

func (s *simplifier) position(vertex int32) int32 { return s.m.Vertices[vertex].Position }

// requeue fills the queue with the collapses of the edges of the triangles
// that are left.
func (s *simplifier) requeue() {
	s.queue = s.queue[:0]
	type edge struct{ from, to int32 }
	seen := make(map[edge]bool, 3*s.count)
	for i, t := range s.m.Triangles {
		if !s.alive[i] {
			continue
		}
		for k := range 3 {
			for _, e := range [2]edge{{s.position(t[k]), s.position(t[(k+1)%3])}, {s.position(t[(k+1)%3]), s.position(t[k])}} {
				if !seen[e] {
					seen[e] = true
					s.push(e.from, e.to)
				}
			}
		}
	}
	heap.Init(&s.queue)
}

func (s *simplifier) point(position int32) [3]float64 {
	p := s.m.Positions[position]
	return [3]float64{float64(p[0]) - s.origin[0], float64(p[1]) - s.origin[1], float64(p[2]) - s.origin[2]}
}

// cost returns the error of collapsing from onto to.
func (s *simplifier) cost(from, to int32) float64 {
	q := s.quadrics[from]
	q.add(&s.quadrics[to])
	return q.meanSquare(s.point(to))
}

// push queues the collapse of from onto to, without ordering the queue.
func (s *simplifier) push(from, to int32) {
	if s.locked[from] {
		return
	}
	d := sub(s.point(from), s.point(to))
	s.queue = append(s.queue, candidate{
		key:       s.cost(from, to) + lengthBias*dot(d, d),
		from:      from,
		to:        to,
		stampFrom: s.stamp[from],
		stampTo:   s.stamp[to],
	})
}

// around returns the live triangles around position, dropping the dead ones
// from its list.
func (s *simplifier) around(position int32) []int32 {
	list := s.triangles[position]
	kept := list[:0]
	for _, t := range list {
		if s.alive[t] {
			kept = append(kept, t)
		}
	}
	s.triangles[position] = kept
	return kept
}

// corner returns which corner of triangle t is at position, or -1.
func (s *simplifier) corner(t, position int32) int {
	for k, v := range s.m.Triangles[t] {
		if s.position(v) == position {
			return k
		}
	}
	return -1
}

// pair maps a vertex at the collapsing position to the vertex that replaces
// it.
type pair struct{ from, to int32 }

// partners returns, for each vertex at the position from, the vertex at the
// position to that takes its place, how far the collapse leaves the surface
// from where it started, and whether it keeps the mesh and its texture
// sound, within the limit.
func (s *simplifier) partners(from, to int32) ([maxSeamVertices]pair, float64, bool) {
	var pairs [maxSeamVertices]pair
	for i := range pairs {
		pairs[i] = pair{-1, -1}
	}
	if s.locked[from] {
		return pairs, 0, false
	}
	around := s.around(from)
	shared := 0
	for _, t := range around {
		tri := s.m.Triangles[t]
		k := s.corner(t, from)
		slot := -1
		for i := range pairs {
			if pairs[i].from == tri[k] || pairs[i].from < 0 {
				slot = i
				break
			}
		}
		if slot < 0 {
			return pairs, 0, false
		}
		pairs[slot].from = tri[k]
		j := s.corner(t, to)
		if j < 0 {
			continue
		}
		shared++
		if pairs[slot].to >= 0 && pairs[slot].to != tri[j] {
			return pairs, 0, false
		}
		pairs[slot].to = tri[j]
	}
	if shared == 0 || (s.border[from] && shared != 1) {
		return pairs, 0, false
	}
	for _, p := range pairs {
		if p.from >= 0 && p.to < 0 {
			// A vertex of a seam with no neighbor on its side at to: the
			// edge leaves the seam.
			return pairs, 0, false
		}
	}
	if s.commonNeighbors(from, to) != shared {
		return pairs, 0, false
	}
	moved := s.moved[from]
	if s.border[from] {
		moved += s.borderShift(from, to)
	}
	target := s.m.Positions[to]
	var deviation float64
	for _, t := range around {
		if s.corner(t, to) >= 0 {
			continue
		}
		tri := s.m.Triangles[t]
		k := s.corner(t, from)
		var before, after [3][3]float32
		var uvBefore, uvAfter [3][2]float32
		for c, v := range tri {
			vertex := s.m.Vertices[v]
			before[c], uvBefore[c] = s.m.Positions[vertex.Position], vertex.UV
			after[c], uvAfter[c] = before[c], uvBefore[c]
		}
		after[k] = target
		for _, p := range pairs {
			if p.from == tri[k] {
				uvAfter[k] = s.m.Vertices[p.to].UV
			}
		}
		n0 := mesh.Normal(before[0], before[1], before[2])
		n1 := mesh.Normal(after[0], after[1], after[2])
		// A triangle without a surface has no side to keep.
		if dot(n0, n0) > 0 && dot(n0, n1) <= minNormalCosine*math.Sqrt(dot(n0, n0)*dot(n1, n1)) {
			return pairs, 0, false
		}
		if uvArea(uvBefore)*uvArea(uvAfter) < 0 {
			return pairs, 0, false
		}
		// The triangle now reaches to the new position: how far that lies
		// from its plane is how far it leaves it.
		if length := math.Sqrt(dot(n0, n0)); length > 0 {
			offset := [3]float64{float64(target[0]) - float64(before[0][0]), float64(target[1]) - float64(before[0][1]), float64(target[2]) - float64(before[0][2])}
			deviation = math.Max(deviation, math.Abs(dot(n0, offset))/length)
		}
		if original := s.normals[t]; original != [3]float64{} && dot(original, n1) < minDriftCosine*math.Sqrt(dot(n1, n1)) {
			return pairs, 0, false
		}
	}
	if moved += deviation; moved > s.limit {
		return pairs, 0, false
	}
	return pairs, moved, true
}

// commonNeighbors counts the positions that share an edge with both a and b.
// A collapse of the edge between them is sound only when these are the third
// corners of the triangles on the edge: any other would be pinched, and
// parts of the surface that were apart would join there.
func (s *simplifier) commonNeighbors(a, b int32) int {
	s.scratch = s.scratch[:0]
	for _, t := range s.around(a) {
		for _, v := range s.m.Triangles[t] {
			if p := s.position(v); p != a && p != b && !contains(s.scratch, p) {
				s.scratch = append(s.scratch, p)
			}
		}
	}
	neighbors := len(s.scratch)
	common := 0
	for _, t := range s.around(b) {
		for _, v := range s.m.Triangles[t] {
			p := s.position(v)
			for i := range neighbors {
				if s.scratch[i] == p {
					common++
					// Count each common neighbor once.
					s.scratch[i] = -1
				}
			}
		}
	}
	return common
}

// borderShift returns how far the border of the mesh moves when the border
// position from collapses onto to: the distance from from to the straight
// line that then joins to and the other neighbor of from along the border.
func (s *simplifier) borderShift(from, to int32) float64 {
	// The neighbors along the border are those in one triangle only.
	s.scratch, s.counts = s.scratch[:0], s.counts[:0]
	for _, t := range s.around(from) {
		for _, v := range s.m.Triangles[t] {
			p := s.position(v)
			if p == from {
				continue
			}
			found := false
			for i, q := range s.scratch {
				if q == p {
					s.counts[i]++
					found = true
				}
			}
			if !found {
				s.scratch, s.counts = append(s.scratch, p), append(s.counts, 1)
			}
		}
	}
	var shift float64
	point, end := s.point(from), s.point(to)
	for i, p := range s.scratch {
		if s.counts[i] == 1 && p != to {
			shift = math.Max(shift, segmentDistance(point, s.point(p), end))
		}
	}
	return shift
}

// segmentDistance returns the distance from p to the segment from a to b.
func segmentDistance(p, a, b [3]float64) float64 {
	along, offset := sub(b, a), sub(p, a)
	t := 0.0
	if length := dot(along, along); length > 0 {
		t = math.Min(math.Max(dot(offset, along)/length, 0), 1)
	}
	d := sub(offset, [3]float64{t * along[0], t * along[1], t * along[2]})
	return math.Sqrt(dot(d, d))
}

// collapse moves the position c.from onto c.to: the triangles on the edge
// between them go, and the others around from take its partner vertices.
// The surface around to then lies up to moved from where it started.
func (s *simplifier) collapse(c candidate, pairs [maxSeamVertices]pair, moved float64) {
	s.moved[c.to] = math.Max(s.moved[c.to], moved)
	s.maxMoved = math.Max(s.maxMoved, moved)
	for _, t := range s.around(c.from) {
		if s.corner(t, c.to) >= 0 {
			s.alive[t] = false
			s.count--
			continue
		}
		k := s.corner(t, c.from)
		for _, p := range pairs {
			if p.from == s.m.Triangles[t][k] {
				s.m.Triangles[t][k] = p.to
				break
			}
		}
		s.triangles[c.to] = append(s.triangles[c.to], t)
	}
	s.triangles[c.from] = nil
	s.quadrics[c.to].add(&s.quadrics[c.from])
	s.locked[c.from] = true
	s.stamp[c.from]++
	s.stamp[c.to]++
	s.scratch = s.scratch[:0]
	for _, t := range s.around(c.to) {
		for _, v := range s.m.Triangles[t] {
			if p := s.position(v); p != c.to && !contains(s.scratch, p) {
				s.scratch = append(s.scratch, p)
			}
		}
	}
	for _, p := range s.scratch {
		for _, pushed := range [2][2]int32{{c.to, p}, {p, c.to}} {
			before := len(s.queue)
			s.push(pushed[0], pushed[1])
			if len(s.queue) > before {
				heap.Fix(&s.queue, before)
			}
		}
	}
}

func contains(list []int32, value int32) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func uvArea(uv [3][2]float32) float64 {
	ux, uy := float64(uv[1][0])-float64(uv[0][0]), float64(uv[1][1])-float64(uv[0][1])
	vx, vy := float64(uv[2][0])-float64(uv[0][0]), float64(uv[2][1])-float64(uv[0][1])
	return ux*vy - uy*vx
}

func sub(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func dot(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

// unit returns v scaled to length 1, and its length.
func unit(v [3]float64) ([3]float64, float64) {
	length := math.Sqrt(dot(v, v))
	if length == 0 {
		return v, 0
	}
	return [3]float64{v[0] / length, v[1] / length, v[2] / length}, length
}

// DropSmall removes from m the parts that are smaller than size: the sets
// of triangles connected by their corners whose bounds, from corner to
// corner, are shorter. Parts that hold a position where keep is true stay.
// It returns how many triangles it removed.
//
// Scanned meshes hold many small parts that float on their own. Collapses
// cannot remove them, as the last ones would fold them over, so they would
// otherwise fill the coarse levels of detail, where they are too small to
// see.
func DropSmall(m *mesh.Indexed, keep []bool, size float64) int {
	parent := make([]int32, len(m.Positions))
	for i := range parent {
		parent[i] = int32(i)
	}
	find := func(p int32) int32 {
		for parent[p] != p {
			parent[p] = parent[parent[p]]
			p = parent[p]
		}
		return p
	}
	position := func(vertex int32) int32 { return m.Vertices[vertex].Position }
	for _, t := range m.Triangles {
		a, b, c := find(position(t[0])), find(position(t[1])), find(position(t[2]))
		parent[b], parent[c] = a, a
	}
	type part struct {
		low, high  [3]float32
		seen, kept bool
	}
	parts := make([]part, len(m.Positions))
	for _, t := range m.Triangles {
		for _, v := range t {
			p := position(v)
			part := &parts[find(p)]
			point := m.Positions[p]
			if !part.seen {
				part.seen, part.low, part.high = true, point, point
			}
			for a := range 3 {
				part.low[a], part.high[a] = min(part.low[a], point[a]), max(part.high[a], point[a])
			}
			if int(p) < len(keep) && keep[p] {
				part.kept = true
			}
		}
	}
	kept := m.Triangles[:0]
	for _, t := range m.Triangles {
		part := &parts[find(position(t[0]))]
		d := [3]float64{float64(part.high[0]) - float64(part.low[0]), float64(part.high[1]) - float64(part.low[1]), float64(part.high[2]) - float64(part.low[2])}
		if part.kept || dot(d, d) >= size*size {
			kept = append(kept, t)
		}
	}
	removed := len(m.Triangles) - len(kept)
	m.Triangles = kept
	return removed
}

// clusterSteps is how many cell sizes Cluster tries: each halves the range
// that holds the size it looks for.
const clusterSteps = 24

// Cluster reduces m toward target triangles by merging the positions that
// fall in the same cell of a grid, and returns how far the surface then
// lies from where it started, at most, given how far it lay around each
// position before, in moved, which it updates and which can be nil. It uses the smallest cells that reach the target, up to maxCell
// along each axis: with cells of that size it stops, even over the target.
//
// The positions of a cell merge into the one nearest to their middle, so
// the positions that are left are positions of the input. Positions where
// locked is true stay as they are, each alone. Triangles whose corners
// merge go, as do copies of a triangle.
//
// Unlike Simplify it does not look at the shape of the surface: it can fold
// it, and join parts that only come near each other. It is for the coarse
// levels of detail, where triangles are a few pixels large and collapses,
// which keep the shape, no longer find any to do. It leaves the positions
// and vertices of m as they are, some unused, and replaces its triangles.
func Cluster(m *mesh.Indexed, locked []bool, moved []float64, target int, maxCell float64) float64 {
	if moved == nil {
		moved = make([]float64, len(m.Positions))
	}
	if len(m.Triangles) <= target || !(maxCell > 0) {
		var most float64
		for _, t := range m.Triangles {
			for _, v := range t {
				most = math.Max(most, moved[m.Vertices[v].Position])
			}
		}
		return most
	}
	c := clusterer{m: m, locked: locked, moved: moved, cellOf: make([]int32, len(m.Positions))}
	small, large := maxCell/float64(1<<clusterSteps), maxCell
	if c.count(large) <= target {
		for range clusterSteps {
			middle := math.Sqrt(small * large)
			if c.count(middle) <= target {
				large = middle
			} else {
				small = middle
			}
		}
		c.count(large)
	}
	var most float64
	m.Triangles, most = c.triangles()
	return most
}

type clusterer struct {
	m      *mesh.Indexed
	locked []bool
	moved  []float64
	// cellOf holds the cluster of each position.
	cellOf []int32
	cells  map[[3]int32]int32
	seen   map[[3]int32]struct{}
}

// assign groups the positions by the cell of the given size they fall in,
// and returns the number of groups.
func (c *clusterer) assign(size float64) int {
	clear(c.cells)
	if c.cells == nil {
		c.cells = map[[3]int32]int32{}
	}
	next := int32(0)
	for i, p := range c.m.Positions {
		if i < len(c.locked) && c.locked[i] {
			c.cellOf[i] = next
			next++
			continue
		}
		key := [3]int32{int32(math.Floor(float64(p[0]) / size)), int32(math.Floor(float64(p[1]) / size)), int32(math.Floor(float64(p[2]) / size))}
		cell, ok := c.cells[key]
		if !ok {
			cell = next
			next++
			c.cells[key] = cell
		}
		c.cellOf[i] = cell
	}
	return int(next)
}

// corners returns the clusters of the corners of a triangle, from the
// lowest, in the order of the triangle, and whether they differ.
func (c *clusterer) corners(t [3]int32) ([3]int32, bool) {
	k := [3]int32{c.cellOf[c.m.Vertices[t[0]].Position], c.cellOf[c.m.Vertices[t[1]].Position], c.cellOf[c.m.Vertices[t[2]].Position]}
	if k[0] == k[1] || k[1] == k[2] || k[0] == k[2] {
		return k, false
	}
	for k[0] > k[1] || k[0] > k[2] {
		k[0], k[1], k[2] = k[1], k[2], k[0]
	}
	return k, true
}

// count returns how many triangles are left with cells of the given size.
func (c *clusterer) count(size float64) int {
	c.assign(size)
	clear(c.seen)
	if c.seen == nil {
		c.seen = map[[3]int32]struct{}{}
	}
	for _, t := range c.m.Triangles {
		if k, ok := c.corners(t); ok {
			c.seen[k] = struct{}{}
		}
	}
	return len(c.seen)
}

// triangles returns the triangles that are left after the last count, each
// with the vertices of the positions that its clusters merge into, and how
// far the surface then lies from where it started, at most. It updates
// moved for the positions that are left.
func (c *clusterer) triangles() ([][3]int32, float64) {
	clusters := 0
	for _, cell := range c.cellOf {
		clusters = max(clusters, int(cell)+1)
	}
	sums := make([][3]float64, clusters)
	sizes := make([]int, clusters)
	for i, p := range c.m.Positions {
		cell := c.cellOf[i]
		sizes[cell]++
		for a := range 3 {
			sums[cell][a] += float64(p[a])
		}
	}
	// The position of each cluster nearest to its middle, the first of
	// those as near.
	nearest := make([]int32, clusters)
	distance := make([]float64, clusters)
	for i := range nearest {
		nearest[i] = -1
	}
	for i, p := range c.m.Positions {
		cell := c.cellOf[i]
		var d float64
		for a := range 3 {
			delta := float64(p[a]) - sums[cell][a]/float64(sizes[cell])
			d += delta * delta
		}
		if nearest[cell] < 0 || d < distance[cell] {
			nearest[cell], distance[cell] = int32(i), d
		}
	}
	// A vertex for each position that is left: the first that uses it.
	vertexOf := make([]int32, len(c.m.Positions))
	for i := range vertexOf {
		vertexOf[i] = -1
	}
	for i, v := range c.m.Vertices {
		if vertexOf[v.Position] < 0 {
			vertexOf[v.Position] = int32(i)
		}
	}
	clear(c.seen)
	var out [][3]int32
	// A position that others merge into stands for the surface around
	// them: it lies as far from where that started as the farthest of them
	// did, and moved.
	reached := make([]float64, len(c.m.Positions))
	for i := range c.m.Positions {
		to := nearest[c.cellOf[i]]
		var d float64
		for a := range 3 {
			delta := float64(c.m.Positions[i][a]) - float64(c.m.Positions[to][a])
			d += delta * delta
		}
		reached[to] = math.Max(reached[to], c.moved[i]+math.Sqrt(d))
	}
	var most float64
	for _, t := range c.m.Triangles {
		k, ok := c.corners(t)
		if !ok {
			continue
		}
		if _, dup := c.seen[k]; dup {
			continue
		}
		c.seen[k] = struct{}{}
		out = append(out, [3]int32{vertexOf[nearest[k[0]]], vertexOf[nearest[k[1]]], vertexOf[nearest[k[2]]]})
		for _, cell := range k {
			c.moved[nearest[cell]] = reached[nearest[cell]]
			most = math.Max(most, reached[nearest[cell]])
		}
	}
	return out, most
}
