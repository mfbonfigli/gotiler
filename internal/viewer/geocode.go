package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	nominatimEndpoint  = "https://nominatim.openstreetmap.org/search"
	nominatimUserAgent = "gotiler"
	// nominatimInterval is the minimum spacing between requests allowed by
	// the Nominatim usage policy.
	nominatimInterval = time.Second
	nominatimResults  = 5
)

type place struct {
	DisplayName string  `json:"displayName"`
	Longitude   float64 `json:"longitude"`
	Latitude    float64 `json:"latitude"`
}

// geocoder looks addresses up with Nominatim, following its usage policy: an
// identifying User-Agent, at most one request per second and cached results.
type geocoder struct {
	endpoint string
	interval time.Duration
	client   *http.Client

	mu          sync.Mutex
	cache       map[string][]place
	nextRequest time.Time
}

func newGeocoder() *geocoder {
	return &geocoder{
		endpoint: nominatimEndpoint,
		interval: nominatimInterval,
		client:   &http.Client{Timeout: 15 * time.Second},
		cache:    map[string][]place{},
	}
}

func (g *geocoder) search(ctx context.Context, query string) ([]place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("the search query is empty")
	}
	key := strings.ToLower(query)

	g.mu.Lock()
	if cached, ok := g.cache[key]; ok {
		g.mu.Unlock()
		return cached, nil
	}
	start := time.Now()
	if g.nextRequest.After(start) {
		start = g.nextRequest
	}
	g.nextRequest = start.Add(g.interval)
	g.mu.Unlock()

	if wait := time.Until(start); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	places, err := g.fetch(ctx, query)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.cache[key] = places
	g.mu.Unlock()
	return places, nil
}

func (g *geocoder) fetch(ctx context.Context, query string) ([]place, error) {
	params := url.Values{}
	params.Set("format", "jsonv2")
	params.Set("limit", strconv.Itoa(nominatimResults))
	params.Set("q", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", nominatimUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("address search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("address search failed: %s", resp.Status)
	}

	var results []struct {
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("address search returned an invalid response: %w", err)
	}
	places := make([]place, 0, len(results))
	for _, r := range results {
		latitude, latErr := strconv.ParseFloat(r.Lat, 64)
		longitude, lonErr := strconv.ParseFloat(r.Lon, 64)
		if latErr != nil || lonErr != nil {
			continue
		}
		places = append(places, place{DisplayName: r.DisplayName, Longitude: longitude, Latitude: latitude})
	}
	return places, nil
}
