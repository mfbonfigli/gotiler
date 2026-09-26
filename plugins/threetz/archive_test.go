package threetz

import (
	"archive/zip"
	"crypto/md5"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveWritesIndexAsLastEntry(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tiles.3tz")
	a, err := New(out, WithAutoFinalize(false), WithModifiedTime(time.Unix(0, 0).UTC()))
	if err != nil {
		t.Fatalf("new archive: %v", err)
	}
	writeArchiveFile(t, a, "data/d.glb", []byte("glb"))
	writeArchiveFile(t, a, "tileset.json", []byte(`{"asset":{"version":"1.1"}}`))
	if err := a.Finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	if len(zr.File) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(zr.File))
	}
	indexFile := zr.File[len(zr.File)-1]
	if indexFile.Name != IndexFilename {
		t.Fatalf("expected index as last entry, got %q", indexFile.Name)
	}
	if indexFile.Method != MethodStore {
		t.Fatalf("expected uncompressed index, got method %d", indexFile.Method)
	}
	if indexFile.Comment != "" {
		t.Fatalf("expected empty index comment, got %q", indexFile.Comment)
	}

	indexData := readZipFile(t, indexFile)
	if len(indexData) != 48 {
		t.Fatalf("expected two 24-byte index entries, got %d bytes", len(indexData))
	}
	archiveData, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	expected := map[[16]byte]string{
		md5.Sum([]byte("data/d.glb")):   "data/d.glb",
		md5.Sum([]byte("tileset.json")): "tileset.json",
	}
	for i := 0; i < len(indexData); i += 24 {
		var hash [16]byte
		copy(hash[:], indexData[i:i+16])
		name, ok := expected[hash]
		if !ok {
			t.Fatalf("unexpected index hash at offset %d", i)
		}
		offset := binary.LittleEndian.Uint64(indexData[i+16 : i+24])
		localName := localHeaderName(t, archiveData, offset)
		if localName != name {
			t.Fatalf("index offset for %q points to %q", name, localName)
		}
	}
}

func TestArchiveAutoFinalizesWhenTilesetCloses(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tiles.3tz")
	a, err := New(out)
	if err != nil {
		t.Fatalf("new archive: %v", err)
	}
	writeArchiveFile(t, a, "data/d.glb", []byte("glb"))
	writeArchiveFile(t, a, "tileset.json", []byte(`{"asset":{"version":"1.1"}}`))
	if err := a.Finalize(); err != nil {
		t.Fatalf("second finalize should be idempotent: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	if got := zr.File[len(zr.File)-1].Name; got != IndexFilename {
		t.Fatalf("expected index as last entry, got %q", got)
	}
	if _, err := a.Open("data/late.glb"); err == nil {
		t.Fatalf("expected opening after finalize to fail")
	}
}

func TestArchiveDeflateEntriesAreReadable(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tiles.3tz")
	a, err := New(out, WithCompressionMethod(MethodDeflate))
	if err != nil {
		t.Fatalf("new archive: %v", err)
	}
	writeArchiveFile(t, a, "data/d.glb", []byte("hello hello hello"))
	writeArchiveFile(t, a, "tileset.json", []byte(`{"asset":{"version":"1.1"}}`))

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	if zr.File[0].Method != MethodDeflate {
		t.Fatalf("expected deflate method, got %d", zr.File[0].Method)
	}
	if got := string(readZipFile(t, zr.File[0])); got != "hello hello hello" {
		t.Fatalf("unexpected deflated payload %q", got)
	}
}

func TestArchiveRejectsInvalid3TZNames(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "tiles.zip")); err == nil {
		t.Fatalf("expected non-.3tz filename to fail")
	}
	a, err := New(filepath.Join(t.TempDir(), "tiles.3tz"), WithAutoFinalize(false))
	if err != nil {
		t.Fatalf("new archive: %v", err)
	}
	if _, err := a.Open("nested/model.3tz"); err == nil {
		t.Fatalf("expected nested .3tz name to fail")
	}
	if _, err := a.Open(IndexFilename); err == nil {
		t.Fatalf("expected reserved index name to fail")
	}
	if err := a.Finalize(); err == nil {
		t.Fatalf("expected missing tileset.json to fail")
	}
}

func writeArchiveFile(t *testing.T, a *Archive, name string, data []byte) {
	t.Helper()
	w, err := a.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close %s: %v", name, err)
	}
}

func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open zip file %s: %v", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read zip file %s: %v", f.Name, err)
	}
	return data
}

func localHeaderName(t *testing.T, data []byte, offset uint64) string {
	t.Helper()
	if offset+30 > uint64(len(data)) {
		t.Fatalf("local header offset %d outside archive", offset)
	}
	pos := int(offset)
	if sig := binary.LittleEndian.Uint32(data[pos : pos+4]); sig != 0x04034b50 {
		t.Fatalf("expected local file header signature at %d, got 0x%x", offset, sig)
	}
	nameLen := int(binary.LittleEndian.Uint16(data[pos+26 : pos+28]))
	extraLen := int(binary.LittleEndian.Uint16(data[pos+28 : pos+30]))
	start := pos + 30
	end := start + nameLen
	if end+extraLen > len(data) {
		t.Fatalf("local header name outside archive")
	}
	return string(data[start:end])
}
