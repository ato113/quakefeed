package store

import (
	"sort"
	"sync"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

// Health records how a source last behaved.
type Health struct {
	Source       model.Source `json:"source"`
	LastSuccess  *time.Time   `json:"last_success,omitempty"`
	LastAttempt  *time.Time   `json:"last_attempt,omitempty"`
	LastError    string       `json:"last_error,omitempty"`
	Observations int          `json:"observations"`
	Healthy      bool         `json:"healthy"`
}

// Query describes a request for events.
type Query struct {
	Since    *time.Time
	Until    *time.Time
	MinMag   *float64
	Source   model.Source
	BBox     *BBox
	Limit    int
	Offset   int
	Detailed bool
}

// BBox is a geographic filter: south/west to north/east.
type BBox struct {
	MinLat, MinLon, MaxLat, MaxLon float64
}

func (b BBox) contains(event model.Event) bool {
	return event.Latitude >= b.MinLat && event.Latitude <= b.MaxLat &&
		event.Longitude >= b.MinLon && event.Longitude <= b.MaxLon
}

// Store keeps the merged catalogue and each source's last known state.
//
// Observations are kept per source so one source failing never removes its
// previous data: the merge simply runs again over whatever is still held.
// That is what lets the API keep serving when an upstream goes down.
type Store struct {
	mu           sync.RWMutex
	bySource     map[model.Source][]model.Observation
	health       map[model.Source]Health
	events       []model.Event
	rule         MergeRule
	retention    time.Duration
	lastMergedAt time.Time
}

// New returns an empty store. Observations older than retention are dropped
// on each update.
func New(rule MergeRule, retention time.Duration) *Store {
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	return &Store{
		bySource:  make(map[model.Source][]model.Observation),
		health:    make(map[model.Source]Health),
		rule:      rule,
		retention: retention,
	}
}

// Replace swaps in a source's latest observations and rebuilds the catalogue.
func (s *Store) Replace(source model.Source, observations []model.Observation, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bySource[source] = observations
	successAt := at
	previous := s.health[source]
	previous.Source = source
	previous.LastAttempt = &at
	previous.LastSuccess = &successAt
	previous.LastError = ""
	previous.Observations = len(observations)
	previous.Healthy = true
	s.health[source] = previous

	s.rebuild(at)
}

// Fail records that a source could not be refreshed. Its previous
// observations stay in the catalogue.
func (s *Store) Fail(source model.Source, err error, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.health[source]
	previous.Source = source
	previous.LastAttempt = &at
	previous.LastError = err.Error()
	previous.Healthy = false
	s.health[source] = previous
}

// rebuild recomputes the merged catalogue. The caller holds the lock.
func (s *Store) rebuild(now time.Time) {
	cutoff := now.Add(-s.retention)

	var all []model.Observation
	for source, observations := range s.bySource {
		kept := observations[:0:0]
		for _, observation := range observations {
			if observation.Time.After(cutoff) {
				kept = append(kept, observation)
			}
		}
		s.bySource[source] = kept
		all = append(all, kept...)
	}

	s.events = Merge(all, s.rule)
	s.lastMergedAt = now
}

// Events returns the events matching q, plus the total before paging.
func (s *Store) Events(q Query) ([]model.Event, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]model.Event, 0, len(s.events))
	for _, event := range s.events {
		if q.Since != nil && event.Time.Before(*q.Since) {
			continue
		}
		if q.Until != nil && event.Time.After(*q.Until) {
			continue
		}
		if q.MinMag != nil && event.Magnitude < *q.MinMag {
			continue
		}
		if q.BBox != nil && !q.BBox.contains(event) {
			continue
		}
		if q.Source != "" && !eventHasSource(event, q.Source) {
			continue
		}
		matched = append(matched, event)
	}

	total := len(matched)

	if q.Offset > 0 {
		if q.Offset >= len(matched) {
			return []model.Event{}, total
		}
		matched = matched[q.Offset:]
	}
	if q.Limit > 0 && len(matched) > q.Limit {
		matched = matched[:q.Limit]
	}

	out := make([]model.Event, len(matched))
	copy(out, matched)
	if !q.Detailed {
		for i := range out {
			out[i].Reports = nil
		}
	}
	return out, total
}

func eventHasSource(event model.Event, source model.Source) bool {
	for _, candidate := range event.Sources {
		if candidate == source {
			return true
		}
	}
	return false
}

// Health returns each source's state, ordered by name.
func (s *Store) Health() []Health {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Health, 0, len(s.health))
	for _, health := range s.health {
		out = append(out, health)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// Stats reports the catalogue size and when it was last rebuilt.
func (s *Store) Stats() (events int, lastMergedAt time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events), s.lastMergedAt
}
