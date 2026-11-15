package texture

import (
	"cmp"
	"errors"
	"math"
	"slices"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

const (
	// padding is how many pixels the atlas adds on each side of a chart,
	// repeating those at its border, so that filtering there does not blend
	// in the chart packed next to it.
	padding = 2
	// minAtlasSize is the smallest side of an atlas.
	minAtlasSize = 16
	// atlasGrain is what the sides of an atlas are multiples of, in pixels:
	// the blocks of compressed textures are four pixels wide. Sides need
	// not be powers of two, which would leave up to half an atlas empty.
	atlasGrain = 4
	// shrink is how much the charts are scaled down by each time they do not
	// fit the largest atlas.
	shrink = 0.9
	// maxShrinks bounds the attempts to fit the charts.
	maxShrinks = 100
	// minScale is the scale below which charts are a pixel large whatever
	// their size in the sources.
	minScale = 1e-6
)

// Options configure Pack.
type Options struct {
	// Scale is how many pixels of the atlas a pixel of the sources takes
	// along each axis, at most: 1 keeps their resolution.
	Scale float64
	// MaxSize is the largest side of the atlas in pixels. Charts that do not
	// fit are scaled down further.
	MaxSize int
	// VUp tells that the texture coordinates of the mesh grow upward, as in
	// OBJ files. Those of the atlas always grow downward, as in glTF.
	VUp bool
}

// chart is a connected part of a mesh that maps to one region of an image.
type chart struct {
	material uint32
	// shift moves the texture coordinates of the chart into the image, for
	// those that repeat it.
	shiftU, shiftV float64
	// x, y, width and height locate the pixels the chart covers in the
	// source, and outWidth and outHeight their size in the atlas.
	x, y, width, height int
	outWidth, outHeight int
	// left and top locate the chart in the atlas, padding included.
	left, top              int
	minU, minV, maxU, maxV float64
}

// Pack copies the regions of the sources that the triangles of m use into a
// new image, an atlas, and makes the vertices of m refer to it: their
// material becomes 0 and their texture coordinates locate them in the atlas,
// growing downward. The material of a vertex indexes sources.
//
// Triangles that share vertices keep sharing them, so texture seams stay
// where they were.
func Pack(m *mesh.Indexed, sources []Source, opts Options) (*Image, error) {
	if len(m.Triangles) == 0 {
		return nil, errors.New("no triangles to pack")
	}
	charts, chartOf := findCharts(m)
	for i := range charts {
		c := &charts[i]
		if int(c.material) >= len(sources) {
			return nil, errors.New("a triangle refers to a missing material")
		}
		c.locate(sources[c.material], opts.VUp)
	}
	scale := math.Min(opts.Scale, 1)
	if !(scale > 0) {
		scale = 1
	}
	maxSize := max(opts.MaxSize, minAtlasSize)
	var width, height int
	for attempt := 0; ; attempt++ {
		if attempt > maxShrinks {
			return nil, errors.New("the textures of the mesh do not fit the largest atlas")
		}
		for i := range charts {
			c := &charts[i]
			c.outWidth = max(1, int(math.Round(float64(c.width)*scale)))
			c.outHeight = max(1, int(math.Round(float64(c.height)*scale)))
		}
		// Far too large for the atlas: go straight to the scale that would
		// fit if nothing were lost between the charts.
		if excess := float64(area(charts)) / float64(maxSize*maxSize); excess > 1 && scale > minScale {
			scale = math.Max(scale/math.Sqrt(excess), minScale)
			continue
		}
		var fits bool
		if width, height, fits = layout(charts, maxSize); fits {
			break
		}
		scale *= shrink
	}
	atlas := NewImage(width, height)
	covered := make([]bool, width*height)
	var sum [channels]float64
	var buffer []byte
	for i := range charts {
		var err error
		if buffer, err = charts[i].render(atlas, sources[charts[i].material], buffer, covered, &sum); err != nil {
			return nil, err
		}
	}
	fillBackground(atlas, covered, sum)
	for i := range m.Vertices {
		v := &m.Vertices[i]
		if chartOf[i] < 0 {
			continue
		}
		c := &charts[chartOf[i]]
		sourceWidth, sourceHeight := sources[c.material].Size()
		x := (float64(v.UV[0])-c.shiftU)*float64(sourceWidth) - float64(c.x)
		y := (float64(v.UV[1]) - c.shiftV) * float64(sourceHeight)
		if opts.VUp {
			y = float64(sourceHeight) - y
		}
		y -= float64(c.y)
		v.UV[0] = float32((float64(c.left+padding) + x*float64(c.outWidth)/float64(c.width)) / float64(width))
		v.UV[1] = float32((float64(c.top+padding) + y*float64(c.outHeight)/float64(c.height)) / float64(height))
		v.Material = 0
	}
	return atlas, nil
}

// findCharts groups the vertices of m into charts: the sets of vertices that
// triangles connect. It returns the charts and, for each vertex, the index
// of its chart, or -1 for vertices no triangle uses.
func findCharts(m *mesh.Indexed) ([]chart, []int32) {
	parent := make([]int32, len(m.Vertices))
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(v int32) int32
	find = func(v int32) int32 {
		for parent[v] != v {
			parent[v] = parent[parent[v]]
			v = parent[v]
		}
		return v
	}
	union := func(a, b int32) {
		a, b = find(a), find(b)
		// The lowest vertex is the root, so that charts come out in the
		// order of their first vertex.
		if a < b {
			parent[b] = a
		} else {
			parent[a] = b
		}
	}
	used := make([]bool, len(m.Vertices))
	for _, t := range m.Triangles {
		union(t[0], t[1])
		union(t[1], t[2])
		used[t[0]], used[t[1]], used[t[2]] = true, true, true
	}
	chartOf := make([]int32, len(m.Vertices))
	var charts []chart
	for i := range m.Vertices {
		chartOf[i] = -1
		if !used[i] {
			continue
		}
		root := find(int32(i))
		v := m.Vertices[i]
		u, w := float64(v.UV[0]), float64(v.UV[1])
		if root == int32(i) {
			chartOf[i] = int32(len(charts))
			charts = append(charts, chart{material: v.Material, minU: u, maxU: u, minV: w, maxV: w})
			continue
		}
		// The root has a lower index, so its chart exists already.
		chartOf[i] = chartOf[root]
		c := &charts[chartOf[i]]
		c.minU, c.maxU = math.Min(c.minU, u), math.Max(c.maxU, u)
		c.minV, c.maxV = math.Min(c.minV, w), math.Max(c.maxV, w)
	}
	return charts, chartOf
}

// locate finds the pixels of source that the chart covers.
func (c *chart) locate(source Source, vUp bool) {
	width, height := source.Size()
	c.shiftU, c.shiftV = math.Floor(c.minU), math.Floor(c.minV)
	x0 := int(math.Floor((c.minU - c.shiftU) * float64(width)))
	x1 := int(math.Ceil((c.maxU - c.shiftU) * float64(width)))
	top, bottom := (c.minV-c.shiftV)*float64(height), (c.maxV-c.shiftV)*float64(height)
	if vUp {
		top, bottom = float64(height)-bottom, float64(height)-top
	}
	y0, y1 := int(math.Floor(top)), int(math.Ceil(bottom))
	c.x, c.y = x0, y0
	c.width, c.height = max(1, x1-x0), max(1, y1-y0)
}

// layout places the charts on shelves in the smallest atlas it finds, whose
// sides are multiples of atlasGrain, and returns its size, and whether it is
// within maxSize.
func layout(charts []chart, maxSize int) (width, height int, fits bool) {
	order := make([]int, len(charts))
	var area, widest int
	for i := range charts {
		order[i] = i
		w, h := charts[i].outWidth+2*padding, charts[i].outHeight+2*padding
		area += w * h
		widest = max(widest, w)
	}
	// Tall charts first: each shelf is as tall as its first chart.
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(charts[b].outHeight, charts[a].outHeight)
	})
	// Among widths from that of a square of the charts up to twice that,
	// take one whose atlas is within maxSize if any, then the smallest
	// atlas, then the closest to a square.
	side := math.Sqrt(float64(area))
	for _, ratio := range []float64{1, 1.1, 1.25, 1.5, 2} {
		w := max(minAtlasSize, widest, int(math.Ceil(side*ratio)))
		w = min(roundUp(w), max(roundUp(widest), maxSize))
		h := max(roundUp(shelve(charts, order, w, false)), minAtlasSize)
		ok := w <= maxSize && h <= maxSize
		better := width == 0 || (ok && !fits)
		if !better && ok == fits {
			better = w*h < width*height || (w*h == width*height && max(w, h) < max(width, height))
		}
		if better {
			width, height, fits = w, h, ok
		}
	}
	shelve(charts, order, width, true)
	return width, height, fits
}

// area returns the pixels the charts take together, padding included.
func area(charts []chart) int {
	total := 0
	for i := range charts {
		total += (charts[i].outWidth + 2*padding) * (charts[i].outHeight + 2*padding)
	}
	return total
}

// shelve places the charts, in the given order, left to right on shelves of
// an atlas of the given width, and returns the height they take. It records
// the places only if place is true.
func shelve(charts []chart, order []int, width int, place bool) int {
	x, y, shelf := 0, 0, 0
	for _, i := range order {
		c := &charts[i]
		w, h := c.outWidth+2*padding, c.outHeight+2*padding
		if x+w > width && x > 0 {
			x, y, shelf = 0, y+shelf, 0
		}
		if place {
			c.left, c.top = x, y
		}
		x += w
		shelf = max(shelf, h)
	}
	return y + shelf
}

// roundUp returns n rounded up to a multiple of atlasGrain.
func roundUp(n int) int {
	return (n + atlasGrain - 1) / atlasGrain * atlasGrain
}

// render draws the chart, with its padding, at its place in atlas,
// averaging the pixels of the source that each of its pixels covers. It
// marks the pixels it draws in covered and adds their colors to sum. It
// reads the source into buffer, which it returns, grown if needed.
func (c *chart) render(atlas *Image, source Source, buffer []byte, covered []bool, sum *[channels]float64) ([]byte, error) {
	sourceWidth, sourceHeight := source.Size()
	outWidth, outHeight := c.outWidth+2*padding, c.outHeight+2*padding
	columns := spans(c.x, c.width, c.outWidth, sourceWidth)
	rows := spans(c.y, c.height, c.outHeight, sourceHeight)
	x0, x1 := columns[0].low, columns[len(columns)-1].high
	y0, y1 := rows[0].low, rows[len(rows)-1].high
	for _, s := range columns {
		x0, x1 = min(x0, s.low), max(x1, s.high)
	}
	for _, s := range rows {
		y0, y1 = min(y0, s.low), max(y1, s.high)
	}
	regionWidth := x1 - x0
	if need := regionWidth * (y1 - y0) * channels; cap(buffer) < need {
		buffer = make([]byte, need)
	}
	buffer = buffer[:regionWidth*(y1-y0)*channels]
	if err := source.Read(x0, y0, regionWidth, y1-y0, buffer); err != nil {
		return buffer, err
	}
	for j := range outHeight {
		row := rows[j]
		for i := range outWidth {
			column := columns[i]
			var total [channels]int
			for y := row.low; y < row.high; y++ {
				line := buffer[((y-y0)*regionWidth+column.low-x0)*channels:]
				for x := range column.high - column.low {
					total[0] += int(line[x*channels])
					total[1] += int(line[x*channels+1])
					total[2] += int(line[x*channels+2])
				}
			}
			count := (row.high - row.low) * (column.high - column.low)
			pixel := (c.top+j)*atlas.Width + c.left + i
			for k := range channels {
				value := (total[k] + count/2) / count
				atlas.Pix[pixel*channels+k] = uint8(value)
				sum[k] += float64(value)
			}
			covered[pixel] = true
		}
	}
	return buffer, nil
}

// span is a range of pixels of a source, from low up to high, not included.
type span struct{ low, high int }

// spans returns, for each pixel of a chart in the atlas along one axis,
// padding included, the pixels of the source it covers: the chart takes size
// pixels of the source from start, and outSize pixels of the atlas.
//
// Spans stay within the chart, and within the limit pixels of the source:
// the padding repeats the pixels at the border of the chart. What lies past
// it in the source belongs to other charts.
func spans(start, size, outSize, limit int) []span {
	out := make([]span, outSize+2*padding)
	step := float64(size) / float64(outSize)
	first, last := min(max(start, 0), limit-1), min(max(start+size, 1), limit)
	if last <= first {
		last = first + 1
	}
	for i := range out {
		low := int(math.Floor(float64(start) + float64(i-padding)*step))
		high := int(math.Ceil(float64(start) + float64(i-padding+1)*step))
		low = min(max(low, first), last-1)
		high = min(max(high, low+1), last)
		out[i] = span{low, high}
	}
	return out
}

// fillBackground gives the pixels of atlas that no chart covers the average
// color of the others, so that compression artifacts and coarse mip levels
// at the border of charts stay close to their colors.
func fillBackground(atlas *Image, covered []bool, sum [channels]float64) {
	count := 0
	for _, c := range covered {
		if c {
			count++
		}
	}
	if count == 0 {
		return
	}
	var average [channels]uint8
	for k := range average {
		average[k] = uint8(math.Round(sum[k] / float64(count)))
	}
	for i, c := range covered {
		if !c {
			copy(atlas.Pix[i*channels:], average[:])
		}
	}
}
