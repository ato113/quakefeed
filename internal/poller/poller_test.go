package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
	"github.com/ato113/quakefeed/internal/source"
	"github.com/ato113/quakefeed/internal/store"
)

type fakeFetcher struct {
	name   model.Source
	result []model.Observation
	err    error
	calls  atomic.Int32
}

func (f *fakeFetcher) Name() model.Source { return f.name }

func (f *fakeFetcher) Fetch(ctx context.Context) ([]model.Observation, error) {
	f.calls.Add(1)
	return f.result, f.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func observation(source model.Source) model.Observation {
	return model.Observation{
		Source: source, Time: time.Now().UTC(),
		Latitude: 39, Longitude: 28, Magnitude: 3, MagType: "ML",
	}
}

func TestRefreshOnceCallsEverySource(t *testing.T) {
	afad := &fakeFetcher{name: model.SourceAFAD, result: []model.Observation{observation(model.SourceAFAD)}}
	usgs := &fakeFetcher{name: model.SourceUSGS, result: []model.Observation{observation(model.SourceUSGS)}}

	st := store.New(store.DefaultMergeRule(), time.Hour)
	New([]source.Fetcher{afad, usgs}, st, Options{Logger: quietLogger()}).RefreshOnce(context.Background())

	if afad.calls.Load() != 1 || usgs.calls.Load() != 1 {
		t.Fatalf("calls: afad=%d usgs=%d", afad.calls.Load(), usgs.calls.Load())
	}
	if events, _ := st.Events(store.Query{}); len(events) == 0 {
		t.Fatal("nothing landed in the store")
	}
}

func TestOneFailingSourceDoesNotStopTheOthers(t *testing.T) {
	broken := &fakeFetcher{name: model.SourceKandilli, err: errors.New("upstream down")}
	working := &fakeFetcher{name: model.SourceAFAD, result: []model.Observation{observation(model.SourceAFAD)}}

	st := store.New(store.DefaultMergeRule(), time.Hour)
	New([]source.Fetcher{broken, working}, st, Options{Logger: quietLogger()}).RefreshOnce(context.Background())

	events, _ := st.Events(store.Query{})
	if len(events) != 1 {
		t.Fatalf("got %d events, want the working source's data", len(events))
	}

	for _, health := range st.Health() {
		if health.Source == model.SourceKandilli && health.Healthy {
			t.Error("the failing source is marked healthy")
		}
		if health.Source == model.SourceAFAD && !health.Healthy {
			t.Error("the working source is marked unhealthy")
		}
	}
}

func TestRunRefreshesImmediatelyAndStopsOnCancel(t *testing.T) {
	fetcher := &fakeFetcher{name: model.SourceAFAD, result: []model.Observation{observation(model.SourceAFAD)}}
	st := store.New(store.DefaultMergeRule(), time.Hour)
	p := New([]source.Fetcher{fetcher}, st, Options{Interval: 20 * time.Millisecond, Logger: quietLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	// The first refresh happens without waiting for the interval.
	deadline := time.After(time.Second)
	for fetcher.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("no refresh within a second")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
