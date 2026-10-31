// Package model holds the normalized shape every source is mapped into.
package model

import (
	"math"
	"time"
)

// Source identifies where an observation came from.
type Source string

const (
	SourceAFAD     Source = "afad"
	SourceKandilli Source = "kandilli"
	SourceUSGS     Source = "usgs"
)

// Observation is one source's report of one earthquake.
//
// Sources disagree: the same quake gets a different epicentre, a different
// magnitude on a different scale, and a timestamp that may be seconds apart.
// An Observation keeps a single source's numbers intact; merging happens
// later and never overwrites what a source actually said.
type Observation struct {
	Source    Source    `json:"source"`
	SourceID  string    `json:"source_id"`
	Time      time.Time `json:"time"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	DepthKm   float64   `json:"depth_km"`
	Magnitude float64   `json:"magnitude"`
	// MagType is the scale: ML, Mw, Md, mb. Values on different scales are
	// not interchangeable, so the scale travels with the number.
	MagType string `json:"mag_type"`
	Place   string `json:"place"`
	URL     string `json:"url,omitempty"`
}

// Event is one earthquake, as agreed by the sources that saw it.
type Event struct {
	ID        string        `json:"id"`
	Time      time.Time     `json:"time"`
	Latitude  float64       `json:"latitude"`
	Longitude float64       `json:"longitude"`
	DepthKm   float64       `json:"depth_km"`
	Magnitude float64       `json:"magnitude"`
	MagType   string        `json:"mag_type"`
	Place     string        `json:"place"`
	Sources   []Source      `json:"sources"`
	Reports   []Observation `json:"reports,omitempty"`
}

const earthRadiusKm = 6371.0

// DistanceKm is the great-circle distance between two points.
func DistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(math.Min(1, a)))
}
