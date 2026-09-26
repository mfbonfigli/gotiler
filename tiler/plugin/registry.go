package plugin

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

// ReaderOptions contains format-agnostic options passed to point cloud readers.
type ReaderOptions struct {
	EightBitColor       bool
	RequestedAttributes model.Attributes
}

// ReaderFactory opens a point cloud reader for one input file.
type ReaderFactory func(filename, crs string, opts ReaderOptions) (pointcloud.Reader, error)

var (
	readerMu sync.RWMutex
	readers  = map[string]ReaderFactory{}

	encoderMu sync.RWMutex
	encoders  = map[string]GeometryEncoderFactory{}
)

// RegisterPointCloudReader registers a reader factory for a file extension.
// The extension is case-insensitive and may be passed with or without a dot.
func RegisterPointCloudReader(extension string, factory ReaderFactory) {
	ext := normalizeExtension(extension)
	if ext == "" {
		panic("gotiler plugin: empty point cloud extension")
	}
	if factory == nil {
		panic(fmt.Sprintf("gotiler plugin: nil reader factory for %s", ext))
	}
	readerMu.Lock()
	defer readerMu.Unlock()
	if _, exists := readers[ext]; exists {
		panic(fmt.Sprintf("gotiler plugin: point cloud reader already registered for %s", ext))
	}
	readers[ext] = factory
}

// PointCloudReaderFactoryFor returns the registered reader factory for a path
// or extension.
func PointCloudReaderFactoryFor(filenameOrExt string) (ReaderFactory, bool) {
	ext := normalizeExtension(filenameOrExt)
	readerMu.RLock()
	defer readerMu.RUnlock()
	factory, ok := readers[ext]
	return factory, ok
}

// IsPointCloudExtension reports whether a path or extension has a registered
// reader.
func IsPointCloudExtension(filenameOrExt string) bool {
	_, ok := PointCloudReaderFactoryFor(filenameOrExt)
	return ok
}

// SupportedPointCloudExtensions returns the registered point cloud extensions,
// including their leading dots.
func SupportedPointCloudExtensions() []string {
	readerMu.RLock()
	defer readerMu.RUnlock()
	extensions := make([]string, 0, len(readers))
	for ext := range readers {
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions
}

// RegisterGeometryEncoder registers a named geometry encoder factory.
func RegisterGeometryEncoder(name string, factory GeometryEncoderFactory) {
	name = normalizeName(name)
	if name == "" {
		panic("gotiler plugin: empty geometry encoder name")
	}
	if factory == nil {
		panic(fmt.Sprintf("gotiler plugin: nil geometry encoder factory for %s", name))
	}
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if _, exists := encoders[name]; exists {
		panic(fmt.Sprintf("gotiler plugin: geometry encoder already registered for %s", name))
	}
	encoders[name] = factory
}

// GeometryEncoderFactoryFor returns the registered geometry encoder factory for
// a name.
func GeometryEncoderFactoryFor(name string) (GeometryEncoderFactory, bool) {
	name = normalizeName(name)
	encoderMu.RLock()
	defer encoderMu.RUnlock()
	factory, ok := encoders[name]
	return factory, ok
}

// SupportedGeometryEncoders returns registered encoder IDs.
func SupportedGeometryEncoders() []string {
	encoderMu.RLock()
	defer encoderMu.RUnlock()
	names := make([]string, 0, len(encoders))
	for name := range encoders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeExtension(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if ext := filepath.Ext(value); ext != "" {
		value = ext
	} else if strings.ContainsAny(value, `/\`) {
		value = filepath.Ext(value)
	}
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	return value
}

func normalizeName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}
