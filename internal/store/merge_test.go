package store

import (
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

var base = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func obs(source model.Source, offset time.Duration, lat, lon, mag float64) model.Observation {
	return model.Observation{
		Source:    source,
		Time:      base.Add(offset),
		Latitude:  lat,
		Longitude: lon,
		Magnitude: mag,
		MagType:   "ML",
		Place:     string(source) + " place",
	}
}

func TestMergeCombinesTheSameQuakeAcrossSources(t *testing.T) {
	observations := []model.Observation{
		obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6),
		obs(model.SourceKandilli, 8*time.Second, 39.46, 28.12, 3.4),
		obs(model.SourceUSGS, 20*time.Second, 39.44, 28.09, 3.7),
	}

	events := Merge(observations, DefaultMergeRule())

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if len(events[0].Sources) != 3 {
		t.Errorf("sources = %v, want all three", events[0].Sources)
	}
	if len(events[0].Reports) != 3 {
		t.Errorf("reports = %d, want 3", len(events[0].Reports))
	}
}

func TestMergeKeepsDistinctQuakesApart(t *testing.T) {
	observations := []model.Observation{
		obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6),
		// An hour later in the same place is an aftershock, not the same quake.
		obs(model.SourceKandilli, time.Hour, 39.45, 28.10, 2.9),
		// Same moment, but on the other side of the country.
		obs(model.SourceUSGS, 5*time.Second, 37.00, 35.00, 4.1),
	}

	if events := Merge(observations, DefaultMergeRule()); len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
}

func TestMergeNeverCombinesTwoReportsFromOneSource(t *testing.T) {
	// Two quakes seconds apart in the same spot, both seen by AFAD: a swarm.
	observations := []model.Observation{
		obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6),
		obs(model.SourceAFAD, 10*time.Second, 39.45, 28.10, 2.2),
	}

	if events := Merge(observations, DefaultMergeRule()); len(events) != 2 {
		t.Fatalf("got %d events, want 2 — one source's reports are distinct quakes", len(events))
	}
}

func TestMergePrefersTheLocalNetworkForHeadlineFigures(t *testing.T) {
	observations := []model.Observation{
		obs(model.SourceUSGS, 0, 39.44, 28.09, 3.7),
		obs(model.SourceAFAD, 5*time.Second, 39.45, 28.10, 3.6),
	}

	events := Merge(observations, DefaultMergeRule())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Magnitude != 3.6 || events[0].Latitude != 39.45 {
		t.Errorf("headline figures came from %v, want AFAD's", events[0])
	}
	if events[0].Sources[0] != model.SourceAFAD {
		t.Errorf("sources = %v, want afad first", events[0].Sources)
	}
}

func TestMergeReturnsNewestFirst(t *testing.T) {
	observations := []model.Observation{
		obs(model.SourceAFAD, 0, 39.0, 28.0, 2.0),
		obs(model.SourceAFAD, 2*time.Hour, 37.0, 35.0, 2.0),
		obs(model.SourceAFAD, time.Hour, 38.0, 30.0, 2.0),
	}

	events := Merge(observations, DefaultMergeRule())
	for i := 1; i < len(events); i++ {
		if events[i].Time.After(events[i-1].Time) {
			t.Fatalf("events are not newest first: %v", events)
		}
	}
}

func TestEventIDIsStableAcrossRefreshes(t *testing.T) {
	first := Merge([]model.Observation{obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6)}, DefaultMergeRule())
	// The same quake, with the source having nudged its numbers slightly.
	nudged := obs(model.SourceAFAD, 3*time.Second, 39.4512, 28.1009, 3.7)
	second := Merge([]model.Observation{nudged}, DefaultMergeRule())

	if first[0].ID != second[0].ID {
		t.Errorf("id changed after a minor revision: %s vs %s", first[0].ID, second[0].ID)
	}
}

func TestEventIDDiffersBetweenQuakes(t *testing.T) {
	events := Merge([]model.Observation{
		obs(model.SourceAFAD, 0, 39.45, 28.10, 3.6),
		obs(model.SourceAFAD, 2*time.Hour, 37.00, 35.00, 4.1),
	}, DefaultMergeRule())

	if events[0].ID == events[1].ID {
		t.Error("distinct quakes share an id")
	}
}

func TestMergeHandlesNoObservations(t *testing.T) {
	if events := Merge(nil, DefaultMergeRule()); len(events) != 0 {
		t.Errorf("got %d events, want none", len(events))
	}
}

func TestDistanceKm(t *testing.T) {
	// Istanbul to Ankara is about 350 km.
	got := model.DistanceKm(41.0082, 28.9784, 39.9334, 32.8597)
	if got < 330 || got > 370 {
		t.Errorf("distance = %.1f km, want roughly 350", got)
	}
	if model.DistanceKm(39, 28, 39, 28) != 0 {
		t.Error("distance to itself is not zero")
	}
}
