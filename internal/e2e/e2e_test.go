// Package e2e runs the whole pipeline — fetch, parse, merge, serve — against
// fake upstream servers, so the only thing left untested is the public
// internet.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/api"
	"github.com/ato113/quakefeed/internal/model"
	"github.com/ato113/quakefeed/internal/poller"
	"github.com/ato113/quakefeed/internal/source"
	"github.com/ato113/quakefeed/internal/store"
)

// One earthquake near Sındırgı, as each network would report it: slightly
// different epicentre, magnitude on a different scale, timestamps seconds
// apart, and the two Turkish sources publishing local time.
const (
	afadBody = `[{"eventID":"725481","location":"Sındırgı (Balıkesir)","latitude":"39.45100",` +
		`"longitude":"28.10200","depth":"10.10","type":"ML","magnitude":"3.6","date":"2026-10-31T17:58:05"}]`

	kandilliBody = `<html><pre>
Tarih      Saat      Enlem(N)  Boylam(E) Derinlik(km) MD   ML   Mw    Yer
2026.10.31 17:58:12   39.4600    28.1200      11.0    -.-  3.4  -.-   SINDIRGI (BALIKESIR)      Ilksel
2026.10.31 09:15:00   36.8000    34.6000       5.0    -.-  1.8  -.-   MERSIN MERKEZ (MERSIN)    Ilksel
</pre></html>`

	usgsBody = `{"features":[{"id":"us7000abcd","properties":{"mag":3.7,"place":"12 km NE of Sındırgı",` +
		`"time":1793458698000,"magType":"mb","type":"earthquake","url":"https://example.test/us7000abcd"},` +
		`"geometry":{"coordinates":[28.09,39.44,11.2]}}]}`
)

func serve(body string, status *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != nil && *status != http.StatusOK {
			w.WriteHeader(*status)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
}

func buildStack(t *testing.T, afadURL, kandilliURL, usgsURL string) (http.Handler, *store.Store, *poller.Poller) {
	t.Helper()
	fetchers := source.New(source.Config{
		AFADURL: afadURL, KandilliURL: kandilliURL, USGSURL: usgsURL,
		Timeout: 5 * time.Second,
	})
	st := store.New(store.DefaultMergeRule(), 7*24*time.Hour)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := poller.New(fetchers, st, poller.Options{Logger: quiet})
	handler := api.NewServer(st, api.Options{Logger: quiet}).Handler()
	return handler, st, p
}

func getJSON(t *testing.T, handler http.Handler, path string, into any) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if into != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), into); err != nil {
			t.Fatalf("decoding %s: %v\nbody: %s", path, err, recorder.Body.String())
		}
	}
	return recorder.Code
}

type eventsBody struct {
	Count  int           `json:"count"`
	Total  int           `json:"total"`
	Events []model.Event `json:"events"`
}

func TestThreeSourcesBecomeOneEvent(t *testing.T) {
	afad := serve(afadBody, nil)
	defer afad.Close()
	kandilli := serve(kandilliBody, nil)
	defer kandilli.Close()
	usgs := serve(usgsBody, nil)
	defer usgs.Close()

	handler, _, p := buildStack(t, afad.URL, kandilli.URL, usgs.URL)
	p.RefreshOnce(context.Background())

	var body eventsBody
	if code := getJSON(t, handler, "/v1/events?detailed=true", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	// Four observations in total, but the Sındırgı quake was seen by all
	// three networks, so there should be two events: that one and Mersin.
	if body.Total != 2 {
		t.Fatalf("got %d events, want 2\n%+v", body.Total, body.Events)
	}

	newest := body.Events[0]
	if len(newest.Sources) != 3 {
		t.Fatalf("sources = %v, want all three merged", newest.Sources)
	}
	if len(newest.Reports) != 3 {
		t.Errorf("reports = %d, want each source's own numbers kept", len(newest.Reports))
	}

	// AFAD leads, so its figures are the headline ones.
	if newest.Magnitude != 3.6 || newest.MagType != "ML" {
		t.Errorf("magnitude = %v %s, want AFAD's 3.6 ML", newest.Magnitude, newest.MagType)
	}
	if newest.Place != "Sındırgı (Balıkesir)" {
		t.Errorf("place = %q — Turkish characters may have been mangled", newest.Place)
	}

	// 17:58:05 Turkey time is 14:58:05 UTC.
	want := time.Date(2026, 10, 31, 14, 58, 5, 0, time.UTC)
	if !newest.Time.Equal(want) {
		t.Errorf("time = %s, want %s — check the timezone conversion", newest.Time, want)
	}

	// Every source's own report survives intact.
	bySource := map[model.Source]model.Observation{}
	for _, report := range newest.Reports {
		bySource[report.Source] = report
	}
	if bySource[model.SourceKandilli].Magnitude != 3.4 {
		t.Errorf("kandilli report = %+v", bySource[model.SourceKandilli])
	}
	if bySource[model.SourceUSGS].MagType != "mb" {
		t.Errorf("usgs report = %+v", bySource[model.SourceUSGS])
	}
}

func TestServiceStaysUpWhenASourceFails(t *testing.T) {
	down := http.StatusServiceUnavailable
	afad := serve(afadBody, nil)
	defer afad.Close()
	kandilli := serve("", &down)
	defer kandilli.Close()

	handler, _, p := buildStack(t, afad.URL, kandilli.URL, "")
	p.RefreshOnce(context.Background())

	var body eventsBody
	if code := getJSON(t, handler, "/v1/events", &body); code != http.StatusOK {
		t.Fatalf("status = %d, want the API to answer anyway", code)
	}
	if body.Total != 1 {
		t.Fatalf("got %d events, want AFAD's one", body.Total)
	}

	var health struct {
		Status   string   `json:"status"`
		Degraded []string `json:"degraded_sources"`
	}
	if code := getJSON(t, handler, "/healthz", &health); code != http.StatusOK {
		t.Errorf("healthz status = %d, want 200 while degraded", code)
	}
	if health.Status != "degraded" || len(health.Degraded) != 1 {
		t.Errorf("health = %+v", health)
	}
}

func TestDataSurvivesASourceGoingDownAfterASuccess(t *testing.T) {
	status := http.StatusOK
	afad := serve(afadBody, &status)
	defer afad.Close()

	handler, _, p := buildStack(t, afad.URL, "", "")
	p.RefreshOnce(context.Background())

	// The source now starts failing.
	status = http.StatusBadGateway
	p.RefreshOnce(context.Background())

	var body eventsBody
	getJSON(t, handler, "/v1/events", &body)
	if body.Total != 1 {
		t.Fatalf("got %d events; the last good data should still be served", body.Total)
	}
}

func TestFiltersWorkEndToEnd(t *testing.T) {
	afad := serve(afadBody, nil)
	defer afad.Close()
	kandilli := serve(kandilliBody, nil)
	defer kandilli.Close()

	handler, _, p := buildStack(t, afad.URL, kandilli.URL, "")
	p.RefreshOnce(context.Background())

	cases := []struct {
		query string
		want  int
	}{
		{"", 2},
		{"?min_mag=3", 1},
		{"?min_mag=10", 0},
		{"?bbox=39,28,40,29", 1},
		{"?source=kandilli", 2},
		{"?source=afad", 1},
		{"?limit=1", 1},
	}

	for _, testCase := range cases {
		var body eventsBody
		getJSON(t, handler, "/v1/events"+testCase.query, &body)
		if body.Count != testCase.want {
			t.Errorf("/v1/events%s returned %d events, want %d", testCase.query, body.Count, testCase.want)
		}
	}
}

func TestRefreshIsIdempotent(t *testing.T) {
	afad := serve(afadBody, nil)
	defer afad.Close()

	handler, _, p := buildStack(t, afad.URL, "", "")

	var first, second eventsBody
	p.RefreshOnce(context.Background())
	getJSON(t, handler, "/v1/events", &first)
	p.RefreshOnce(context.Background())
	getJSON(t, handler, "/v1/events", &second)

	if first.Total != second.Total {
		t.Fatalf("event count changed on a second refresh: %d then %d", first.Total, second.Total)
	}
	if first.Events[0].ID != second.Events[0].ID {
		t.Error("event id changed between refreshes")
	}
}
