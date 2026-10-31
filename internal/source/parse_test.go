package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func TestParseKandilli(t *testing.T) {
	observations, err := ParseKandilli(load(t, "kandilli.html"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Four valid rows; the prose line and the broken timestamp are skipped.
	if len(observations) != 4 {
		t.Fatalf("got %d observations, want 4", len(observations))
	}

	first := observations[0]
	if first.Source != model.SourceKandilli {
		t.Errorf("source = %q", first.Source)
	}
	// 15:04:22 Turkey time is 12:04:22 UTC.
	want := time.Date(2026, 10, 31, 12, 4, 22, 0, time.UTC)
	if !first.Time.Equal(want) {
		t.Errorf("time = %s, want %s", first.Time, want)
	}
	if first.Latitude != 38.1234 || first.Longitude != 27.5678 {
		t.Errorf("coordinates = %v, %v", first.Latitude, first.Longitude)
	}
	if first.Magnitude != 2.1 || first.MagType != "ML" {
		t.Errorf("magnitude = %v %s, want 2.1 ML", first.Magnitude, first.MagType)
	}
	if first.Place != "SOMEWHERE-MENDERES (IZMIR)" {
		t.Errorf("place = %q", first.Place)
	}
}

func TestParseKandilliPrefersMwOverML(t *testing.T) {
	observations, _ := ParseKandilli(load(t, "kandilli.html"))
	// The Sindirgi row carries both ML 3.4 and Mw 3.6.
	var found bool
	for _, observation := range observations {
		if observation.Place == "SINDIRGI (BALIKESIR)" {
			found = true
			if observation.Magnitude != 3.6 || observation.MagType != "Mw" {
				t.Errorf("magnitude = %v %s, want 3.6 Mw", observation.Magnitude, observation.MagType)
			}
		}
	}
	if !found {
		t.Fatal("Sindirgi row missing")
	}
}

func TestParseKandilliFallsBackToMd(t *testing.T) {
	observations, _ := ParseKandilli(load(t, "kandilli.html"))
	for _, observation := range observations {
		if observation.Place == "CEVREPINAR-(KAHRAMANMARAS)" {
			if observation.MagType != "Md" || observation.Magnitude != 2.0 {
				t.Errorf("magnitude = %v %s, want 2 Md", observation.Magnitude, observation.MagType)
			}
			return
		}
	}
	t.Fatal("row with only Md missing")
}

func TestParseKandilliStripsQualityMarker(t *testing.T) {
	observations, _ := ParseKandilli(load(t, "kandilli.html"))
	for _, observation := range observations {
		if observation.Place == "" {
			t.Error("empty place")
		}
		for _, marker := range []string{"Ilksel", "REVIZE01"} {
			if observation.Place == marker || len(observation.Place) > len(marker) &&
				observation.Place[len(observation.Place)-len(marker):] == marker {
				t.Errorf("place %q still carries the quality marker", observation.Place)
			}
		}
	}
}

func TestParseKandilliRejectsUnrelatedPage(t *testing.T) {
	if _, err := ParseKandilli([]byte("<html><body>service unavailable</body></html>")); err == nil {
		t.Fatal("expected an error when no rows are present")
	}
}

func TestDecodeLatin5(t *testing.T) {
	// "İZMİR" in ISO-8859-9.
	raw := []byte{0xDD, 'Z', 'M', 0xDD, 'R'}
	if got := decodeLatin5(raw); got != "İZMİR" {
		t.Errorf("decodeLatin5 = %q, want İZMİR", got)
	}
}

func TestDecodeLatin5LeavesUTF8Alone(t *testing.T) {
	if got := decodeLatin5([]byte("İZMİR")); got != "İZMİR" {
		t.Errorf("decodeLatin5 = %q", got)
	}
}

func TestParseAFAD(t *testing.T) {
	observations, err := ParseAFAD(load(t, "afad.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("got %d observations, want 2 (the malformed row is dropped)", len(observations))
	}

	first := observations[0]
	// 18:04:20 Turkey time is 15:04:20 UTC.
	want := time.Date(2026, 10, 31, 15, 4, 20, 0, time.UTC)
	if !first.Time.Equal(want) {
		t.Errorf("time = %s, want %s", first.Time, want)
	}
	if first.Place != "Korkuteli (Antalya)" {
		t.Errorf("place = %q", first.Place)
	}
	if first.MagType != "ML" {
		t.Errorf("magType = %q", first.MagType)
	}
}

func TestParseAFADAcceptsBothDateLayouts(t *testing.T) {
	observations, _ := ParseAFAD(load(t, "afad.json"))
	second := observations[1]
	want := time.Date(2026, 10, 31, 14, 58, 5, 0, time.UTC)
	if !second.Time.Equal(want) {
		t.Errorf("time = %s, want %s", second.Time, want)
	}
}

func TestParseAFADFallsBackToProvinceAndDistrict(t *testing.T) {
	observations, _ := ParseAFAD(load(t, "afad.json"))
	if got := observations[1].Place; got != "Sindirgi, Balikesir" {
		t.Errorf("place = %q, want %q", got, "Sindirgi, Balikesir")
	}
}

func TestParseAFADNormalisesMagType(t *testing.T) {
	observations, _ := ParseAFAD(load(t, "afad.json"))
	if got := observations[1].MagType; got != "MW" {
		t.Errorf("magType = %q, want MW", got)
	}
}

func TestParseAFADRejectsGarbage(t *testing.T) {
	if _, err := ParseAFAD([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseUSGS(t *testing.T) {
	observations, err := ParseUSGS(load(t, "usgs.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The quarry blast and the magnitude-less entry are both dropped.
	if len(observations) != 1 {
		t.Fatalf("got %d observations, want 1", len(observations))
	}

	only := observations[0]
	if only.Latitude != 39.46 || only.Longitude != 28.12 {
		t.Errorf("coordinates = %v, %v — are longitude and latitude swapped?", only.Latitude, only.Longitude)
	}
	if only.DepthKm != 11.2 {
		t.Errorf("depth = %v", only.DepthKm)
	}
	if only.SourceID != "us7000abcd" {
		t.Errorf("source id = %q", only.SourceID)
	}
	if only.Time.Location() != time.UTC {
		t.Errorf("time is not UTC: %s", only.Time)
	}
}

func TestParseUSGSRejectsGarbage(t *testing.T) {
	if _, err := ParseUSGS([]byte("{{{")); err == nil {
		t.Fatal("expected an error")
	}
}
