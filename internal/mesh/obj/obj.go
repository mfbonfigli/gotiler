// Package obj reads textured triangle meshes from Wavefront OBJ files.
package obj

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// Options configure Read.
type Options struct {
	// TempDir is the folder of the temporary files of the vertices.
	TempDir string
	// CacheBytes is how much memory the vertices read back from the
	// temporary files can take. 0 means 256 MB.
	CacheBytes int
	// Progress, if not nil, receives the number of bytes of the file read so
	// far, every few megabytes.
	Progress func(bytes int64)
}

// Info describes a mesh that Read read.
type Info struct {
	// Materials are the materials of the triangles, indexed by their
	// Material field. The paths of their textures are absolute.
	Materials []mesh.Material
	// Origin is the point the triangles are relative to.
	Origin [3]float64
	// Vertices and Triangles count the positions and the triangles read.
	Vertices, Triangles int64
	// Size is the size of the file in bytes.
	Size int64
}

const (
	positionSize  = 12
	texCoordSize  = 8
	progressEvery = 8 << 20
	// maxLineSize bounds the length of a line, for faces of many corners.
	maxLineSize = 64 << 20
)

// defaultColor is the color of faces without a material.
var defaultColor = [3]uint8{200, 200, 200}

type reader struct {
	dir       string
	positions *table
	texCoords *table
	info      Info
	hasOrigin bool
	byName    map[string]uint32
	// current is the material of the faces being read, once a usemtl line
	// named one: hasCurrent. Faces before it take the material at none.
	current    uint32
	hasCurrent bool
	noMaterial uint32
	hasNone    bool
	emit       func(*mesh.Triangle) error
	corners    []corner
}

type corner struct {
	position [3]float32
	uv       [2]float32
}

// Read reads the triangles of the OBJ file at path and passes them to emit,
// in the order of the file. Faces of more than three corners are split into
// triangles that fan out of their first corner.
//
// It keeps the vertices in temporary files in opts.TempDir, so that its
// memory does not grow with the mesh.
func Read(path string, opts Options, emit func(*mesh.Triangle) error) (info Info, err error) {
	file, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer file.Close() //nolint:errcheck
	stat, err := file.Stat()
	if err != nil {
		return Info{}, err
	}
	cache := opts.CacheBytes
	if cache <= 0 {
		cache = 256 << 20
	}
	r := &reader{dir: filepath.Dir(path), byName: map[string]uint32{}, emit: emit}
	r.info.Size = stat.Size()
	if r.positions, err = newTable(filepath.Join(opts.TempDir, "obj_positions.bin"), positionSize, cache/2); err != nil {
		return Info{}, err
	}
	defer func() { err = errors.Join(err, r.positions.close()) }()
	if r.texCoords, err = newTable(filepath.Join(opts.TempDir, "obj_texcoords.bin"), texCoordSize, cache/2); err != nil {
		return Info{}, err
	}
	defer func() { err = errors.Join(err, r.texCoords.close()) }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4<<20), maxLineSize)
	var read, reported int64
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Bytes()
		read += int64(len(text)) + 1
		if opts.Progress != nil && read-reported >= progressEvery {
			reported = read
			opts.Progress(read)
		}
		if err := r.line(text); err != nil {
			return Info{}, fmt.Errorf("%s, line %d: %w", filepath.Base(path), line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Info{}, err
	}
	r.info.Vertices = r.positions.count
	return r.info, nil
}

func (r *reader) line(text []byte) error {
	text = bytes.TrimLeft(text, " \t")
	if len(text) < 2 {
		return nil
	}
	switch {
	case text[0] == 'v' && isSpace(text[1]):
		return r.position(text[2:])
	case text[0] == 'v' && text[1] == 't' && len(text) > 2 && isSpace(text[2]):
		return r.texCoord(text[3:])
	case text[0] == 'f' && isSpace(text[1]):
		return r.face(text[2:])
	case bytes.HasPrefix(text, []byte("usemtl")):
		r.use(strings.TrimSpace(string(text[6:])))
	case bytes.HasPrefix(text, []byte("mtllib")):
		// A missing or broken library leaves the faces with plain colors.
		_ = r.library(strings.TrimSpace(string(text[6:])))
	}
	return nil
}

func (r *reader) position(text []byte) error {
	var p [3]float64
	for i := range p {
		var ok bool
		if p[i], text, ok = nextFloat(text); !ok {
			return errors.New("a vertex needs three coordinates")
		}
	}
	if !r.hasOrigin {
		r.hasOrigin = true
		r.info.Origin = [3]float64{math.Floor(p[0]), math.Floor(p[1]), math.Floor(p[2])}
	}
	var record [positionSize]byte
	for i := range p {
		binary.LittleEndian.PutUint32(record[4*i:], math.Float32bits(float32(p[i]-r.info.Origin[i])))
	}
	return r.positions.append(record[:])
}

func (r *reader) texCoord(text []byte) error {
	u, text, ok := nextFloat(text)
	if !ok {
		return errors.New("a texture coordinate needs a value")
	}
	// The second coordinate is optional, and 0 when missing.
	v, _, _ := nextFloat(text)
	var record [texCoordSize]byte
	binary.LittleEndian.PutUint32(record[0:], math.Float32bits(float32(u)))
	binary.LittleEndian.PutUint32(record[4:], math.Float32bits(float32(v)))
	return r.texCoords.append(record[:])
}

func (r *reader) face(text []byte) error {
	r.corners = r.corners[:0]
	for {
		text = bytes.TrimLeft(text, " \t\r")
		if len(text) == 0 {
			break
		}
		end := bytes.IndexAny(text, " \t\r")
		if end < 0 {
			end = len(text)
		}
		c, err := r.corner(text[:end])
		if err != nil {
			return err
		}
		r.corners = append(r.corners, c)
		text = text[end:]
	}
	if len(r.corners) < 3 {
		return errors.New("a face needs three corners")
	}
	material := r.current
	if !r.hasCurrent {
		material = r.none()
	}
	for i := 2; i < len(r.corners); i++ {
		t := mesh.Triangle{Material: material}
		for k, c := range []corner{r.corners[0], r.corners[i-1], r.corners[i]} {
			t.P[k], t.UV[k] = c.position, c.uv
		}
		if t.Degenerate() {
			continue
		}
		r.info.Triangles++
		if err := r.emit(&t); err != nil {
			return err
		}
	}
	return nil
}

// corner reads a corner of a face: the index of a position, then optionally
// a slash and the index of a texture coordinate, then a normal, not used.
func (r *reader) corner(text []byte) (corner, error) {
	var c corner
	first := text
	if slash := bytes.IndexByte(text, '/'); slash >= 0 {
		first = text[:slash]
		text = text[slash+1:]
		if second := bytes.IndexByte(text, '/'); second >= 0 {
			text = text[:second]
		}
	} else {
		text = nil
	}
	index, err := resolve(first, r.positions.count)
	if err != nil {
		return c, fmt.Errorf("vertex %w", err)
	}
	record, err := r.positions.get(index)
	if err != nil {
		return c, fmt.Errorf("vertex %w", err)
	}
	for i := range c.position {
		c.position[i] = math.Float32frombits(binary.LittleEndian.Uint32(record[4*i:]))
	}
	if len(text) == 0 {
		return c, nil
	}
	if index, err = resolve(text, r.texCoords.count); err != nil {
		return c, fmt.Errorf("texture coordinate %w", err)
	}
	if record, err = r.texCoords.get(index); err != nil {
		return c, fmt.Errorf("texture coordinate %w", err)
	}
	c.uv[0] = math.Float32frombits(binary.LittleEndian.Uint32(record[0:]))
	c.uv[1] = math.Float32frombits(binary.LittleEndian.Uint32(record[4:]))
	return c, nil
}

// resolve returns the zero-based index that text names among count items:
// positive indices count from the first item, negative ones back from the
// last.
func resolve(text []byte, count int64) (int64, error) {
	index, err := strconv.ParseInt(string(text), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("index %q is not a number", text)
	}
	if index < 0 {
		return count + index, nil
	}
	return index - 1, nil
}

// use makes name the material of the faces that follow.
func (r *reader) use(name string) {
	index, ok := r.byName[name]
	if !ok {
		index = r.add(mesh.Material{Name: name, Color: defaultColor})
	}
	r.current, r.hasCurrent = index, true
}

// none returns the material of faces that name none.
func (r *reader) none() uint32 {
	if !r.hasNone {
		r.hasNone = true
		r.noMaterial = r.add(mesh.Material{Name: "", Color: defaultColor})
	}
	return r.noMaterial
}

func (r *reader) add(m mesh.Material) uint32 {
	index := uint32(len(r.info.Materials))
	r.info.Materials = append(r.info.Materials, m)
	r.byName[m.Name] = index
	return index
}

// library reads the materials of the MTL file name, next to the OBJ file.
func (r *reader) library(name string) error {
	file, err := os.Open(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	current := -1
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "newmtl":
			name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "newmtl"))
			if index, ok := r.byName[name]; ok {
				current = int(index)
			} else {
				current = int(r.add(mesh.Material{Name: name, Color: defaultColor}))
			}
		case "Kd":
			if current >= 0 && len(fields) >= 4 {
				for i := range 3 {
					if v, err := strconv.ParseFloat(fields[i+1], 64); err == nil {
						r.info.Materials[current].Color[i] = uint8(math.Round(255 * min(max(v, 0), 1)))
					}
				}
			}
		case "map_Kd":
			if current >= 0 {
				r.info.Materials[current].Texture = r.texturePath(scanner.Text())
			}
		}
	}
	return scanner.Err()
}

// texturePath returns the absolute path of the image that a map_Kd line
// names: the rest of the line, or its last word when options precede it.
func (r *reader) texturePath(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "map_Kd"))
	if strings.HasPrefix(rest, "-") {
		fields := strings.Fields(rest)
		rest = fields[len(fields)-1]
	}
	rest = filepath.FromSlash(strings.ReplaceAll(rest, `\`, "/"))
	if filepath.IsAbs(rest) {
		return rest
	}
	return filepath.Join(r.dir, rest)
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' }

// pow10 holds the powers of ten that float64 represents exactly.
var pow10 = [...]float64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22}

// nextFloat reads the number at the start of text, after any spaces, and
// returns it with the text that follows it.
//
// Numbers of up to 15 digits and a small exponent, as OBJ files hold, take
// one exact multiplication or division, which rounds correctly. The others
// go through strconv.
func nextFloat(text []byte) (value float64, rest []byte, ok bool) {
	text = bytes.TrimLeft(text, " \t\r")
	end := bytes.IndexAny(text, " \t\r")
	if end < 0 {
		end = len(text)
	}
	token, rest := text[:end], text[end:]
	if len(token) == 0 {
		return 0, rest, false
	}
	i := 0
	negative := false
	if token[0] == '-' || token[0] == '+' {
		negative = token[0] == '-'
		i++
	}
	var mantissa uint64
	digits, exponent := 0, 0
	seenDot := false
	for ; i < len(token); i++ {
		c := token[i]
		if c == '.' && !seenDot {
			seenDot = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		if mantissa != 0 || c != '0' {
			digits++
		}
		if digits <= 15 {
			mantissa = mantissa*10 + uint64(c-'0')
			if seenDot {
				exponent--
			}
		} else if !seenDot {
			exponent++
		}
	}
	if i < len(token) {
		if token[i] != 'e' && token[i] != 'E' {
			return slowFloat(token, rest)
		}
		e, err := strconv.Atoi(string(token[i+1:]))
		if err != nil {
			return slowFloat(token, rest)
		}
		exponent += e
	}
	if digits > 15 || exponent < -22 || exponent > 22 {
		return slowFloat(token, rest)
	}
	value = float64(mantissa)
	if exponent < 0 {
		value /= pow10[-exponent]
	} else {
		value *= pow10[exponent]
	}
	if negative {
		value = -value
	}
	return value, rest, true
}

func slowFloat(token, rest []byte) (float64, []byte, bool) {
	value, err := strconv.ParseFloat(string(token), 64)
	return value, rest, err == nil
}
