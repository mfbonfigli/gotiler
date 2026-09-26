package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func touchFile(name string) error {
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	return file.Close()
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
