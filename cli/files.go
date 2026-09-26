package cli

import (
	"os"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

func findPointCloudFilesInFolder(folder string) ([]string, error) {
	files, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		// registry-aware, so plugin-provided formats (e.g. e57) are picked up
		if !plugin.IsPointCloudExtension(f.Name()) {
			continue
		}
		out = append(out, filepath.Join(folder, f.Name()))
	}
	return out, nil
}
