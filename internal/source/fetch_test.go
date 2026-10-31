package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

func TestFetchParsesUpstreamResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "quakefeed") {
			t.Errorf("User-Agent = %q", got)
		}
		_, _ = w.Write(load(t, "usgs.json"))
	}))
	defer server.Close()

	fetchers := New(Config{USGSURL: server.URL, Timeout: 5 * time.Second})
	if len(fetchers) != 1 {
		t.Fatalf("got %d fetchers, want 1", len(fetchers))
	}

	observations, err := fetchers[0].Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("got %d observations, want 1", len(observations))
	}
}

func TestFetchReportsUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	fetchers := New(Config{USGSURL: server.URL})
	_, err := fetchers[0].Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want one mentioning 502", err)
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fetchers := New(Config{USGSURL: server.URL})
	if _, err := fetchers[0].Fetch(ctx); err == nil {
		t.Fatal("expected the cancelled context to surface as an error")
	}
}

func TestEmptyURLDisablesSource(t *testing.T) {
	fetchers := New(Config{AFADURL: "", KandilliURL: "", USGSURL: "http://example.invalid"})
	if len(fetchers) != 1 || fetchers[0].Name() != model.SourceUSGS {
		t.Fatalf("got %d fetchers, want only usgs", len(fetchers))
	}
}

func TestDefaultsCoverEverySource(t *testing.T) {
	fetchers := New(Defaults())
	if len(fetchers) != 3 {
		t.Fatalf("got %d fetchers, want 3", len(fetchers))
	}
}
