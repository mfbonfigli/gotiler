package pc

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

// attrRemapEntry copies one attribute value from a sub-reader's packed layout
// to the combined layout.
type attrRemapEntry struct {
	srcOff int
	dstOff int
	size   int
}

// CombinedPointCloudReader enables reading a a list of files as if they were a single one
// the files MUST have the same properties (SRID, etc)
type CombinedPointCloudReader struct {
	currentReader atomic.Int32
	readers       []pointcloud.Reader
	numPts        int
	crs           string
	schema        []model.AttributeDescriptor
	schemaSize    int
	// remaps[i] translates reader i's packed values to the combined schema
	// layout; nil when reader i's schema already matches (the common case).
	remaps  [][]attrRemapEntry
	remapMu sync.Mutex
	arena   model.AttributeValuesArena
}

// NewCombinedPointCloudReader creates a new file reader for the files passed as input. If crs is the empty string,
// each file's CRS is autodetected independently: an error is returned if the detected CRS are inconsistent across
// files or if no file carries a detectable CRS; files without CRS metadata inherit the CRS detected from the others.
// attrs lists the optional per-point attributes to emit; nil means none.
func NewCombinedPointCloudReader(files []string, crs string, eightBitColor bool, attrs model.Attributes) (*CombinedPointCloudReader, error) {
	r := &CombinedPointCloudReader{}
	readerOpts := plugin.ReaderOptions{EightBitColor: eightBitColor, RequestedAttributes: attrs}
	type input struct {
		file    string
		factory plugin.ReaderFactory
	}
	var inputs []input
	for _, f := range files {
		factory, ok := plugin.PointCloudReaderFactoryFor(f)
		if !ok {
			continue
		}
		inputs = append(inputs, input{file: f, factory: factory})
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no supported point cloud files found; registered extensions: %v", plugin.SupportedPointCloudExtensions())
	}
	readers := make([]pointcloud.Reader, len(inputs))
	closeAll := func() {
		for _, fr := range readers {
			if fr != nil {
				fr.Close()
			}
		}
	}
	if crs != "" {
		// the user provided a CRS: pass it to every factory, no autodetection
		for i, in := range inputs {
			fr, err := in.factory(in.file, crs, readerOpts)
			if err != nil {
				closeAll()
				return nil, err
			}
			readers[i] = fr
		}
	} else {
		// no CRS provided: let every reader autodetect its own, then check the
		// detections are consistent. Readers may refuse to open a file lacking
		// CRS metadata, so failed constructions are retried below with the CRS
		// inherited from the other files.
		openErrs := make([]error, len(inputs))
		for i, in := range inputs {
			fr, err := in.factory(in.file, "", readerOpts)
			if err != nil {
				openErrs[i] = err
				continue
			}
			readers[i] = fr
			detected := fr.GetCRS()
			if detected == "" {
				continue // no CRS in this file: it inherits the combined CRS
			}
			if crs == "" {
				crs = detected
			} else if crs != detected {
				closeAll()
				return nil, fmt.Errorf("no CRS was provided and inconsistent CRS were detected:\n%s\n\n and\n\n%s", crs, detected)
			}
		}
		if crs == "" {
			closeAll()
			for _, err := range openErrs {
				if err != nil {
					return nil, err
				}
			}
			return nil, fmt.Errorf("no CRS was provided and none could be detected from the input files")
		}
		for i, in := range inputs {
			if readers[i] != nil {
				continue
			}
			fr, err := in.factory(in.file, crs, readerOpts)
			if err != nil {
				closeAll()
				return nil, err
			}
			readers[i] = fr
		}
	}
	r.readers = readers
	for _, fr := range readers {
		r.numPts += fr.NumberOfPoints()
	}
	r.crs = crs
	if err := r.buildAttributeRemaps(); err != nil {
		closeAll()
		return nil, err
	}
	return r, nil
}

// buildAttributeRemaps adopts the first reader's schema as the combined schema
// and precomputes, for every other reader, how its packed values map onto it.
// Attributes a file does not carry are left as zero values for its points; a
// same-name attribute with a different type across files is an error.
func (m *CombinedPointCloudReader) buildAttributeRemaps() error {
	m.schema = m.readers[0].AttributeSchema()
	combined, size, err := model.AttributeSchemaLayout(m.schema)
	if err != nil {
		return err
	}
	m.schemaSize = size
	m.remaps = make([][]attrRemapEntry, len(m.readers))
	for i, reader := range m.readers {
		sub := reader.AttributeSchema()
		if schemasEqual(m.schema, sub) {
			continue // passthrough
		}
		subLayout, _, err := model.AttributeSchemaLayout(sub)
		if err != nil {
			return err
		}
		byName := make(map[string]model.AttributeLayoutEntry, len(subLayout))
		for _, e := range subLayout {
			byName[e.Name] = e
		}
		remap := []attrRemapEntry{} // non-nil marks "needs remapping" even when empty
		for _, dst := range combined {
			src, ok := byName[dst.Name]
			if !ok {
				continue // attribute absent from this file: stays zero
			}
			if src.Type != dst.Type {
				return fmt.Errorf("attribute %q has type %q in one input file and %q in another", dst.Name, src.Type, dst.Type)
			}
			remap = append(remap, attrRemapEntry{srcOff: src.Offset, dstOff: dst.Offset, size: dst.Size})
		}
		m.remaps[i] = remap
	}
	return nil
}

func schemasEqual(a, b []model.AttributeDescriptor) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AttributeSchema implements pointcloud.Reader. The combined schema is the
// first file's schema; points from files lacking one of its attributes carry
// zero values for it.
func (m *CombinedPointCloudReader) AttributeSchema() []model.AttributeDescriptor {
	return m.schema
}

func (m *CombinedPointCloudReader) NumberOfPoints() int {
	return m.numPts
}

func (m *CombinedPointCloudReader) GetCRS() string {
	return m.crs
}

func (m *CombinedPointCloudReader) GetNext() (geom.Point64, error) {
	for {
		currReader := int(m.currentReader.Load())
		if currReader >= len(m.readers) {
			return geom.Point64{}, io.EOF
		}
		pt, err := m.readers[currReader].GetNext()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// reader exhausted: try to move on to the next reader
				m.currentReader.CompareAndSwap(int32(currReader), int32(currReader)+1)
				continue
			}
			// a real read error must be surfaced, not treated as end-of-file
			return geom.Point64{}, err
		}
		if remap := m.remaps[currReader]; remap != nil {
			// This file's schema differs from the combined one: rewrite the
			// packed values into the combined layout (rare path).
			m.remapMu.Lock()
			blob := m.arena.Alloc(m.schemaSize)
			m.remapMu.Unlock()
			for _, e := range remap {
				copy(blob[e.dstOff:e.dstOff+e.size], pt.Attributes[e.srcOff:e.srcOff+e.size])
			}
			pt.Attributes = blob
		}
		return pt, nil
	}
}

func (m *CombinedPointCloudReader) Reset() error {
	m.currentReader.Store(0)
	for _, r := range m.readers {
		if err := r.Reset(); err != nil {
			return err
		}
	}
	return nil
}

func (m *CombinedPointCloudReader) Close() {
	for _, r := range m.readers {
		r.Close()
	}
}
