package utils

import (
	"os"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

func CreateDirectoryIfDoesNotExist(directory string) error {
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		err := os.MkdirAll(directory, 0777)
		if err != nil {
			return err
		}
	}
	return nil
}

func TouchFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func FindPointCloudFilesInFolder(directory string) ([]string, error) {
	if _, err := os.Stat(directory); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !plugin.IsPointCloudExtension(e.Name()) {
			continue
		}
		f := filepath.Join(directory, e.Name())
		files = append(files, f)
	}
	return files, nil
}
