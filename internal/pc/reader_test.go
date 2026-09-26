package pc

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

// mockReaderFactory is the pluggable behavior behind the ".mockpc" extension
// registered below; tests set it to control per-file reader construction.
var mockReaderFactory plugin.ReaderFactory

func init() {
	plugin.RegisterPointCloudReader(".mockpc", func(filename, crs string, opts plugin.ReaderOptions) (pointcloud.Reader, error) {
		return mockReaderFactory(filename, crs, opts)
	})
}

// crsDetectingMockFactory mimics GoLasReader's CRS handling: a provided CRS is
// stored and echoed back; with no CRS the file's own CRS (encoded in the file
// name as e.g. "tile_EPSG-32633.mockpc") is autodetected, and construction
// fails when the file carries none. calls, when non-nil, records the crs
// argument each file was constructed with.
func crsDetectingMockFactory(calls map[string][]string) plugin.ReaderFactory {
	return func(filename, crs string, opts plugin.ReaderOptions) (pointcloud.Reader, error) {
		if calls != nil {
			name := filepath.Base(filename)
			calls[name] = append(calls[name], crs)
		}
		if crs == "" {
			crs = detectMockCRS(filename)
			if crs == "" {
				return nil, fmt.Errorf("no CRS provided and was not possible to determine CRS from file %s", filename)
			}
		}
		return &MockLasReader{CRS: crs, Pts: []geom.Point64{{}}}, nil
	}
}

// detectMockCRS extracts the CRS embedded in a mock file name, e.g.
// "tile_EPSG-32633.mockpc" -> "EPSG:32633". Returns "" when absent.
func detectMockCRS(filename string) string {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	if i := strings.Index(base, "EPSG-"); i >= 0 {
		return "EPSG:" + base[i+len("EPSG-"):]
	}
	return ""
}

// erroringLasReader is a mock sub-reader whose GetNext always fails with a
// non-EOF error, simulating e.g. a corrupt chunk mid-file.
type erroringLasReader struct {
	MockLasReader
	Err error
}

func (e *erroringLasReader) GetNext() (geom.Point64, error) {
	return geom.Point64{}, e.Err
}

//go:embed testdata/*.las
var testdataLas embed.FS

func writeEmbeddedLasFiles(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	entries, err := testdataLas.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		data, err := testdataLas.ReadFile("testdata/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, e.Name())
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	return files, dir
}

func TestCombinedReader(t *testing.T) {
	files, _ := writeEmbeddedLasFiles(t)

	r, err := NewCombinedPointCloudReader(files, "EPSG:32633", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer r.Close()

	if actual := r.NumberOfPoints(); actual != 10*len(files) {
		t.Errorf("expected %d points got %d", 10*len(files), actual)
	}

	if actual := r.GetCRS(); actual != "EPSG:32633" {
		t.Errorf("expected epsg %d got epsg %s", 32633, actual)
	}

	for i := 0; i < r.NumberOfPoints(); i++ {
		_, err := r.GetNext()
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}
	_, err = r.GetNext()
	if err == nil {
		t.Errorf("expected error, got none")
	}
}

// TestCombinedReaderAutodetectInconsistentCRS ensures that, when no CRS is
// provided, mixing files whose autodetected CRS differ is an error instead of
// silently interpreting later files in the first file's CRS.
func TestCombinedReaderAutodetectInconsistentCRS(t *testing.T) {
	mockReaderFactory = crsDetectingMockFactory(nil)
	_, err := NewCombinedPointCloudReader([]string{"a_EPSG-32633.mockpc", "b_EPSG-32632.mockpc"}, "", false, nil)
	if err == nil {
		t.Fatalf("expected an inconsistent CRS error, got none")
	}
	if !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("expected the error to mention the CRS inconsistency, got: %v", err)
	}
}

// TestCombinedReaderAutodetectInheritsCRS ensures that, when no CRS is
// provided, a file with no detectable CRS inherits the CRS detected from the
// other files instead of failing.
func TestCombinedReaderAutodetectInheritsCRS(t *testing.T) {
	calls := map[string][]string{}
	mockReaderFactory = crsDetectingMockFactory(calls)
	r, err := NewCombinedPointCloudReader([]string{"a_EPSG-32633.mockpc", "b_nocrs.mockpc"}, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer r.Close()
	if actual := r.GetCRS(); actual != "EPSG:32633" {
		t.Errorf("expected the combined CRS to be the detected one, got %q", actual)
	}
	if actual := r.NumberOfPoints(); actual != 2 {
		t.Errorf("expected both files to contribute points, got %d", actual)
	}
	// the CRS-less file must end up constructed with the inherited CRS
	got := calls["b_nocrs.mockpc"]
	if len(got) == 0 || got[len(got)-1] != "EPSG:32633" {
		t.Errorf("expected the CRS-less file to be constructed with the inherited CRS, construction calls: %v", got)
	}
}

// TestCombinedReaderAutodetectEmptyCRSReaderInherits ensures a reader that
// constructs fine but detects no CRS simply inherits the combined CRS.
func TestCombinedReaderAutodetectEmptyCRSReaderInherits(t *testing.T) {
	mockReaderFactory = func(filename, crs string, opts plugin.ReaderOptions) (pointcloud.Reader, error) {
		// tolerates a missing CRS: GetCRS just returns the empty string
		return &MockLasReader{CRS: detectMockCRS(filename), Pts: []geom.Point64{{}}}, nil
	}
	r, err := NewCombinedPointCloudReader([]string{"a_EPSG-32633.mockpc", "b_nocrs.mockpc"}, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer r.Close()
	if actual := r.GetCRS(); actual != "EPSG:32633" {
		t.Errorf("expected the combined CRS to be the detected one, got %q", actual)
	}
}

// TestCombinedReaderProvidedCRSPassthrough ensures a user-provided CRS is
// passed to every factory as-is, with no autodetection and no error even when
// the files' own CRS metadata would disagree.
func TestCombinedReaderProvidedCRSPassthrough(t *testing.T) {
	calls := map[string][]string{}
	mockReaderFactory = crsDetectingMockFactory(calls)
	r, err := NewCombinedPointCloudReader([]string{"a_EPSG-32633.mockpc", "b_EPSG-32632.mockpc"}, "EPSG:4326", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer r.Close()
	if actual := r.GetCRS(); actual != "EPSG:4326" {
		t.Errorf("expected the provided CRS, got %q", actual)
	}
	for file, crs := range calls {
		if len(crs) != 1 || crs[0] != "EPSG:4326" {
			t.Errorf("file %q: expected a single construction with the provided CRS, got %v", file, crs)
		}
	}
}

// TestCombinedReaderAutodetectNoCRSAnywhere ensures an error is returned when
// no CRS was provided and none of the files carries a detectable one.
func TestCombinedReaderAutodetectNoCRSAnywhere(t *testing.T) {
	mockReaderFactory = crsDetectingMockFactory(nil)
	_, err := NewCombinedPointCloudReader([]string{"a_nocrs.mockpc", "b_nocrs.mockpc"}, "", false, nil)
	if err == nil {
		t.Fatalf("expected an error when no file has a detectable CRS, got none")
	}
}

// TestCombinedReaderGetNextPropagatesReadErrors ensures a non-EOF error from a
// sub-reader is surfaced to the caller instead of being treated as end-of-file
// and silently truncating the dataset by skipping to the next reader.
func TestCombinedReaderGetNextPropagatesReadErrors(t *testing.T) {
	sentinel := errors.New("corrupt chunk")
	r := &CombinedPointCloudReader{
		readers: []pointcloud.Reader{
			&erroringLasReader{Err: sentinel},
			&MockLasReader{Pts: []geom.Point64{{}}},
		},
		remaps: make([][]attrRemapEntry, 2),
	}
	_, err := r.GetNext()
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the sub-reader error to be propagated, got %v", err)
	}
	// the reader must not have advanced past the failing sub-reader
	_, err = r.GetNext()
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the error again on subsequent reads, got %v", err)
	}
}

func TestCombinedReaderConcurrency(t *testing.T) {
	files, _ := writeEmbeddedLasFiles(t)

	r, err := NewCombinedPointCloudReader(files, "EPSG:32633", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer r.Close()

	if actual := r.NumberOfPoints(); actual != 10*len(files) {
		t.Errorf("expected %d points got %d", 10*len(files), actual)
	}

	if actual := r.GetCRS(); actual != "EPSG:32633" {
		t.Errorf("expected epsg %d got epsg %s", 32633, actual)
	}

	e := make(chan error, 10)
	readFun := func(wg *sync.WaitGroup) {
		defer wg.Done()
		read := 0
		for i := 0; i < r.NumberOfPoints()/5; i++ {
			_, err := r.GetNext()
			if err != nil {
				e <- err
				t.Errorf("unexpected error %v", err)
				continue
			}
			read++
		}
	}
	wg := &sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go readFun(wg)
	}
	wg.Wait()
	if len(e) > 0 {
		t.Errorf("errors detected in the error channel but none expected")
	}
}
