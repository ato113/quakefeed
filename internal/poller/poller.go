// Package poller refreshes every source on a schedule.
package poller

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/ato113/quakefeed/internal/source"
	"github.com/ato113/quakefeed/internal/store"
)

// Poller refreshes sources concurrently and writes results into the store.
type Poller struct {
	fetchers []source.Fetcher
	store    *store.Store
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
}

// Options configures a Poller. Now may be left nil outside tests.
type Options struct {
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
}

func New(fetchers []source.Fetcher, st *store.Store, opts Options) *Poller {
	interval := opts.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Poller{fetchers: fetchers, store: st, interval: interval, log: logger, now: now}
}

// RefreshOnce fetches every source once, in parallel.
//
// One source failing is normal — these are public feeds that go down — so a
// failure is recorded against that source and the others still land.
func (p *Poller) RefreshOnce(ctx context.Context) {
	var wg sync.WaitGroup
	for _, fetcher := range p.fetchers {
		wg.Add(1)
		go func(f source.Fetcher) {
			defer wg.Done()

			observations, err := f.Fetch(ctx)
			at := p.now()
			if err != nil {
				p.store.Fail(f.Name(), err, at)
				p.log.Warn("source refresh failed",
					"source", f.Name(), "error", err)
				return
			}
			p.store.Replace(f.Name(), observations, at)
			p.log.Info("source refreshed",
				"source", f.Name(), "observations", len(observations))
		}(fetcher)
	}
	wg.Wait()
}

// Run refreshes immediately, then on the interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.RefreshOnce(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A little jitter keeps every deployment of this service from
			// hitting the same public feed on the same second.
			jitter := time.Duration(rand.Int63n(int64(p.interval / 10)))
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter):
			}
			p.RefreshOnce(ctx)
		}
	}
}
