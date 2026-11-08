package viewer

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestViewerPageReferencesEmbeddedFilesAndOneCesiumVersion(t *testing.T) {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatal(err)
	}
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}

	localReferences := regexp.MustCompile(`(?:src|href)="([^":]+)"`).FindAllStringSubmatch(string(page), -1)
	if len(localReferences) == 0 {
		t.Fatal("index.html references no local files")
	}
	for _, match := range localReferences {
		if _, err := fs.Stat(assets, match[1]); err != nil {
			t.Errorf("index.html references %q, which is not embedded: %v", match[1], err)
		}
	}

	versions := map[string]bool{}
	for _, match := range regexp.MustCompile(`cesium@([0-9.]+)/`).FindAllStringSubmatch(string(page), -1) {
		versions[match[1]] = true
	}
	if len(versions) != 1 {
		t.Errorf("index.html loads Cesium versions %v, want exactly one", versions)
	}
}

func TestServerSendsExplicitContentTypesForThePage(t *testing.T) {
	s, err := NewServer(writeTilesetFile(t, sampleTileset("")), Options{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.listener.Close()
	for target, want := range map[string]string{
		"/":           "text/html; charset=utf-8",
		"/viewer.js":  "text/javascript; charset=utf-8",
		"/viewer.css": "text/css; charset=utf-8",
	} {
		rec := do(s, "GET", target, "")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != want {
			t.Errorf("GET %s = %d with Content-Type %q, want 200 with %q", target, rec.Code, rec.Header().Get("Content-Type"), want)
		}
	}
	if rec := do(s, "GET", "/viewer.js", ""); !strings.Contains(rec.Body.String(), "from 'cesium'") {
		t.Error("viewer.js is not the embedded viewer script")
	}
}
