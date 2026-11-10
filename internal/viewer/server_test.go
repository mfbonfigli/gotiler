package viewer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const testPage = "<html>viewer page</html>"

func newTestServer(t *testing.T, tilesetName, tilesetContent string, opts Options) *Server {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tilesetName), []byte(tilesetContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "0.glb"), []byte("glb bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte(testPage)}}
	s, err := newServer(filepath.Join(dir, tilesetName), opts, assets)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	t.Cleanup(func() { s.listener.Close() })
	return s
}

func newMovableTestServer(t *testing.T) *Server {
	return newTestServer(t, "tileset.json", sampleTileset(transformJSON(placedTransform)), Options{})
}

// do sends a request straight to the server's handler, addressed to the
// server's own host unless host is given.
func do(s *Server, method, target, body string, host ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = s.listener.Addr().String()
	if len(host) > 0 {
		req.Host = host[0]
	}
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func withToken(s *Server, target string) string {
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return target + separator + "token=" + s.token
}

func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("response %q is not JSON: %v", rec.Body.String(), err)
	}
}

func TestServerURLCarriesARandomToken(t *testing.T) {
	a, b := newMovableTestServer(t), newMovableTestServer(t)
	if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/\?token=[0-9a-f]{32}$`).MatchString(a.URL()) {
		t.Errorf("URL = %q", a.URL())
	}
	if a.token == b.token {
		t.Error("two servers got the same token")
	}
}

func TestServerOnlyAnswersRequestsForItsOwnHost(t *testing.T) {
	s := newMovableTestServer(t)
	port := s.listener.Addr().(*net.TCPAddr).Port
	for _, host := range []string{fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("localhost:%d", port), fmt.Sprintf("LocalHost:%d", port)} {
		if rec := do(s, "GET", "/", "", host); rec.Code != http.StatusOK {
			t.Errorf("host %q got status %d, want 200", host, rec.Code)
		}
	}
	for _, host := range []string{fmt.Sprintf("attacker.example:%d", port), "127.0.0.1:1", fmt.Sprintf("192.168.1.10:%d", port), ""} {
		if rec := do(s, "GET", "/tileset/tileset.json", "", host); rec.Code != http.StatusForbidden {
			t.Errorf("host %q got status %d, want 403", host, rec.Code)
		}
	}
}

func TestServerAPIsRequireTheToken(t *testing.T) {
	s := newMovableTestServer(t)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/config"},
		{"POST", "/api/save"},
		{"GET", "/api/geocode?q=Rome"},
		{"GET", "/api/events"},
	} {
		for _, target := range []string{endpoint.path, withToken(&Server{token: "0123"}, endpoint.path)} {
			rec := do(s, endpoint.method, target, "")
			if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "missing or invalid token") {
				t.Errorf("%s %s got %d %q, want 401", endpoint.method, target, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestServerServesThePageAndTheTilesetFiles(t *testing.T) {
	s := newTestServer(t, "my tiles.json", sampleTileset(""), Options{})
	tests := []struct {
		target      string
		status      int
		body        string
		contentType string
	}{
		{"/", http.StatusOK, testPage, ""},
		{"/tileset/my%20tiles.json", http.StatusOK, sampleTileset(""), "application/json"},
		{"/tileset/data/0.glb", http.StatusOK, "glb bytes", "model/gltf-binary"},
		{"/tileset/data/", http.StatusNotFound, "", ""},
		{"/tileset/data/missing.glb", http.StatusNotFound, "", ""},
	}
	for _, tc := range tests {
		rec := do(s, "GET", tc.target, "")
		if rec.Code != tc.status {
			t.Errorf("GET %s: status %d, want %d", tc.target, rec.Code, tc.status)
			continue
		}
		if tc.body != "" && rec.Body.String() != tc.body {
			t.Errorf("GET %s: body %q, want %q", tc.target, rec.Body.String(), tc.body)
		}
		if tc.contentType != "" && rec.Header().Get("Content-Type") != tc.contentType {
			t.Errorf("GET %s: Content-Type %q, want %q", tc.target, rec.Header().Get("Content-Type"), tc.contentType)
		}
		for header, want := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff"} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("GET %s: %s = %q, want %q", tc.target, header, got, want)
			}
		}
		if rec.Code == http.StatusOK && rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("GET %s: Cache-Control = %q, want no-cache", tc.target, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestNewServerServesTheEmbeddedPage(t *testing.T) {
	s, err := NewServer(writeTilesetFile(t, sampleTileset("")), Options{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.listener.Close()
	rec := do(s, "GET", "/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>GoTiler Viewer</title>") {
		t.Errorf("GET / = %d %q, want the embedded viewer page", rec.Code, rec.Body.String())
	}
}

func TestServerDoesNotServeFilesOutsideTheTilesetFolder(t *testing.T) {
	s := newMovableTestServer(t)
	secret := filepath.Join(filepath.Dir(filepath.Dir(s.tileset.path)), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/tileset/../secret.txt", "/tileset/%2e%2e/secret.txt", "/tileset/..%2fsecret.txt", "/secret.txt"} {
		rec := do(s, "GET", target, "")
		if strings.Contains(rec.Body.String(), "top secret") {
			t.Errorf("GET %s served a file outside the tileset folder", target)
		}
	}
}

func TestServerConfig(t *testing.T) {
	t.Run("movable tileset", func(t *testing.T) {
		s := newTestServer(t, "tileset.json", sampleTileset(""), Options{IonToken: "ion-123"})
		rec := do(s, "GET", withToken(s, "/api/config"), "")
		var got configResponse
		decodeJSONBody(t, rec, &got)
		want := configResponse{TilesetURL: "/tileset/tileset.json", Editable: true, IonToken: "ion-123"}
		if got != want {
			t.Errorf("config = %+v, want %+v", got, want)
		}
	})
	t.Run("splat tileset", func(t *testing.T) {
		s := newTestServer(t, "tileset.json", splatTileset(splatExtensions), Options{})
		rec := do(s, "GET", withToken(s, "/api/config"), "")
		var got configResponse
		decodeJSONBody(t, rec, &got)
		if !got.Splats {
			t.Errorf("config = %+v, want splat mode", got)
		}
	})
	t.Run("starting in edit mode", func(t *testing.T) {
		s := newTestServer(t, "tileset.json", sampleTileset(""), Options{StartEditing: true})
		rec := do(s, "GET", withToken(s, "/api/config"), "")
		var got configResponse
		decodeJSONBody(t, rec, &got)
		if !got.StartEditing {
			t.Errorf("config = %+v, want startEditing", got)
		}
	})
	t.Run("read-only tileset", func(t *testing.T) {
		s := newTestServer(t, "tileset.json", `{"root":{"boundingVolume":{"region":[0,0,1,1,0,1]},"geometricError":1}}`, Options{})
		rec := do(s, "GET", withToken(s, "/api/config"), "")
		var got map[string]any
		decodeJSONBody(t, rec, &got)
		if got["editable"] != false || !strings.Contains(fmt.Sprint(got["readOnlyReason"]), "region bounding volumes") {
			t.Errorf("config = %v, want a read-only tileset with its reason", got)
		}
		if _, ok := got["ionToken"]; ok {
			t.Errorf("config = %v, want no ion token when none is set", got)
		}
	})
}

func TestServerSavesTheRootTransform(t *testing.T) {
	s := newMovableTestServer(t)
	moved := columnMajor(rotationZ(25).Mul(linearPart(placedTransform)), 4517600, 1096960, 4382040)
	body, _ := json.Marshal(map[string]any{"transform": moved})

	rec := do(s, "POST", withToken(s, "/api/save"), string(body))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d %q, want 204", rec.Code, rec.Body.String())
	}
	if got := mustOpenTileset(t, s.tileset.path).rootTransform(); got != moved {
		t.Errorf("saved root transform = %v, want %v", got, moved)
	}
}

func TestServerSaveErrors(t *testing.T) {
	scaledUp := columnMajor(scaled(linearPart(placedTransform), 2), 1, 2, 3)
	scaledBody, _ := json.Marshal(map[string]any{"transform": scaledUp})
	tests := []struct {
		name    string
		content string
		body    string
		status  int
		wantErr string
	}{
		{"not JSON", "", "{", http.StatusBadRequest, "invalid save request"},
		{"too few numbers", "", `{"transform":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0]}`, http.StatusBadRequest, "the transform must have 16 numbers"},
		{"no transform", "", `{}`, http.StatusBadRequest, "the transform must have 16 numbers"},
		{"too large", "", `{"transform":[` + strings.Repeat("0,", maxSaveRequestBytes) + `0]}`, http.StatusBadRequest, "invalid save request"},
		{"not rigid", "", string(scaledBody), http.StatusBadRequest, "not scale, skew or mirror it"},
		{"read-only tileset", `{"root":{"boundingVolume":{"region":[0,0,1,1,0,1]},"geometricError":1}}`, `{"transform":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]}`, http.StatusConflict, "the tileset cannot be moved"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.content
			if content == "" {
				content = sampleTileset(transformJSON(placedTransform))
			}
			s := newTestServer(t, "tileset.json", content, Options{})
			rec := do(s, "POST", withToken(s, "/api/save"), tc.body)
			var got map[string]string
			decodeJSONBody(t, rec, &got)
			if rec.Code != tc.status || !strings.Contains(got["error"], tc.wantErr) {
				t.Errorf("got %d %q, want %d with an error containing %q", rec.Code, got["error"], tc.status, tc.wantErr)
			}
			if data, _ := os.ReadFile(s.tileset.path); string(data) != content {
				t.Error("a failed save changed the tileset")
			}
		})
	}
}

func TestServerGeocode(t *testing.T) {
	s := newMovableTestServer(t)
	fake := &fakeNominatim{t: t, status: http.StatusOK, body: nominatimRome}
	upstream := httptest.NewServer(fake)
	defer upstream.Close()
	s.geocoder.endpoint = upstream.URL
	s.geocoder.interval = 0

	rec := do(s, "GET", withToken(s, "/api/geocode?q=Rome"), "")
	var places []place
	decodeJSONBody(t, rec, &places)
	if rec.Code != http.StatusOK || len(places) != 2 || places[0].DisplayName != "Roma, Lazio, Italia" {
		t.Errorf("got %d %+v", rec.Code, places)
	}

	if rec := do(s, "GET", withToken(s, "/api/geocode?q=%20"), ""); rec.Code != http.StatusBadRequest {
		t.Errorf("empty query got status %d, want 400", rec.Code)
	}

	fake.status = http.StatusServiceUnavailable
	if rec := do(s, "GET", withToken(s, "/api/geocode?q=Paris"), ""); rec.Code != http.StatusBadGateway {
		t.Errorf("failing upstream got status %d, want 502", rec.Code)
	}
}

// serveInBackground runs Serve and returns a channel receiving its result.
func serveInBackground(ctx context.Context, s *Server, grace time.Duration) <-chan error {
	s.pages = newPageTracker(grace)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	return done
}

// openPage opens the events stream a viewer page keeps open and returns a
// function closing it.
func openPage(t *testing.T, s *Server) (closePage func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := fmt.Sprintf("http://%s/api/events?token=%s", s.listener.Addr(), s.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening the events stream: %v", err)
	}
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("events Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if line, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("first event line = %q, %v", line, err)
	}
	return func() {
		cancel()
		resp.Body.Close()
	}
}

func requireStopped(t *testing.T, done <-chan error, within time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	case <-time.After(within):
		t.Fatalf("Serve still running after %v", within)
	}
}

func requireRunning(t *testing.T, done <-chan error, during time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("Serve stopped early: %v", err)
	case <-time.After(during):
	}
}

func TestServeStopsOnceEveryPageHasClosed(t *testing.T) {
	s := newMovableTestServer(t)
	done := serveInBackground(context.Background(), s, 50*time.Millisecond)

	requireRunning(t, done, 150*time.Millisecond)
	closeFirst := openPage(t, s)
	closeSecond := openPage(t, s)
	closeFirst()
	requireRunning(t, done, 200*time.Millisecond)
	closeSecond()
	requireStopped(t, done, 2*time.Second)
}

func TestServeSurvivesAPageReload(t *testing.T) {
	s := newMovableTestServer(t)
	done := serveInBackground(context.Background(), s, 300*time.Millisecond)

	closePage := openPage(t, s)
	closePage()
	closePage = openPage(t, s)
	requireRunning(t, done, 500*time.Millisecond)
	closePage()
	requireStopped(t, done, 2*time.Second)
}

func TestServeStopsWhenTheContextEndsEvenWithOpenPages(t *testing.T) {
	s := newMovableTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := serveInBackground(ctx, s, time.Hour)
	closePage := openPage(t, s)
	defer closePage()

	cancel()
	requireStopped(t, done, 2*time.Second)
}

func TestNewServerErrors(t *testing.T) {
	if _, err := newServer(filepath.Join(t.TempDir(), "missing.json"), Options{}, fstest.MapFS{}); err == nil {
		t.Error("a missing tileset was accepted")
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	path := writeTilesetFile(t, sampleTileset(""))
	_, err = newServer(path, Options{Port: busy.Addr().(*net.TCPAddr).Port}, fstest.MapFS{})
	if err == nil || !strings.Contains(err.Error(), "starting the viewer") {
		t.Errorf("error = %v, want a listen error", err)
	}
}
