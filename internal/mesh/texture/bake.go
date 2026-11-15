package texture

import (
	"errors"
	"fmt"
	"math"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

const (
	// minFacing is the least cosine between the normal of a triangle and the
	// direction its chart is seen from: flatter triangles would get too few
	// pixels.
	minFacing = 0.5
	// bakePadding is how many pixels around the surface of a chart are
	// filled with its colors, so that filtering at its border does not
	// blend in other charts.
	bakePadding = padding
	// minChartArea is the least surface of a triangle that can start a
	// chart or bound one, in pixels.
	minChartArea = 0.01
	// smallChartPixels is the surface, in pixels, under which a chart is
	// merged into one next to it, and minAbsorbFacing the least cosine
	// between the normal of a triangle and the direction of the chart that
	// takes it in. absorbPasses is how many times the triangles are gone
	// through, as a merge can open the way for the next.
	smallChartPixels = 64
	minAbsorbFacing  = 0.1
	absorbPasses     = 2
	// gridCells is the number of cells, along each axis, of the grids that
	// find the detailed triangles under a chart.
	gridCells = 64
	// coverageSlack lets a pixel whose center lies just outside a triangle,
	// by this share of it, count as inside, so that neighbors leave no
	// uncovered pixel along the edge they share.
	coverageSlack = 1e-9
)

// BakeOptions configure Bake.
type BakeOptions struct {
	// TexelSize is the length, on the surface, of the side of a pixel of the
	// atlas. Bake makes it larger if the atlas would not fit MaxSize.
	TexelSize float64
	// MaxSize is the largest side of the atlas in pixels. Only a surface
	// of more charts than fit it, a pixel each, gets a larger one.
	MaxSize int
}

// view is one of the six directions a chart is seen from: along the axis
// depth, from its positive end if sign is 1 and its negative end if -1. The
// axes u and v span the image.
type view struct {
	depth, u, v int
	sign        float64
}

func viewOf(normal [3]float64) view {
	depth := 0
	for a := 1; a < 3; a++ {
		if math.Abs(normal[a]) > math.Abs(normal[depth]) {
			depth = a
		}
	}
	sign := 1.0
	if normal[depth] < 0 {
		sign = -1
	}
	return view{depth: depth, u: (depth + 1) % 3, v: (depth + 2) % 3, sign: sign}
}

// bakeChart is a connected part of the surface seen from one direction,
// which maps to a rectangle of the atlas.
type bakeChart struct {
	view      view
	triangles []int32
	// minU and minV are the lowest coordinates of the chart along the axes
	// of its image.
	minU, minV, maxU, maxV float64
}

// Bake gives the surface of m, which holds positions only, a new texture
// that shows the colors of detail, a finer version of the same surface
// whose triangles are textured by sources. It returns m with the vertices
// and texture coordinates of the new texture, the texture, and the length
// on the surface of the side of its pixels.
//
// Nothing of the texture layout of detail is kept. The surface is split
// into charts, each made of connected triangles that face one of the six
// axis directions, and each chart is drawn as seen from its direction: a
// pixel takes the color of the point of detail that the same line of sight
// meets nearest to the surface. Charts are as large as the surface allows,
// however the charts of detail were cut, so the simplification of m did not
// have to preserve them.
func Bake(m *mesh.Indexed, detail []mesh.Triangle, sources []*Image, opts BakeOptions) (*mesh.Indexed, *Image, float64, error) {
	if len(m.Triangles) == 0 {
		return nil, nil, 0, errors.New("no triangles to texture")
	}
	if !(opts.TexelSize > 0) {
		return nil, nil, 0, errors.New("the size of a pixel on the surface must be positive")
	}
	charts, chartOf := growCharts(m, opts.TexelSize)
	texel := opts.TexelSize
	rects := make([]chart, len(charts))
	var width, height int
	smallest := -1
	for attempt := 0; ; attempt++ {
		if attempt > maxShrinks {
			return nil, nil, 0, fmt.Errorf("the %d charts of the %d triangles do not fit the largest atlas, at %g a pixel (asked %g): %d pixels", len(charts), len(m.Triangles), texel, opts.TexelSize, area(rects))
		}
		for i, c := range charts {
			// A pixel center on each end of the chart, and one to spare.
			rects[i].outWidth = int((c.maxU-c.minU)/texel) + 2 + 2*(bakePadding-padding)
			rects[i].outHeight = int((c.maxV-c.minV)/texel) + 2 + 2*(bakePadding-padding)
		}
		maxSize := max(opts.MaxSize, minAtlasSize)
		// Far too large for the atlas: go straight to the size that would
		// fit if nothing were lost between the charts.
		if excess := float64(area(rects)) / float64(maxSize*maxSize); excess > 1 {
			texel *= math.Sqrt(excess)
			continue
		}
		var fits bool
		if width, height, fits = layout(rects, maxSize); fits {
			break
		}
		// Charts of a pixel cannot get smaller: the atlas is then as large
		// as they need, over the limit.
		if pixels := area(rects); pixels == smallest {
			break
		} else {
			smallest = pixels
		}
		texel /= shrink
	}
	atlas := NewImage(width, height)
	b := &baker{m: m, detail: detail, sources: sources, texel: texel}
	b.stamp = make([]int32, len(detail))
	for i := range b.stamp {
		b.stamp[i] = -1
	}
	for i := range charts {
		b.draw(atlas, &charts[i], int32(i), rects[i].left, rects[i].top, rects[i].outWidth+2*padding, rects[i].outHeight+2*padding)
	}

	out := &mesh.Indexed{Positions: m.Positions, Triangles: make([][3]int32, len(m.Triangles))}
	type key struct{ position, chart int32 }
	vertices := map[key]int32{}
	for t, corners := range m.Triangles {
		c := &charts[chartOf[t]]
		for k, corner := range corners {
			position := m.Vertices[corner].Position
			index, ok := vertices[key{position, chartOf[t]}]
			if !ok {
				p := m.Positions[position]
				x := (float64(p[c.view.u])-c.minU)/texel + bakePadding + 0.5
				y := (float64(p[c.view.v])-c.minV)/texel + bakePadding + 0.5
				index = int32(len(out.Vertices))
				vertices[key{position, chartOf[t]}] = index
				out.Vertices = append(out.Vertices, mesh.Vertex{
					Position: position,
					UV: [2]float32{
						float32((float64(rects[chartOf[t]].left) + x) / float64(width)),
						float32((float64(rects[chartOf[t]].top) + y) / float64(height)),
					},
				})
			}
			out.Triangles[t][k] = index
		}
	}
	return out, atlas, texel, nil
}

// growCharts splits the triangles of m into charts, for a texture whose
// pixels are texel long on the surface. A chart starts from a triangle,
// takes the direction its normal is closest to, and grows across edges over
// the triangles that face that direction well enough. It returns the charts
// and the index of the chart of each triangle.
func growCharts(m *mesh.Indexed, texel float64) ([]bakeChart, []int32) {
	type edge struct{ low, high int32 }
	normals := make([][3]float64, len(m.Triangles))
	neighbors := make(map[edge][]int32, len(m.Triangles)*3/2)
	corner := func(t, k int) int32 { return m.Vertices[m.Triangles[t][k]].Position }
	for t := range m.Triangles {
		n := mesh.Normal(m.Positions[corner(t, 0)], m.Positions[corner(t, 1)], m.Positions[corner(t, 2)])
		// The normal of a triangle much smaller than a pixel is noise: it is
		// left without one, to go with the chart of a neighbor.
		if length := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]); length/2 > minChartArea*texel*texel {
			normals[t] = [3]float64{n[0] / length, n[1] / length, n[2] / length}
		}
		for k := range 3 {
			e := edge{corner(t, k), corner(t, (k+1)%3)}
			if e.low > e.high {
				e.low, e.high = e.high, e.low
			}
			neighbors[e] = append(neighbors[e], int32(t))
		}
	}
	chartOf := make([]int32, len(m.Triangles))
	for i := range chartOf {
		chartOf[i] = -1
	}
	var charts []bakeChart
	var queue []int32
	for seed := range m.Triangles {
		if chartOf[seed] >= 0 {
			continue
		}
		index := int32(len(charts))
		c := bakeChart{view: viewOf(normals[seed]), minU: math.Inf(1), minV: math.Inf(1), maxU: math.Inf(-1), maxV: math.Inf(-1)}
		chartOf[seed] = index
		queue = append(queue[:0], int32(seed))
		for len(queue) > 0 {
			t := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			c.triangles = append(c.triangles, t)
			for k := range 3 {
				p := m.Positions[corner(int(t), k)]
				u, v := float64(p[c.view.u]), float64(p[c.view.v])
				c.minU, c.maxU = math.Min(c.minU, u), math.Max(c.maxU, u)
				c.minV, c.maxV = math.Min(c.minV, v), math.Max(c.maxV, v)
				e := edge{corner(int(t), k), corner(int(t), (k+1)%3)}
				if e.low > e.high {
					e.low, e.high = e.high, e.low
				}
				for _, other := range neighbors[e] {
					// A triangle without a surface has no normal: it goes
					// with whichever chart reaches it.
					facing := normals[other][c.view.depth] * c.view.sign
					if chartOf[other] < 0 && (facing >= minFacing || normals[other] == [3]float64{}) {
						chartOf[other] = index
						queue = append(queue, other)
					}
				}
			}
		}
		charts = append(charts, c)
	}

	// Rough surfaces leave many charts of a triangle or two, whose padding
	// would take more of the atlas than they do. Their triangles join the
	// largest chart next to them that they face at all: they get fewer
	// pixels there than in a chart of their own, but they are small.
	surface := make([]float64, len(charts))
	for t := range m.Triangles {
		n := mesh.Normal(m.Positions[corner(t, 0)], m.Positions[corner(t, 1)], m.Positions[corner(t, 2)])
		surface[chartOf[t]] += math.Sqrt(n[0]*n[0]+n[1]*n[1]+n[2]*n[2]) / 2
	}
	small := func(c int32) bool { return surface[c] < smallChartPixels*texel*texel }
	for range absorbPasses {
		for t := range m.Triangles {
			from := chartOf[t]
			if !small(from) {
				continue
			}
			best := int32(-1)
			for k := range 3 {
				e := edge{corner(t, k), corner(t, (k+1)%3)}
				if e.low > e.high {
					e.low, e.high = e.high, e.low
				}
				for _, other := range neighbors[e] {
					to := chartOf[other]
					if to == from || small(to) || (best >= 0 && surface[to] <= surface[best]) {
						continue
					}
					facing := normals[t][charts[to].view.depth] * charts[to].view.sign
					if facing >= minAbsorbFacing || normals[t] == [3]float64{} {
						best = to
					}
				}
			}
			if best >= 0 {
				chartOf[t] = best
			}
		}
	}
	// The charts as they are now, in the order of their first triangle.
	renumbered := make([]int32, len(charts))
	for i := range renumbered {
		renumbered[i] = -1
	}
	var merged []bakeChart
	for t := range m.Triangles {
		old := chartOf[t]
		if renumbered[old] < 0 {
			renumbered[old] = int32(len(merged))
			merged = append(merged, bakeChart{view: charts[old].view, minU: math.Inf(1), minV: math.Inf(1), maxU: math.Inf(-1), maxV: math.Inf(-1)})
		}
		chartOf[t] = renumbered[old]
		c := &merged[chartOf[t]]
		c.triangles = append(c.triangles, int32(t))
		for k := range 3 {
			p := m.Positions[corner(t, k)]
			u, v := float64(p[c.view.u]), float64(p[c.view.v])
			c.minU, c.maxU = math.Min(c.minU, u), math.Max(c.maxU, u)
			c.minV, c.maxV = math.Min(c.minV, v), math.Max(c.maxV, v)
		}
	}
	return merged, chartOf
}

// baker draws the charts of a surface from its detailed version.
type baker struct {
	m       *mesh.Indexed
	detail  []mesh.Triangle
	sources []*Image
	texel   float64
	// grids find the detailed triangles over a region, seen along each
	// axis. They are made when first needed.
	grids [3]*triangleGrid
	// stamp holds, for each detailed triangle, the last chart it was drawn
	// in, so that it is drawn once per chart, or a negative number of its
	// own for the second pass over a chart: pass counts those.
	stamp []int32
	pass  int32
	// The buffers of the chart being drawn, one value per pixel: the depth
	// of the surface, how far from it the color comes from, and the color.
	surface, distance []float32
	color             []byte
	queue, from       []int32
}

// draw draws the chart c, of width by height pixels with its padding, into
// atlas at left, top.
func (b *baker) draw(atlas *Image, c *bakeChart, index int32, left, top, width, height int) {
	pixels := width * height
	if cap(b.surface) < pixels {
		b.surface, b.distance, b.color = make([]float32, pixels), make([]float32, pixels), make([]byte, pixels*channels)
	}
	surface, distance, color := b.surface[:pixels], b.distance[:pixels], b.color[:pixels*channels]
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	for i := range surface {
		surface[i], distance[i] = nan, inf
	}
	// project returns the place of p in the image of the chart, in pixels,
	// and its depth.
	project := func(p [3]float32) (x, y, depth float64) {
		return (float64(p[c.view.u])-c.minU)/b.texel + bakePadding + 0.5,
			(float64(p[c.view.v])-c.minV)/b.texel + bakePadding + 0.5,
			float64(p[c.view.depth])
	}
	// First the surface of the chart: where it is, and at what depth.
	for _, t := range c.triangles {
		var xs, ys, depths [3]float64
		for k, corner := range b.m.Triangles[t] {
			xs[k], ys[k], depths[k] = project(b.m.Positions[b.m.Vertices[corner].Position])
		}
		rasterize(xs, ys, width, height, func(pixel int, w [3]float64) {
			surface[pixel] = float32(w[0]*depths[0] + w[1]*depths[1] + w[2]*depths[2])
		})
	}
	// Around the surface, and all over a chart too thin to hold the center
	// of a pixel, the depth is that of its nearest pixel: the padding then
	// shows what lies around the chart.
	if from := b.nearest(func(i int) bool { return surface[i] == surface[i] }, width, height); from != nil {
		for i, source := range from {
			surface[i] = surface[source]
		}
	} else {
		var depth float64
		for _, t := range c.triangles {
			for _, corner := range b.m.Triangles[t] {
				depth += float64(b.m.Positions[b.m.Vertices[corner].Position][c.view.depth]) / float64(3*len(c.triangles))
			}
		}
		for i := range surface {
			surface[i] = float32(depth)
		}
	}
	// Then the detailed triangles that face the same way, where they are
	// nearer to the surface than what was drawn so far. If none does, as
	// under a sliver whose normal says little, those that face away too.
	for _, facing := range []bool{true, false} {
		drawn := false
		grid := b.grid(c.view.depth)
		grid.visit(c.minU-bakePadding*b.texel, c.minV-bakePadding*b.texel, c.maxU+bakePadding*b.texel, c.maxV+bakePadding*b.texel, func(d int32) {
			if b.stamp[d] == index {
				return
			}
			b.stamp[d] = index
			t := &b.detail[d]
			if n := mesh.Normal(t.P[0], t.P[1], t.P[2]); facing && n[c.view.depth]*c.view.sign <= 0 {
				return
			}
			var xs, ys, depths [3]float64
			for k := range 3 {
				xs[k], ys[k], depths[k] = project(t.P[k])
			}
			source := b.sources[t.Material]
			rasterize(xs, ys, width, height, func(pixel int, w [3]float64) {
				depth := w[0]*depths[0] + w[1]*depths[1] + w[2]*depths[2]
				gap := float32(math.Abs(depth - float64(surface[pixel])))
				if gap >= distance[pixel] {
					return
				}
				distance[pixel] = gap
				u := w[0]*float64(t.UV[0][0]) + w[1]*float64(t.UV[1][0]) + w[2]*float64(t.UV[2][0])
				v := w[0]*float64(t.UV[0][1]) + w[1]*float64(t.UV[1][1]) + w[2]*float64(t.UV[2][1])
				source.sample(u, v, color[pixel*channels:])
				drawn = true
			})
		})
		if drawn {
			break
		}
		// Let the second pass draw the triangles again.
		b.pass++
		index = -2 - b.pass
	}
	// The pixels that no detailed triangle reached take the color of the
	// nearest that one did.
	if from := b.nearest(func(i int) bool { return distance[i] != inf }, width, height); from != nil {
		for i, source := range from {
			copy(color[i*channels:i*channels+channels], color[int(source)*channels:int(source)*channels+channels])
		}
	} else {
		for i := range color {
			color[i] = 128
		}
	}
	for row := range height {
		copy(atlas.Pix[((top+row)*atlas.Width+left)*channels:], color[row*width*channels:(row+1)*width*channels])
	}
}

// nearest returns, for each pixel of an image of width by height, the index
// of the nearest pixel for which has is true, found step by step from their
// border, or nil when has is true for none.
func (b *baker) nearest(has func(pixel int) bool, width, height int) []int32 {
	pixels := width * height
	if cap(b.from) < pixels {
		b.from = make([]int32, pixels)
	}
	from := b.from[:pixels]
	queue := b.queue[:0]
	for i := range from {
		from[i] = -1
		if has(i) {
			from[i] = int32(i)
			queue = append(queue, int32(i))
		}
	}
	if len(queue) == 0 {
		return nil
	}
	for head := 0; head < len(queue); head++ {
		i := int(queue[head])
		x, y := i%width, i/width
		for _, step := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nx, ny := x+step[0], y+step[1]
			if nx < 0 || ny < 0 || nx >= width || ny >= height {
				continue
			}
			if j := ny*width + nx; from[j] < 0 {
				from[j] = from[i]
				queue = append(queue, int32(j))
			}
		}
	}
	b.queue = queue
	return from
}

// rasterize calls visit for each pixel of an image of width by height whose
// center lies in the triangle with corners at xs, ys, in pixels, with the
// index of the pixel and the weights of the corners at its center.
func rasterize(xs, ys [3]float64, width, height int, visit func(pixel int, weights [3]float64)) {
	area := (xs[1]-xs[0])*(ys[2]-ys[0]) - (xs[2]-xs[0])*(ys[1]-ys[0])
	if area == 0 {
		return
	}
	x0 := max(0, int(math.Floor(min(xs[0], xs[1], xs[2])-0.5)))
	x1 := min(width-1, int(math.Ceil(max(xs[0], xs[1], xs[2])-0.5)))
	y0 := max(0, int(math.Floor(min(ys[0], ys[1], ys[2])-0.5)))
	y1 := min(height-1, int(math.Ceil(max(ys[0], ys[1], ys[2])-0.5)))
	for y := y0; y <= y1; y++ {
		py := float64(y) + 0.5
		for x := x0; x <= x1; x++ {
			px := float64(x) + 0.5
			var w [3]float64
			w[0] = ((xs[1]-px)*(ys[2]-py) - (xs[2]-px)*(ys[1]-py)) / area
			w[1] = ((xs[2]-px)*(ys[0]-py) - (xs[0]-px)*(ys[2]-py)) / area
			w[2] = 1 - w[0] - w[1]
			if w[0] < -coverageSlack || w[1] < -coverageSlack || w[2] < -coverageSlack {
				continue
			}
			visit(y*width+x, w)
		}
	}
}

// sample writes to dst the color of m at the texture coordinates u, v,
// which grow rightward and downward, blended from the four pixels around.
func (m *Image) sample(u, v float64, dst []byte) {
	x := min(max(u*float64(m.Width)-0.5, 0), float64(m.Width-1))
	y := min(max(v*float64(m.Height)-0.5, 0), float64(m.Height-1))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, m.Width-1), min(y0+1, m.Height-1)
	fx, fy := x-float64(x0), y-float64(y0)
	a, b := m.Pix[(y0*m.Width+x0)*channels:], m.Pix[(y0*m.Width+x1)*channels:]
	c, d := m.Pix[(y1*m.Width+x0)*channels:], m.Pix[(y1*m.Width+x1)*channels:]
	for k := range channels {
		top := float64(a[k])*(1-fx) + float64(b[k])*fx
		bottom := float64(c[k])*(1-fx) + float64(d[k])*fx
		dst[k] = uint8(top*(1-fy) + bottom*fy + 0.5)
	}
}

// triangleGrid lists the triangles over each cell of a grid laid on the
// plane across one axis.
type triangleGrid struct {
	minU, minV, cellU, cellV float64
	cells                    [][]int32
}

// grid returns the grid of the detailed triangles seen along the axis
// depth.
func (b *baker) grid(depth int) *triangleGrid {
	if b.grids[depth] != nil {
		return b.grids[depth]
	}
	u, v := (depth+1)%3, (depth+2)%3
	g := &triangleGrid{minU: math.Inf(1), minV: math.Inf(1), cells: make([][]int32, gridCells*gridCells)}
	maxU, maxV := math.Inf(-1), math.Inf(-1)
	for i := range b.detail {
		for _, p := range b.detail[i].P {
			g.minU, maxU = math.Min(g.minU, float64(p[u])), math.Max(maxU, float64(p[u]))
			g.minV, maxV = math.Min(g.minV, float64(p[v])), math.Max(maxV, float64(p[v]))
		}
	}
	g.cellU, g.cellV = math.Max((maxU-g.minU)/gridCells, 1e-9), math.Max((maxV-g.minV)/gridCells, 1e-9)
	for i := range b.detail {
		p := b.detail[i].P
		lowU, highU := min(p[0][u], p[1][u], p[2][u]), max(p[0][u], p[1][u], p[2][u])
		lowV, highV := min(p[0][v], p[1][v], p[2][v]), max(p[0][v], p[1][v], p[2][v])
		g.each(float64(lowU), float64(lowV), float64(highU), float64(highV), func(cell int) {
			g.cells[cell] = append(g.cells[cell], int32(i))
		})
	}
	b.grids[depth] = g
	return g
}

// each calls visit with the index of each cell that the given region
// touches.
func (g *triangleGrid) each(minU, minV, maxU, maxV float64, visit func(cell int)) {
	clamp := func(value, origin, size float64) int {
		return min(max(int((value-origin)/size), 0), gridCells-1)
	}
	u0, u1 := clamp(minU, g.minU, g.cellU), clamp(maxU, g.minU, g.cellU)
	v0, v1 := clamp(minV, g.minV, g.cellV), clamp(maxV, g.minV, g.cellV)
	for v := v0; v <= v1; v++ {
		for u := u0; u <= u1; u++ {
			visit(v*gridCells + u)
		}
	}
}

// visit calls visit with each triangle listed in a cell that the given
// region touches, possibly more than once.
func (g *triangleGrid) visit(minU, minV, maxU, maxV float64, visit func(triangle int32)) {
	g.each(minU, minV, maxU, maxV, func(cell int) {
		for _, t := range g.cells[cell] {
			visit(t)
		}
	})
}
