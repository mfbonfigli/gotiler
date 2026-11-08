package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/viewer"
	"github.com/mfbonfigli/gotiler/v3/tiler"
)

type viewerCall struct {
	tilesetPath string
	opts        viewer.Options
}

// fakeViewer replaces serveViewer for the test and records its calls.
func fakeViewer(t *testing.T, result error) *[]viewerCall {
	t.Helper()
	var calls []viewerCall
	original := serveViewer
	serveViewer = func(ctx context.Context, tilesetPath string, opts viewer.Options) error {
		calls = append(calls, viewerCall{tilesetPath, opts})
		return result
	}
	t.Cleanup(func() { serveViewer = original })
	return &calls
}

func newMockApp() (*tiler.MockTiler, func(args ...string) error) {
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) { return mockTiler, nil }})
	return mockTiler, func(args ...string) error {
		return app.Run(context.Background(), append([]string{"gotiler"}, args...))
	}
}

func TestViewCommandServesTheGivenTileset(t *testing.T) {
	calls := fakeViewer(t, nil)
	_, run := newMockApp()
	if err := run("view", "--port", "9000", "--ion-token", "ion-abc", "out/tileset.json"); err != nil {
		t.Fatalf("view: %v", err)
	}
	want := viewerCall{"out/tileset.json", viewer.Options{Port: 9000, IonToken: "ion-abc"}}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("viewer calls = %+v, want [%+v]", *calls, want)
	}
}

func TestViewCommandReadsTheIonTokenFromTheEnvironment(t *testing.T) {
	t.Setenv("CESIUM_ION_TOKEN", "ion-from-env")
	calls := fakeViewer(t, nil)
	_, run := newMockApp()
	if err := run("view", "out"); err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].opts.IonToken != "ion-from-env" {
		t.Errorf("viewer calls = %+v, want the token from CESIUM_ION_TOKEN", *calls)
	}
}

func TestViewCommandNeedsExactlyOneTileset(t *testing.T) {
	calls := fakeViewer(t, nil)
	_, run := newMockApp()
	for _, args := range [][]string{{"view"}, {"view", "a", "b"}} {
		if err := run(args...); err == nil || !strings.Contains(err.Error(), "must be set") {
			t.Errorf("%v: error = %v, want a missing tileset error", args, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("the viewer was started: %+v", *calls)
	}
}

func TestViewCommandReturnsViewerErrors(t *testing.T) {
	fakeViewer(t, errors.New("port in use"))
	_, run := newMockApp()
	if err := run("view", "out"); err == nil || err.Error() != "port in use" {
		t.Errorf("error = %v, want the viewer error", err)
	}
}

func TestPreviewOpensTheTiledFileInTheViewerInEditMode(t *testing.T) {
	calls := fakeViewer(t, nil)
	input := filepath.Join(t.TempDir(), "cloud.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	mockTiler, run := newMockApp()
	if err := run("-o", "out", "--crs", "EPSG:32633", "--preview", "--port", "9001", "--ion-token", "ion-abc", input); err != nil {
		t.Fatalf("tiling: %v", err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("the file was not tiled")
	}
	want := viewerCall{filepath.Join("out", "tileset.json"), viewer.Options{Port: 9001, IonToken: "ion-abc", StartEditing: true}}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("viewer calls = %+v, want [%+v]", *calls, want)
	}
}

func TestPreviewOpensAJoinedFolderInTheViewer(t *testing.T) {
	calls := fakeViewer(t, nil)
	input := t.TempDir()
	if err := touchFile(filepath.Join(input, "a.las")); err != nil {
		t.Fatal(err)
	}
	_, run := newMockApp()
	if err := run("-o", "out", "--crs", "EPSG:32633", "--join", "--preview", input); err != nil {
		t.Fatalf("tiling: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].tilesetPath != filepath.Join("out", "tileset.json") {
		t.Errorf("viewer calls = %+v, want the joined tileset", *calls)
	}
}

func TestPreviewRejectsRunsWithoutASingleTilesetBeforeTiling(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cloud.las")
	if err := touchFile(file); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"folder without --join", []string{"-o", "out", "--preview", t.TempDir()}, "--preview shows a single tileset"},
		{"3tz archive", []string{"-o", "out", "--preview", "--3tz", file}, "--preview cannot show .3tz archives"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := fakeViewer(t, nil)
			mockTiler, run := newMockApp()
			err := run(tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
			if mockTiler.ProcessFilesCalled || mockTiler.ProcessFolderCalled || len(*calls) != 0 {
				t.Error("tiling or the viewer started despite the invalid --preview")
			}
		})
	}
}

func TestTilingWithoutPreviewDoesNotStartTheViewer(t *testing.T) {
	calls := fakeViewer(t, nil)
	input := filepath.Join(t.TempDir(), "cloud.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	_, run := newMockApp()
	if err := run("-o", "out", "--crs", "EPSG:32633", "--port", "9001", input); err != nil {
		t.Fatalf("tiling: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("the viewer was started: %+v", *calls)
	}
}

// fakeBrowser replaces openInBrowser for the test and records the opened URLs.
func fakeBrowser(t *testing.T, result error) *[]string {
	t.Helper()
	var urls []string
	original := openInBrowser
	openInBrowser = func(url string) error {
		urls = append(urls, url)
		return result
	}
	t.Cleanup(func() { openInBrowser = original })
	return &urls
}

func writeTestTileset(t *testing.T) string {
	t.Helper()
	tileset := filepath.Join(t.TempDir(), "tileset.json")
	if err := os.WriteFile(tileset, []byte(`{"asset":{"version":"1.1"},"root":{"boundingVolume":{"box":[0,0,0,1,0,0,0,1,0,0,0,1]},"geometricError":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return tileset
}

func TestServeViewerOpensTheViewerInTheBrowserUntilTheContextEnds(t *testing.T) {
	urls := fakeBrowser(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serveViewer(ctx, writeTestTileset(t), viewer.Options{}); err != nil {
		t.Errorf("serveViewer = %v, want nil once the context has ended", err)
	}
	if len(*urls) != 1 || !strings.HasPrefix((*urls)[0], "http://127.0.0.1:") || !strings.Contains((*urls)[0], "token=") {
		t.Errorf("opened URLs = %v, want the viewer address with its token", *urls)
	}
}

func TestServeViewerRunsEvenIfTheBrowserCannotBeOpened(t *testing.T) {
	fakeBrowser(t, errors.New("no browser"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serveViewer(ctx, writeTestTileset(t), viewer.Options{}); err != nil {
		t.Errorf("serveViewer = %v, want nil when only the browser failed", err)
	}
}

func TestServeViewerRejectsAMissingTilesetWithoutOpeningTheBrowser(t *testing.T) {
	urls := fakeBrowser(t, nil)
	if err := serveViewer(context.Background(), filepath.Join(t.TempDir(), "missing.json"), viewer.Options{}); err == nil {
		t.Error("serveViewer accepted a missing tileset")
	}
	if len(*urls) != 0 {
		t.Errorf("the browser was opened: %v", *urls)
	}
}
