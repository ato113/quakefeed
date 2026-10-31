// Package source turns each upstream feed into normalized observations.
package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ato113/quakefeed/internal/model"
)

// Turkey has been on UTC+3 year-round since 2016, and both AFAD and Kandilli
// publish local time with no offset in the payload. A fixed zone is used
// rather than time.LoadLocation so the binary still works in a scratch or
// distroless image, which carries no tzdata.
var turkeyTime = time.FixedZone("+03", 3*60*60)

// Fetcher retrieves and parses one upstream feed.
type Fetcher interface {
	Name() model.Source
	Fetch(ctx context.Context) ([]model.Observation, error)
}

// Parser turns a raw upstream payload into observations. Keeping this
// separate from the HTTP call is what lets every format be tested against a
// saved sample instead of the live service.
type Parser func([]byte) ([]model.Observation, error)

// httpFetcher is the shared plumbing: one request, one parse.
type httpFetcher struct {
	name   model.Source
	url    string
	parse  Parser
	client *http.Client
	// maxBytes caps the response so a misbehaving upstream cannot exhaust memory.
	maxBytes int64
}

func (f *httpFetcher) Name() model.Source { return f.name }

func (f *httpFetcher) Fetch(ctx context.Context) ([]model.Observation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: building request: %w", f.name, err)
	}
	req.Header.Set("User-Agent", "quakefeed/1.0 (+https://github.com/ato113/quakefeed)")
	req.Header.Set("Accept", "application/json, text/plain, text/html;q=0.9")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: upstream returned %s", f.name, resp.Status)
	}

	limit := f.maxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("%s: reading body: %w", f.name, err)
	}

	observations, err := f.parse(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.name, err)
	}
	return observations, nil
}

// Config describes where each feed lives and how long to wait for it.
type Config struct {
	AFADURL     string
	KandilliURL string
	USGSURL     string
	Timeout     time.Duration
}

// Defaults returns the upstream endpoints as published at the time of writing.
func Defaults() Config {
	return Config{
		AFADURL:     "https://deprem.afad.gov.tr/apiv2/event/latest",
		KandilliURL: "http://www.koeri.boun.edu.tr/scripts/lst0.asp",
		USGSURL:     "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/all_day.geojson",
		Timeout:     15 * time.Second,
	}
}

// New builds the fetchers for the configured feeds. A feed with an empty URL
// is left out, which is how an operator disables a source.
func New(cfg Config) []Fetcher {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	var fetchers []Fetcher
	add := func(name model.Source, url string, parse Parser) {
		if url == "" {
			return
		}
		fetchers = append(fetchers, &httpFetcher{name: name, url: url, parse: parse, client: client})
	}

	add(model.SourceAFAD, cfg.AFADURL, ParseAFAD)
	add(model.SourceKandilli, cfg.KandilliURL, ParseKandilli)
	add(model.SourceUSGS, cfg.USGSURL, ParseUSGS)
	return fetchers
}
