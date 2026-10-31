// Package store holds the merged catalogue in memory.
package store

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

// MergeRule decides when two observations describe the same earthquake.
//
// They never match exactly: networks place the epicentre tens of kilometres
// apart and their clocks disagree by seconds. The defaults are deliberately
// loose enough to catch the same quake and tight enough that a genuine
// aftershock stays a separate event.
type MergeRule struct {
	MaxTimeDelta time.Duration
	MaxDistKm    float64
}

// DefaultMergeRule is tuned for the Turkish networks plus USGS.
func DefaultMergeRule() MergeRule {
	return MergeRule{MaxTimeDelta: 45 * time.Second, MaxDistKm: 80}
}

func (r MergeRule) sameQuake(a, b model.Observation) bool {
	delta := a.Time.Sub(b.Time)
	if delta < 0 {
		delta = -delta
	}
	if delta > r.MaxTimeDelta {
		return false
	}
	return model.DistanceKm(a.Latitude, a.Longitude, b.Latitude, b.Longitude) <= r.MaxDistKm
}

// sourceRank orders the sources when their numbers disagree. The local
// networks site Turkish epicentres more precisely than a global one, so they
// supply the headline figures; USGS still contributes its own report.
var sourceRank = map[model.Source]int{
	model.SourceAFAD:     0,
	model.SourceKandilli: 1,
	model.SourceUSGS:     2,
}

// Merge groups observations into events.
//
// Observations are processed oldest first and compared against the events
// already built, so a quake seen by three networks becomes one event carrying
// three reports.
func Merge(observations []model.Observation, rule MergeRule) []model.Event {
	sorted := make([]model.Observation, len(observations))
	copy(sorted, observations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	var groups [][]model.Observation
	for _, observation := range sorted {
		placed := false
		// Only the tail can still be within the time window, so walk backwards
		// and stop as soon as the gap is too large.
		for i := len(groups) - 1; i >= 0; i-- {
			group := groups[i]
			last := group[len(group)-1]
			if observation.Time.Sub(last.Time) > rule.MaxTimeDelta {
				break
			}
			if hasSource(group, observation.Source) {
				continue
			}
			if rule.sameQuake(last, observation) {
				groups[i] = append(group, observation)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, []model.Observation{observation})
		}
	}

	events := make([]model.Event, 0, len(groups))
	for _, group := range groups {
		events = append(events, buildEvent(group))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Time.After(events[j].Time) })
	return events
}

func hasSource(group []model.Observation, source model.Source) bool {
	for _, observation := range group {
		if observation.Source == source {
			return true
		}
	}
	return false
}

func buildEvent(group []model.Observation) model.Event {
	primary := group[0]
	for _, observation := range group[1:] {
		if sourceRank[observation.Source] < sourceRank[primary.Source] {
			primary = observation
		}
	}

	sources := make([]model.Source, 0, len(group))
	for _, observation := range group {
		sources = append(sources, observation.Source)
	}
	sort.Slice(sources, func(i, j int) bool { return sourceRank[sources[i]] < sourceRank[sources[j]] })

	reports := make([]model.Observation, len(group))
	copy(reports, group)
	sort.Slice(reports, func(i, j int) bool {
		return sourceRank[reports[i].Source] < sourceRank[reports[j].Source]
	})

	return model.Event{
		ID:        eventID(group),
		Time:      primary.Time,
		Latitude:  primary.Latitude,
		Longitude: primary.Longitude,
		DepthKm:   primary.DepthKm,
		Magnitude: primary.Magnitude,
		MagType:   primary.MagType,
		Place:     primary.Place,
		Sources:   sources,
		Reports:   reports,
	}
}

// eventID is derived from the quake itself, not from any one source's id, so
// it stays the same across refreshes and no matter which sources reported it.
// Coordinates are rounded to roughly a kilometre and the time to the minute,
// which is coarse enough to survive a source revising its numbers slightly.
func eventID(group []model.Observation) string {
	primary := group[0]
	for _, observation := range group[1:] {
		if sourceRank[observation.Source] < sourceRank[primary.Source] {
			primary = observation
		}
	}

	key := fmt.Sprintf("%d|%.2f|%.2f",
		primary.Time.UTC().Truncate(time.Minute).Unix(),
		math.Round(primary.Latitude*100)/100,
		math.Round(primary.Longitude*100)/100,
	)
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:8])
}
