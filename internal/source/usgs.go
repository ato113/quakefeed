package source

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

// usgsFeed is the subset of the GeoJSON feed this service uses.
// Coordinates are [longitude, latitude, depth] — note the order.
type usgsFeed struct {
	Features []struct {
		ID         string `json:"id"`
		Properties struct {
			Mag     *float64 `json:"mag"`
			Place   string   `json:"place"`
			Time    int64    `json:"time"`
			URL     string   `json:"url"`
			MagType string   `json:"magType"`
			Type    string   `json:"type"`
		} `json:"properties"`
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

// ParseUSGS reads the USGS GeoJSON summary feed.
func ParseUSGS(data []byte) ([]model.Observation, error) {
	var feed usgsFeed
	if err := json.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("decoding GeoJSON: %w", err)
	}

	out := make([]model.Observation, 0, len(feed.Features))
	for _, feature := range feed.Features {
		// The feed carries quarry blasts and explosions too.
		if feature.Properties.Type != "" && feature.Properties.Type != "earthquake" {
			continue
		}
		if len(feature.Geometry.Coordinates) < 2 || feature.Properties.Mag == nil {
			continue
		}

		depth := 0.0
		if len(feature.Geometry.Coordinates) >= 3 {
			depth = feature.Geometry.Coordinates[2]
		}

		out = append(out, model.Observation{
			Source:   model.SourceUSGS,
			SourceID: feature.ID,
			// USGS timestamps are milliseconds since the epoch, UTC.
			Time:      time.UnixMilli(feature.Properties.Time).UTC(),
			Longitude: feature.Geometry.Coordinates[0],
			Latitude:  feature.Geometry.Coordinates[1],
			DepthKm:   depth,
			Magnitude: *feature.Properties.Mag,
			MagType:   feature.Properties.MagType,
			Place:     feature.Properties.Place,
			URL:       feature.Properties.URL,
		})
	}
	return out, nil
}
