// Command quakefeed serves a merged earthquake catalogue over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ato113/quakefeed/internal/api"
	"github.com/ato113/quakefeed/internal/poller"
	"github.com/ato113/quakefeed/internal/source"
	"github.com/ato113/quakefeed/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	defaults := source.Defaults()

	addr := flag.String("addr", envOr("QUAKEFEED_ADDR", ":8080"), "address to listen on")
	interval := flag.Duration("interval", envDuration("QUAKEFEED_INTERVAL", time.Minute), "how often to refresh sources")
	retention := flag.Duration("retention", envDuration("QUAKEFEED_RETENTION", 7*24*time.Hour), "how long to keep events")
	timeout := flag.Duration("timeout", envDuration("QUAKEFEED_TIMEOUT", 15*time.Second), "per-source HTTP timeout")
	afad := flag.String("afad-url", envOr("QUAKEFEED_AFAD_URL", defaults.AFADURL), "AFAD endpoint, empty to disable")
	kandilli := flag.String("kandilli-url", envOr("QUAKEFEED_KANDILLI_URL", defaults.KandilliURL), "Kandilli endpoint, empty to disable")
	usgs := flag.String("usgs-url", envOr("QUAKEFEED_USGS_URL", defaults.USGSURL), "USGS endpoint, empty to disable")
	logLevel := flag.String("log-level", envOr("QUAKEFEED_LOG_LEVEL", "info"), "debug, info, warn or error")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(*logLevel)}))
	slog.SetDefault(logger)

	fetchers := source.New(source.Config{
		AFADURL:     *afad,
		KandilliURL: *kandilli,
		USGSURL:     *usgs,
		Timeout:     *timeout,
	})
	if len(fetchers) == 0 {
		return errors.New("every source is disabled; nothing to do")
	}

	st := store.New(store.DefaultMergeRule(), *retention)
	p := poller.New(fetchers, st, poller.Options{Interval: *interval, Logger: logger})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go p.Run(ctx)

	server := &http.Server{
		Addr:              *addr,
		Handler:           api.NewServer(st, api.Options{Logger: logger}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", *addr, "sources", len(fetchers))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
