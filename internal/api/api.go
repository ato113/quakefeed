// Package api exposes the merged catalogue over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ato113/quakefeed/internal/model"
	"github.com/ato113/quakefeed/internal/store"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// Server wires the store to HTTP handlers.
type Server struct {
	store   *store.Store
	log     *slog.Logger
	started time.Time
	now     func() time.Time
}

type Options struct {
	Logger *slog.Logger
	Now    func() time.Time
}

func NewServer(st *store.Store, opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Server{store: st, log: logger, started: now(), now: now}
}

// Handler returns the router, with logging and recovery already applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("GET /v1/sources", s.handleSources)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /", s.handleIndex)
	return recoverer(s.log, logging(s.log, mux))
}

type eventsResponse struct {
	Count     int           `json:"count"`
	Total     int           `json:"total"`
	Events    []model.Event `json:"events"`
	Generated time.Time     `json:"generated_at"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	query, err := parseQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	events, total := s.store.Events(query)
	writeJSON(w, http.StatusOK, eventsResponse{
		Count:     len(events),
		Total:     total,
		Events:    events,
		Generated: s.now().UTC(),
	})
}

type sourcesResponse struct {
	Sources []store.Health `json:"sources"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sourcesResponse{Sources: s.store.Health()})
}

type healthResponse struct {
	Status   string   `json:"status"`
	Events   int      `json:"events"`
	Uptime   string   `json:"uptime"`
	LastSync string   `json:"last_sync,omitempty"`
	Degraded []string `json:"degraded_sources,omitempty"`
}

// handleHealth reports degraded rather than failing when some sources are
// down: the service is still answering from the sources that work, and a
// load balancer should not pull it out for that.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	events, lastMerged := s.store.Stats()

	var degraded []string
	anyHealthy := false
	for _, health := range s.store.Health() {
		if health.Healthy {
			anyHealthy = true
		} else {
			degraded = append(degraded, string(health.Source))
		}
	}

	response := healthResponse{
		Status:   "ok",
		Events:   events,
		Uptime:   s.now().Sub(s.started).Round(time.Second).String(),
		Degraded: degraded,
	}
	if !lastMerged.IsZero() {
		response.LastSync = lastMerged.UTC().Format(time.RFC3339)
	}

	status := http.StatusOK
	switch {
	case !anyHealthy && len(degraded) > 0:
		response.Status = "unhealthy"
		status = http.StatusServiceUnavailable
	case len(degraded) > 0:
		response.Status = "degraded"
	}

	writeJSON(w, status, response)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "quakefeed",
		"endpoints": []string{
			"GET /v1/events?min_mag=&since=&until=&bbox=&source=&limit=&offset=&detailed=",
			"GET /v1/sources",
			"GET /healthz",
		},
	})
}

func parseQuery(r *http.Request) (store.Query, error) {
	values := r.URL.Query()
	query := store.Query{Limit: defaultLimit}

	if raw := values.Get("min_mag"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return query, fmt.Errorf("min_mag must be a number, got %q", raw)
		}
		query.MinMag = &value
	}

	if raw := values.Get("since"); raw != "" {
		when, err := parseTime(raw)
		if err != nil {
			return query, fmt.Errorf("since: %w", err)
		}
		query.Since = &when
	}
	if raw := values.Get("until"); raw != "" {
		when, err := parseTime(raw)
		if err != nil {
			return query, fmt.Errorf("until: %w", err)
		}
		query.Until = &when
	}
	if query.Since != nil && query.Until != nil && query.Since.After(*query.Until) {
		return query, errors.New("since must be before until")
	}

	if raw := values.Get("bbox"); raw != "" {
		box, err := parseBBox(raw)
		if err != nil {
			return query, err
		}
		query.BBox = box
	}

	if raw := values.Get("source"); raw != "" {
		source := model.Source(strings.ToLower(raw))
		switch source {
		case model.SourceAFAD, model.SourceKandilli, model.SourceUSGS:
			query.Source = source
		default:
			return query, fmt.Errorf("unknown source %q; use afad, kandilli or usgs", raw)
		}
	}

	if raw := values.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return query, fmt.Errorf("limit must be a positive whole number, got %q", raw)
		}
		if value > maxLimit {
			value = maxLimit
		}
		query.Limit = value
	}
	if raw := values.Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return query, fmt.Errorf("offset must be zero or more, got %q", raw)
		}
		query.Offset = value
	}

	query.Detailed = values.Get("detailed") == "true"
	return query, nil
}

// parseTime accepts RFC 3339, or a relative window such as "6h" or "7d",
// which is what people actually want to type.
func parseTime(raw string) (time.Time, error) {
	if when, err := time.Parse(time.RFC3339, raw); err == nil {
		return when.UTC(), nil
	}
	if strings.HasSuffix(raw, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err == nil && days > 0 {
			return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return time.Now().UTC().Add(-duration), nil
	}
	return time.Time{}, fmt.Errorf("expected an RFC 3339 timestamp or a window like 6h or 7d, got %q", raw)
}

func parseBBox(raw string) (*store.BBox, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("bbox needs four comma-separated numbers: min_lat,min_lon,max_lat,max_lon")
	}
	numbers := make([]float64, 4)
	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("bbox: %q is not a number", part)
		}
		numbers[i] = value
	}
	box := &store.BBox{MinLat: numbers[0], MinLon: numbers[1], MaxLat: numbers[2], MaxLon: numbers[3]}
	if box.MinLat > box.MaxLat || box.MinLon > box.MaxLon {
		return nil, errors.New("bbox: minimum values must not exceed the maximums")
	}
	return box, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
