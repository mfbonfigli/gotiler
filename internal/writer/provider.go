package writer

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

// NewDiskWriterProvider returns a WriterProvider rooted at basePath.
func NewDiskWriterProvider(basePath string) plugin.WriterProvider {
	return func(filename string) (io.WriteCloser, error) {
		filePath := filepath.Join(basePath, filepath.FromSlash(filename))
		if err := os.MkdirAll(filepath.Dir(filePath), 0777); err != nil {
			return nil, err
		}
		return os.Create(filePath)
	}
}

// PrefixWriterProvider scopes another provider under prefix.
func PrefixWriterProvider(wp plugin.WriterProvider, prefix string) plugin.WriterProvider {
	if prefix == "" {
		return wp
	}
	return func(filename string) (io.WriteCloser, error) {
		return wp(path.Join(prefix, filename))
	}
}
