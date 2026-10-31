package source

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ato113/quakefeed/internal/model"
)

// Kandilli publishes a fixed-width listing inside a <pre> block:
//
//	Tarih      Saat     Enlem(N) Boylam(E) Derinlik(km)  MD   ML   Mw   Yer           Çözüm Niteliği
//	2026.10.31 15:04:22  38.1234   27.5678       7.1    -.-  2.1  -.-  SOMEWHERE (IZMIR)  İlksel
//
// Rather than trust the header or a fixed column offset — both of which have
// shifted over the years — every line that starts with a date is treated as a
// row and split on whitespace.
var kandilliRow = regexp.MustCompile(`^\s*(\d{4}\.\d{2}\.\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(.*)$`)

// Solution-quality markers that trail the place name.
var kandilliQuality = regexp.MustCompile(`(?i)^(İlksel|Ilksel|REVIZE.*|REVİZE.*|K E S .*)$`)

// ParseKandilli reads the Kandilli (KOERI) listing page.
func ParseKandilli(data []byte) ([]model.Observation, error) {
	text := decodeLatin5(data)
	if body, ok := between(text, "<pre", "</pre>"); ok {
		// Drop the rest of the opening tag.
		if idx := strings.Index(body, ">"); idx >= 0 {
			body = body[idx+1:]
		}
		text = body
	}

	var out []model.Observation
	for _, line := range strings.Split(text, "\n") {
		observation, ok := parseKandilliLine(line)
		if ok {
			out = append(out, observation)
		}
	}

	if len(out) == 0 {
		return nil, errors.New("no rows found; the page layout may have changed")
	}
	return out, nil
}

func parseKandilliLine(line string) (model.Observation, bool) {
	match := kandilliRow.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return model.Observation{}, false
	}

	when, err := time.ParseInLocation("2006.01.02 15:04:05", match[1]+" "+match[2], turkeyTime)
	if err != nil {
		return model.Observation{}, false
	}

	fields := strings.Fields(match[3])
	// latitude, longitude, depth, MD, ML, Mw, then the place.
	if len(fields) < 7 {
		return model.Observation{}, false
	}

	lat, err1 := strconv.ParseFloat(fields[0], 64)
	lon, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil {
		return model.Observation{}, false
	}
	depth, _ := strconv.ParseFloat(fields[2], 64)

	magnitude, magType, ok := pickMagnitude(fields[3], fields[4], fields[5])
	if !ok {
		return model.Observation{}, false
	}

	return model.Observation{
		Source:    model.SourceKandilli,
		Time:      when.UTC(),
		Latitude:  lat,
		Longitude: lon,
		DepthKm:   depth,
		Magnitude: magnitude,
		MagType:   magType,
		Place:     cleanPlace(fields[6:]),
	}, true
}

// pickMagnitude chooses between the three scales Kandilli reports. Missing
// values are written "-.-". ML is preferred because it is the one Kandilli
// fills in for almost every local event; Mw is the better scale for large
// quakes, so it wins when both are present.
func pickMagnitude(md, ml, mw string) (float64, string, bool) {
	for _, candidate := range []struct {
		raw     string
		magType string
	}{{mw, "Mw"}, {ml, "ML"}, {md, "Md"}} {
		value, err := strconv.ParseFloat(candidate.raw, 64)
		if err == nil && value > 0 {
			return value, candidate.magType, true
		}
	}
	return 0, "", false
}

func cleanPlace(fields []string) string {
	if len(fields) > 1 && kandilliQuality.MatchString(fields[len(fields)-1]) {
		fields = fields[:len(fields)-1]
	}
	return strings.Join(fields, " ")
}

func between(s, start, end string) (string, bool) {
	lower := strings.ToLower(s)
	i := strings.Index(lower, strings.ToLower(start))
	if i < 0 {
		return "", false
	}
	j := strings.Index(lower[i:], strings.ToLower(end))
	if j < 0 {
		return "", false
	}
	return s[i : i+j], true
}

// Kandilli serves ISO-8859-9 (Latin-5). It differs from Latin-1 in exactly
// six positions, all of them Turkish letters, so the conversion is a small
// table rather than a dependency on golang.org/x/text.
var latin5 = map[byte]rune{
	0xD0: 'Ğ', 0xDD: 'İ', 0xDE: 'Ş',
	0xF0: 'ğ', 0xFD: 'ı', 0xFE: 'ş',
}

func decodeLatin5(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		if r, ok := latin5[c]; ok {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(rune(c)) // Latin-1 maps byte to code point.
	}
	return b.String()
}
