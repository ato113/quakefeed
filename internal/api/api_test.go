package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
	"github.com/ato113/quakefeed/internal/store"
)

var base = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func obs(source model.Source, offset time.Duration, lat, lon, mag float64) model.Observation {
	return model.Observation{
		Source: source, Time: base.Add(offset),
		Latitude: lat, Longitude: lon, Magnitude: mag, MagType: "ML", Place: "somewhere",
	}
}

func newServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st := store.New(store.DefaultMergeRule(), 7*24*time.Hour)
	server := NewServer(st, Options{Now: func() time.Time { return base.Add(time.Hour) }})
	return server.Handler(), st
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestEventsReturnsJSON(t *testing.T) {
	handler, st := newServer(t)
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 3.2)}, base)

	recorder := get(t, handler, "/v1/events")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content type = %q", got)
	}

	var body eventsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body.Count != 1 || body.Total != 1 || len(body.Events) != 1 {
		t.Fatalf("body = %+v", body)
	}
}

func TestEventsFiltersByMagnitude(t *testing.T) {
	handler, st := newServer(t)
	st.Replace(model.SourceAFAD, []model.Observation{
		obs(model.SourceAFAD, 0, 39.0, 28.0, 1.1),
		obs(model.SourceAFAD, -2*time.Hour, 37.0, 35.0, 4.5),
	}, base)

	var body eventsResponse
	recorder := get(t, handler, "/v1/events?min_mag=3")
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)

	if body.Count != 1 || body.Events[0].Magnitude != 4.5 {
		t.Fatalf("body = %+v", body)
	}
}

func TestEventsAcceptsRelativeWindows(t *testing.T) {
	handler, _ := newServer(t)
	for _, window := range []string{"6h", "7d", "90m"} {
		if recorder := get(t, handler, "/v1/events?since="+window); recorder.Code != http.StatusOK {
			t.Errorf("since=%s gave status %d", window, recorder.Code)
		}
	}
}

func TestEventsRejectsBadInput(t *testing.T) {
	handler, _ := newServer(t)

	cases := map[string]string{
		"min_mag=abc":      "a non-numeric magnitude",
		"since=yesterday":  "an unparseable window",
		"bbox=1,2,3":       "a bbox with three numbers",
		"bbox=40,27,38,29": "a bbox with min above max",
		"source=richter":   "an unknown source",
		"limit=-1":         "a negative limit",
		"offset=-5":        "a negative offset",
		"since=2026-01-02T00:00:00Z&until=2026-01-01T00:00:00Z": "since after until",
	}

	for query, description := range cases {
		recorder := get(t, handler, "/v1/events?"+query)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", description, recorder.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		if body["error"] == "" {
			t.Errorf("%s: no error message in the body", description)
		}
	}
}

func TestLimitIsCapped(t *testing.T) {
	handler, _ := newServer(t)
	if recorder := get(t, handler, "/v1/events?limit=999999"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestDetailedIncludesPerSourceReports(t *testing.T) {
	handler, st := newServer(t)
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 3.2)}, base)

	var plain, detailed eventsResponse
	_ = json.Unmarshal(get(t, handler, "/v1/events").Body.Bytes(), &plain)
	_ = json.Unmarshal(get(t, handler, "/v1/events?detailed=true").Body.Bytes(), &detailed)

	if len(plain.Events[0].Reports) != 0 {
		t.Error("reports leaked into the plain response")
	}
	if len(detailed.Events[0].Reports) != 1 {
		t.Error("reports missing from the detailed response")
	}
}

func TestSourcesReportsHealth(t *testing.T) {
	handler, st := newServer(t)
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 3.2)}, base)
	st.Fail(model.SourceUSGS, errors.New("boom"), base)

	var body sourcesResponse
	_ = json.Unmarshal(get(t, handler, "/v1/sources").Body.Bytes(), &body)

	if len(body.Sources) != 2 {
		t.Fatalf("sources = %+v", body.Sources)
	}
	for _, health := range body.Sources {
		switch health.Source {
		case model.SourceAFAD:
			if !health.Healthy || health.Observations != 1 {
				t.Errorf("afad = %+v", health)
			}
		case model.SourceUSGS:
			if health.Healthy || health.LastError != "boom" {
				t.Errorf("usgs = %+v", health)
			}
		}
	}
}

func TestHealthzIsDegradedWhenOneSourceIsDown(t *testing.T) {
	handler, st := newServer(t)
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 3.2)}, base)
	st.Fail(model.SourceUSGS, errors.New("boom"), base)

	recorder := get(t, handler, "/healthz")
	// Degraded still answers 200: the service works, just with fewer sources.
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var body healthResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if body.Status != "degraded" || len(body.Degraded) != 1 {
		t.Fatalf("body = %+v", body)
	}
}

func TestHealthzIsUnhealthyWhenEverySourceIsDown(t *testing.T) {
	handler, st := newServer(t)
	st.Fail(model.SourceAFAD, errors.New("boom"), base)
	st.Fail(model.SourceUSGS, errors.New("boom"), base)

	recorder := get(t, handler, "/healthz")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestHealthzIsOkOnAFreshStart(t *testing.T) {
	handler, _ := newServer(t)
	recorder := get(t, handler, "/healthz")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestIndexListsEndpoints(t *testing.T) {
	handler, _ := newServer(t)
	if recorder := get(t, handler, "/"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	handler, _ := newServer(t)
	if recorder := get(t, handler, "/nope"); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestTurkishPlaceNamesSurviveEncoding(t *testing.T) {
	handler, st := newServer(t)
	observation := obs(model.SourceAFAD, 0, 39.0, 28.0, 3.2)
	observation.Place = "Sındırgı (Balıkesir)"
	st.Replace(model.SourceAFAD, []model.Observation{observation}, base)

	var body eventsResponse
	_ = json.Unmarshal(get(t, handler, "/v1/events").Body.Bytes(), &body)
	if body.Events[0].Place != "Sındırgı (Balıkesir)" {
		t.Errorf("place = %q", body.Events[0].Place)
	}
}
