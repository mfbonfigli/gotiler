package obj

import (
	"errors"
	"fmt"
	"os"
)

// pageRecords is how many records a page of a table holds.
const pageRecords = 1 << 15

// table is an append-only list of fixed-size records kept in a temporary
// file, with the most recently used pages cached in memory.
//
// OBJ faces refer to vertices by index, anywhere in the file before them, so
// the vertices must stay reachable while the faces are read. The table keeps
// them on disk, so that a mesh of any size is read in bounded memory.
type table struct {
	file       *os.File
	recordSize int
	// count is the number of records appended.
	count int64
	// tail is the page being filled, not yet in the file.
	tail []byte
	// pages holds the cached pages by their index, nil when not loaded.
	pages    []*page
	loaded   int
	maxPages int
	clock    uint64
}

type page struct {
	data    []byte
	lastUse uint64
}

// newTable creates a table of records of recordSize bytes in a new file at
// path, caching up to cacheBytes of them in memory.
func newTable(path string, recordSize, cacheBytes int) (*table, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &table{
		file:       f,
		recordSize: recordSize,
		tail:       make([]byte, 0, pageRecords*recordSize),
		maxPages:   max(2, cacheBytes/(pageRecords*recordSize)),
	}, nil
}

// append adds a record, which must be recordSize bytes long.
func (t *table) append(record []byte) error {
	t.tail = append(t.tail, record...)
	t.count++
	if len(t.tail) < cap(t.tail) {
		return nil
	}
	index := int(t.count/pageRecords) - 1
	if _, err := t.file.WriteAt(t.tail, int64(index)*int64(cap(t.tail))); err != nil {
		return err
	}
	// The page just filled is likely to be read soon: keep it cached.
	t.install(index, t.tail)
	t.tail = make([]byte, 0, pageRecords*t.recordSize)
	return nil
}

// get returns the record at index. The slice is valid until the next call of
// a method of the table.
func (t *table) get(index int64) ([]byte, error) {
	if index < 0 || index >= t.count {
		return nil, fmt.Errorf("index %d out of range, there are %d", index+1, t.count)
	}
	pageIndex := int(index / pageRecords)
	offset := int(index%pageRecords) * t.recordSize
	if int64(pageIndex) == t.count/pageRecords {
		return t.tail[offset : offset+t.recordSize], nil
	}
	var p *page
	if pageIndex < len(t.pages) {
		p = t.pages[pageIndex]
	}
	if p == nil {
		data := make([]byte, pageRecords*t.recordSize)
		if _, err := t.file.ReadAt(data, int64(pageIndex)*int64(len(data))); err != nil {
			return nil, err
		}
		p = t.install(pageIndex, data)
	}
	t.clock++
	p.lastUse = t.clock
	return p.data[offset : offset+t.recordSize], nil
}

// install caches data as the page at index, dropping the least recently used
// page when the cache is full.
func (t *table) install(index int, data []byte) *page {
	if t.loaded >= t.maxPages {
		oldest := -1
		for i, p := range t.pages {
			if p != nil && (oldest < 0 || p.lastUse < t.pages[oldest].lastUse) {
				oldest = i
			}
		}
		t.pages[oldest] = nil
		t.loaded--
	}
	for len(t.pages) <= index {
		t.pages = append(t.pages, nil)
	}
	t.clock++
	p := &page{data: data, lastUse: t.clock}
	t.pages[index] = p
	t.loaded++
	return p
}

// close closes and removes the file of the table.
func (t *table) close() error {
	name := t.file.Name()
	return errors.Join(t.file.Close(), os.Remove(name))
}
