package viewer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFiles embed.FS

const (
	defaultShutdownGrace = 10 * time.Second
	maxSaveRequestBytes  = 64 * 1024
)

// contentTypes overrides the operating system's MIME table, which on Windows
// comes from the registry and can map .js to text/plain: browsers refuse to
// run module scripts served with such a type.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json",
	".glb":  "model/gltf-binary",
	".pnts": "application/octet-stream",
}

// Options configures a viewer server.
type Options struct {
	// Port is the local port to listen on; 0 picks a free one.
	Port int
	// IonToken is an optional Cesium ion access token. With it the page uses
	// Cesium World Imagery and World Terrain instead of the default imagery.
	IonToken string
}

// Server serves the viewer page for one tileset on 127.0.0.1.
type Server struct {
	tileset    *tilesetFile
	ionToken   string
	token      string
	listener   net.Listener
	httpServer *http.Server
	geocoder   *geocoder
	pages      *pageTracker
	stopping   chan struct{}
}

// NewServer opens the tileset, given its tileset.json or the folder holding
// it, and starts listening. Call Serve to handle requests.
func NewServer(tilesetPath string, opts Options) (*Server, error) {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	return newServer(tilesetPath, opts, assets)
}

func newServer(tilesetPath string, opts Options, assets fs.FS) (*Server, error) {
	tileset, err := openTileset(tilesetPath)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port)))
	if err != nil {
		return nil, fmt.Errorf("starting the viewer: %w", err)
	}
	s := &Server{
		tileset:  tileset,
		ionToken: opts.IonToken,
		token:    token,
		listener: listener,
		geocoder: newGeocoder(),
		pages:    newPageTracker(defaultShutdownGrace),
		stopping: make(chan struct{}),
	}
	s.httpServer = &http.Server{
		Handler:           s.routes(assets),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// URL returns the address of the viewer page, including the access token.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/?token=%s", s.listener.Addr(), s.token)
}

// Serve handles requests until ctx is done or the last viewer page has been
// closed for a few seconds, then shuts the server down.
func (s *Server) Serve(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.httpServer.Serve(s.listener) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	case <-s.pages.allPagesClosed():
	}

	close(s.stopping)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", withContentType(http.FileServerFS(assets)))
	mux.Handle("GET /tileset/", http.StripPrefix("/tileset", tileFileServer(filepath.Dir(s.tileset.path))))
	mux.HandleFunc("GET /api/config", s.requireToken(s.handleConfig))
	mux.HandleFunc("POST /api/save", s.requireToken(s.handleSave))
	mux.HandleFunc("GET /api/geocode", s.requireToken(s.handleGeocode))
	mux.HandleFunc("GET /api/events", s.requireToken(s.handleEvents))
	return s.restrictToLocalHost(mux)
}

// restrictToLocalHost rejects requests addressed to any other host name, which
// keeps web pages that rebind their domain to 127.0.0.1 from reading the
// tileset, and sets headers that keep the page URL and its token private.
func (s *Server) restrictToLocalHost(next http.Handler) http.Handler {
	port := strconv.Itoa(s.listener.Addr().(*net.TCPAddr).Port)
	allowedHosts := []string{net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("localhost", port)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(allowedHosts, strings.ToLower(r.Host)) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		given := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		next(w, r)
	}
}

// tileFileServer serves the files of the tileset folder, without directory
// listings.
func tileFileServer(dir string) http.Handler {
	files := withContentType(http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// withContentType sets the Content-Type of the files listed in contentTypes,
// treating a folder path as its index.html.
func withContentType(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		if contentType, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
			w.Header().Set("Content-Type", contentType)
		}
		files.ServeHTTP(w, r)
	})
}

type configResponse struct {
	TilesetURL     string `json:"tilesetUrl"`
	Editable       bool   `json:"editable"`
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`
	IonToken       string `json:"ionToken,omitempty"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, configResponse{
		TilesetURL:     "/tileset/" + url.PathEscape(filepath.Base(s.tileset.path)),
		Editable:       s.tileset.readOnlyReason == "",
		ReadOnlyReason: s.tileset.readOnlyReason,
		IonToken:       s.ionToken,
	})
}

type saveRequest struct {
	Transform []float64 `json:"transform"`
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	var request saveRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveRequestBytes)).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid save request: "+err.Error())
		return
	}
	if len(request.Transform) != 16 {
		writeJSONError(w, http.StatusBadRequest, "invalid save request: the transform must have 16 numbers")
		return
	}
	var transform [16]float64
	copy(transform[:], request.Transform)

	err := s.tileset.saveRootTransform(transform)
	switch {
	case errors.Is(err, errNotMovable):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errInvalidTransform):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) == "" {
		writeJSONError(w, http.StatusBadRequest, "the search query is empty")
		return
	}
	places, err := s.geocoder.search(r.Context(), query)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, places)
}

// handleEvents holds a server-sent events stream open for as long as the page
// is open, which is how the server knows when the last page has closed.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.pages.pageOpened()
	defer s.pages.pageClosed()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	select {
	case <-r.Context().Done():
	case <-s.stopping:
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
