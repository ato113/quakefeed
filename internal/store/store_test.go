package store

import (
	"errors"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

func newTestStore() *Store {
	return New(DefaultMergeRule(), 7*24*time.Hour)
}

func TestStoreMergesAcrossSources(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6)}, base)
	st.Replace(model.SourceUSGS, []model.Observation{obs(model.SourceUSGS, 5*time.Second, 39.44, 28.09, 3.7)}, base)

	events, total := st.Events(Query{})
	if total != 1 || len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if len(events[0].Sources) != 2 {
		t.Errorf("sources = %v", events[0].Sources)
	}
}

func TestFailedSourceKeepsServingItsLastData(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6)}, base)

	st.Fail(model.SourceAFAD, errors.New("upstream returned 503"), base.Add(time.Minute))

	events, _ := st.Events(Query{})
	if len(events) != 1 {
		t.Fatalf("data disappeared when the source failed: got %d events", len(events))
	}

	health := st.Health()
	if len(health) != 1 || health[0].Healthy {
		t.Fatalf("health = %+v, want afad marked unhealthy", health)
	}
	if health[0].LastSuccess == nil {
		t.Error("last success was cleared by a failure")
	}
}

func TestRetentionDropsOldObservations(t *testing.T) {
	st := New(DefaultMergeRule(), time.Hour)
	st.Replace(model.SourceAFAD, []model.Observation{
		obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6),
		obs(model.SourceAFAD, -2*time.Hour, 37.00, 35.00, 4.1),
	}, base)

	events, _ := st.Events(Query{})
	if len(events) != 1 {
		t.Fatalf("got %d events, want only the recent one", len(events))
	}
}

func TestFilterByMagnitude(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{
		obs(model.SourceAFAD, 0, 39.0, 28.0, 1.5),
		obs(model.SourceAFAD, 2*time.Hour, 37.0, 35.0, 4.2),
	}, base.Add(3*time.Hour))

	min := 3.0
	events, total := st.Events(Query{MinMag: &min})
	if len(events) != 1 || total != 1 || events[0].Magnitude != 4.2 {
		t.Fatalf("got %+v", events)
	}
}

func TestFilterByTimeWindow(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{
		obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0),
		obs(model.SourceAFAD, 3*time.Hour, 37.0, 35.0, 2.0),
	}, base.Add(4*time.Hour))

	since := base.Add(time.Hour)
	events, _ := st.Events(Query{Since: &since})
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestFilterByBoundingBox(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{
		obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0),
		obs(model.SourceAFAD, 2*time.Hour, 37.0, 35.0, 2.0),
	}, base.Add(3*time.Hour))

	box := &BBox{MinLat: 38, MinLon: 27, MaxLat: 40, MaxLon: 29}
	events, _ := st.Events(Query{BBox: box})
	if len(events) != 1 || events[0].Latitude != 39.0 {
		t.Fatalf("got %+v", events)
	}
}

func TestFilterBySource(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0)}, base)
	st.Replace(model.SourceUSGS, []model.Observation{obs(model.SourceUSGS, 2*time.Hour, 37.0, 35.0, 2.0)}, base.Add(3*time.Hour))

	events, _ := st.Events(Query{Source: model.SourceUSGS})
	if len(events) != 1 || events[0].Sources[0] != model.SourceUSGS {
		t.Fatalf("got %+v", events)
	}
}

func TestPagingReportsTotalBeforeTheLimit(t *testing.T) {
	var observations []model.Observation
	for i := 0; i < 10; i++ {
		observations = append(observations, obs(model.SourceAFAD, time.Duration(i)*time.Hour, 39.0+float64(i), 28.0, 2.0))
	}
	st := newTestStore()
	st.Replace(model.SourceAFAD, observations, base.Add(11*time.Hour))

	events, total := st.Events(Query{Limit: 3})
	if len(events) != 3 || total != 10 {
		t.Fatalf("got %d of %d, want 3 of 10", len(events), total)
	}

	page2, _ := st.Events(Query{Limit: 3, Offset: 3})
	if len(page2) != 3 || page2[0].ID == events[0].ID {
		t.Error("the second page repeats the first")
	}

	past, _ := st.Events(Query{Offset: 99})
	if len(past) != 0 {
		t.Error("an offset past the end should return nothing, not wrap")
	}
}

func TestReportsAreOmittedUnlessAskedFor(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0)}, base)

	plain, _ := st.Events(Query{})
	if plain[0].Reports != nil {
		t.Error("reports included without detailed=true")
	}

	detailed, _ := st.Events(Query{Detailed: true})
	if len(detailed[0].Reports) != 1 {
		t.Error("reports missing with detailed=true")
	}
}

func TestEventsReturnsACopy(t *testing.T) {
	st := newTestStore()
	st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0)}, base)

	events, _ := st.Events(Query{})
	events[0].Magnitude = 99

	again, _ := st.Events(Query{})
	if again[0].Magnitude == 99 {
		t.Error("callers can mutate the store's data")
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	st := newTestStore()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 200; i++ {
			st.Replace(model.SourceAFAD, []model.Observation{obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0)}, base)
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		st.Events(Query{})
		st.Health()
		st.Stats()
	}
	<-done
}
