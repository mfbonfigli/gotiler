package viewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNominatim records the requests it receives and answers with body.
type fakeNominatim struct {
	t      *testing.T
	status int
	body   string

	mu       sync.Mutex
	requests []*http.Request
	times    []time.Time
}

func (f *fakeNominatim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.times = append(f.times, time.Now())
	f.mu.Unlock()
	w.WriteHeader(f.status)
	w.Write([]byte(f.body)) //nolint:errcheck
}

func (f *fakeNominatim) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

const nominatimRome = `[
	{"display_name": "Roma, Lazio, Italia", "lat": "41.8933203", "lon": "12.4829321", "type": "city"},
	{"display_name": "Broken entry", "lat": "north", "lon": "12"},
	{"display_name": "Rome, Georgia, United States", "lat": "34.257", "lon": "-85.1647"}
]`

func newTestGeocoder(t *testing.T, status int, body string) (*geocoder, *fakeNominatim) {
	t.Helper()
	fake := &fakeNominatim{t: t, status: status, body: body}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	g := newGeocoder()
	g.endpoint = server.URL + "/search"
	g.interval = 0
	return g, fake
}

func TestGeocoderReturnsPlacesWithValidCoordinates(t *testing.T) {
	g, fake := newTestGeocoder(t, http.StatusOK, nominatimRome)
	got, err := g.search(context.Background(), "  Rome  ")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := []place{
		{DisplayName: "Roma, Lazio, Italia", Longitude: 12.4829321, Latitude: 41.8933203},
		{DisplayName: "Rome, Georgia, United States", Longitude: -85.1647, Latitude: 34.257},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("places = %+v, want %+v", got, want)
	}

	r := fake.requests[0]
	query := r.URL.Query()
	if query.Get("q") != "Rome" || query.Get("format") != "jsonv2" || query.Get("limit") != "5" {
		t.Errorf("query = %v", query)
	}
	if r.Header.Get("User-Agent") != nominatimUserAgent {
		t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), nominatimUserAgent)
	}
}

func TestGeocoderReturnsAnEmptyListWhenNothingMatches(t *testing.T) {
	g, _ := newTestGeocoder(t, http.StatusOK, "[]")
	got, err := g.search(context.Background(), "nowhere at all")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("places = %#v, want an empty, non-nil list", got)
	}
}

func TestGeocoderCachesResultsIgnoringCase(t *testing.T) {
	g, fake := newTestGeocoder(t, http.StatusOK, nominatimRome)
	for _, query := range []string{"Rome", "rome", " ROME "} {
		if _, err := g.search(context.Background(), query); err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
	}
	if n := fake.requestCount(); n != 1 {
		t.Errorf("Nominatim received %d requests, want 1", n)
	}
}

func TestGeocoderSpacesRequestsByTheInterval(t *testing.T) {
	g, fake := newTestGeocoder(t, http.StatusOK, "[]")
	g.interval = 80 * time.Millisecond
	for _, query := range []string{"first", "second", "third"} {
		if _, err := g.search(context.Background(), query); err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
	}
	for i := 1; i < len(fake.times); i++ {
		if gap := fake.times[i].Sub(fake.times[i-1]); gap < 75*time.Millisecond {
			t.Errorf("requests %d and %d were %v apart, want at least the 80ms interval", i, i+1, gap)
		}
	}
}

func TestGeocoderStopsWaitingWhenTheRequestIsCanceled(t *testing.T) {
	g, fake := newTestGeocoder(t, http.StatusOK, "[]")
	g.interval = time.Hour
	if _, err := g.search(context.Background(), "first"); err != nil {
		t.Fatalf("search: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := g.search(ctx, "second"); err != context.DeadlineExceeded {
		t.Errorf("error = %v, want the context deadline", err)
	}
	if n := fake.requestCount(); n != 1 {
		t.Errorf("Nominatim received %d requests, want only the first", n)
	}
}

func TestGeocoderErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		query   string
		wantErr string
	}{
		{"empty query", http.StatusOK, "[]", "   ", "the search query is empty"},
		{"server error", http.StatusInternalServerError, "", "Rome", "address search failed: 500 Internal Server Error"},
		{"invalid response", http.StatusOK, "<html>", "Rome", "address search returned an invalid response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := newTestGeocoder(t, tc.status, tc.body)
			_, err := g.search(context.Background(), tc.query)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
