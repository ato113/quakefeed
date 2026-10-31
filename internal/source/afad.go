package source

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

// afadEvent mirrors AFAD's apiv2 payload. Numbers arrive as strings, which is
// why every field here is one.
type afadEvent struct {
	EventID   string `json:"eventID"`
	Location  string `json:"location"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Depth     string `json:"depth"`
	Magnitude string `json:"magnitude"`
	Type      string `json:"type"`
	Date      string `json:"date"`
	Province  string `json:"province"`
	District  string `json:"district"`
}

// ParseAFAD reads AFAD's apiv2 event list.
func ParseAFAD(data []byte) ([]model.Observation, error) {
	var events []afadEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("decoding AFAD JSON: %w", err)
	}

	out := make([]model.Observation, 0, len(events))
	for _, event := range events {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(event.Latitude), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(event.Longitude), 64)
		mag, err3 := strconv.ParseFloat(strings.TrimSpace(event.Magnitude), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			// One malformed row should not discard the whole feed.
			continue
		}
		depth, _ := strconv.ParseFloat(strings.TrimSpace(event.Depth), 64)

		when, err := parseAFADTime(event.Date)
		if err != nil {
			continue
		}

		out = append(out, model.Observation{
			Source:    model.SourceAFAD,
			SourceID:  event.EventID,
			Time:      when,
			Latitude:  lat,
			Longitude: lon,
			DepthKm:   depth,
			Magnitude: mag,
			MagType:   strings.ToUpper(strings.TrimSpace(event.Type)),
			Place:     afadPlace(event),
		})
	}
	return out, nil
}

// AFAD has published its date in more than one shape over the years, and the
// value carries no zone — it is Turkey local time.
var afadLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.000",
	time.RFC3339,
}

func parseAFADTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range afadLayouts {
		if layout == time.RFC3339 {
			if when, err := time.Parse(layout, value); err == nil {
				return when.UTC(), nil
			}
			continue
		}
		if when, err := time.ParseInLocation(layout, value, turkeyTime); err == nil {
			return when.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised AFAD date %q", value)
}

func afadPlace(event afadEvent) string {
	location := strings.TrimSpace(event.Location)
	if location != "" {
		return location
	}
	parts := make([]string, 0, 2)
	for _, part := range []string{event.District, event.Province} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ", ")
}
