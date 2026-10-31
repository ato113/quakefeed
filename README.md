# quakefeed

*[Türkçe](README.tr.md) · English*

**One earthquake API from three networks that disagree.**

[![CI](https://github.com/ato113/quakefeed/actions/workflows/ci.yml/badge.svg)](https://github.com/ato113/quakefeed/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.23%2B-00ADD8)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

If you want recent earthquakes in Türkiye you have three choices, and all of
them are awkward. AFAD returns JSON with every number as a string. Kandilli
publishes a fixed-width table inside a `<pre>` block, encoded in ISO-8859-9.
USGS gives you clean GeoJSON but places Turkish epicentres less precisely than
the local networks do. All three report times differently, use different
magnitude scales, and describe the same earthquake with different numbers.

`quakefeed` polls all three, merges reports of the same quake into one event,
and serves the result as plain JSON. No API key, no dependencies, one binary.

```console
$ curl 'localhost:8080/v1/events?min_mag=3&since=24h'
{
  "count": 2,
  "total": 2,
  "events": [
    {
      "id": "1a6a5ace8b727913",
      "time": "2026-10-31T14:58:05Z",
      "latitude": 39.451,
      "longitude": 28.102,
      "depth_km": 10.1,
      "magnitude": 3.6,
      "mag_type": "ML",
      "place": "Sındırgı (Balıkesir)",
      "sources": ["afad", "kandilli", "usgs"]
    }
  ]
}
```

That one event is three separate reports — AFAD's, Kandilli's and USGS's —
recognised as the same earthquake and merged.

## Run it

### With Docker (nothing else to install)

```bash
git clone https://github.com/ato113/quakefeed
cd quakefeed
docker compose up
```

### With Go

You need Go 1.23 or newer — `go version` tells you what you have.

```bash
git clone https://github.com/ato113/quakefeed
cd quakefeed
go run ./cmd/quakefeed
```

To get a binary you can copy anywhere:

```bash
go build -o quakefeed ./cmd/quakefeed
./quakefeed
```

### Check it is working

Either way it listens on `:8080` and refreshes every minute. The first refresh
happens at startup, so give it a couple of seconds, then:

```bash
curl localhost:8080/healthz          # status, event count, uptime
curl localhost:8080/v1/sources       # whether each source is answering
curl 'localhost:8080/v1/events?min_mag=3&since=24h'
```

If `/v1/sources` shows a source as unhealthy, its `last_error` says why. The
service keeps working on whatever sources do answer.

## The interesting part: merging

The same quake never looks the same twice. For the event above, the three
networks reported:

| Source | Time (UTC) | Epicentre | Magnitude |
|--------|-----------|-----------|-----------|
| AFAD | 14:58:05 | 39.451, 28.102 | 3.6 ML |
| Kandilli | 14:58:12 | 39.460, 28.120 | 3.4 ML |
| USGS | 14:58:18 | 39.440, 28.090 | 3.7 mb |

Two reports are treated as the same earthquake when they are **within 45
seconds and 80 km** of each other. Those numbers are deliberate: tighter and
the same quake splits into three events; looser and a genuine aftershock gets
swallowed into its mainshock.

Three rules keep the merge honest:

- **One source never merges with itself.** Two AFAD reports ten seconds apart
  are a swarm, not a duplicate — so a source's own reports always stay distinct.
- **The local networks lead.** AFAD and Kandilli site Turkish epicentres more
  precisely than a global network, so the headline figures come from them.
  USGS still contributes its own report.
- **Nothing is averaged away.** `?detailed=true` returns every source's own
  numbers, untouched. Magnitudes on different scales (ML, Mw, Md, mb) are not
  interchangeable, so the scale always travels with the number.

Event IDs are derived from the quake itself — time rounded to the minute,
coordinates to roughly a kilometre — not from any one source's ID. An event
keeps its ID across refreshes, when a source revises its numbers slightly, and
no matter which sources saw it.

## When a source goes down

These are public feeds and they do go down. A failed refresh never removes
data: the source keeps its last good observations and the catalogue is rebuilt
from whatever is still held.

`/healthz` reports `degraded` with HTTP 200 while some sources work, and
`unhealthy` with 503 only when every source is failing — so a load balancer
doesn't pull a working service out of rotation because one upstream had a bad
afternoon. `/v1/sources` shows each source's last success, last error and
observation count.

## API

### `GET /v1/events`

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `min_mag` | `3.5` | Minimum magnitude |
| `since` / `until` | `24h`, `7d`, `2026-10-31T00:00:00Z` | Time window; relative or RFC 3339 |
| `bbox` | `36,26,42,45` | `min_lat,min_lon,max_lat,max_lon` |
| `source` | `afad` | Only events this source reported |
| `limit` / `offset` | `50` / `100` | Paging; `limit` caps at 1000, default 100 |
| `detailed` | `true` | Include each source's own report |

`total` is the count before paging, so you can tell how much is left.

### `GET /v1/sources`

Per-source health: last success, last attempt, last error, observation count.

### `GET /healthz`

`ok`, `degraded` or `unhealthy`, with the event count and uptime.

## Configuration

Every flag has an environment variable; flags win.

| Flag | Environment variable | Default |
|------|---------------------|---------|
| `-addr` | `QUAKEFEED_ADDR` | `:8080` |
| `-interval` | `QUAKEFEED_INTERVAL` | `1m` |
| `-retention` | `QUAKEFEED_RETENTION` | `168h` |
| `-timeout` | `QUAKEFEED_TIMEOUT` | `15s` |
| `-afad-url` | `QUAKEFEED_AFAD_URL` | AFAD apiv2 latest |
| `-kandilli-url` | `QUAKEFEED_KANDILLI_URL` | KOERI listing page |
| `-usgs-url` | `QUAKEFEED_USGS_URL` | USGS all-day summary |
| `-log-level` | `QUAKEFEED_LOG_LEVEL` | `info` |

Setting a source's URL to an empty string disables it.

## Notes on the sources

- **Times.** AFAD and Kandilli publish Turkey local time with no offset in the
  payload. Türkiye has been on UTC+3 year-round since 2016, so the conversion
  uses a fixed offset rather than `tzdata` — which also means the container
  needs no zoneinfo. Everything the API returns is UTC.
- **Encoding.** Kandilli serves ISO-8859-9. It differs from Latin-1 in exactly
  six positions, all Turkish letters, so the conversion is a small table rather
  than a dependency.
- **Magnitudes.** Kandilli publishes Md, ML and Mw columns, often with only one
  filled in. Mw is preferred when present, then ML, then Md.
- **Parsers are written against saved samples, not live traffic.** The fixtures
  in `testdata/` document the format each parser expects. If an upstream changes
  its layout, the parser in `internal/source/` is the only thing to fix — and
  the fixture should be updated with it. **Run it against the live feeds once
  before you rely on it.**

> Not for life-safety decisions. For official information, use AFAD and
> Kandilli directly.

## Development

```bash
go test ./...            # 60 tests
go test -race -cover ./...
go vet ./...
```

The pipeline is tested end to end in `internal/e2e`: three fake upstream
servers, the real fetchers, the real poller and the real HTTP handlers, with
only the public internet left out.

Parsing is deliberately separated from fetching, which is what lets every
format be tested against a saved sample instead of the live service.

## Contributing

Adding a source means writing one parser in `internal/source/` that returns
`[]model.Observation`, plus a fixture in `testdata/`. Everything downstream —
merging, filtering, serving — is shared.

AI-assisted contributions are fine as long as you say so in the pull request
and can explain what you're submitting.

## License

MIT — see [LICENSE](LICENSE).
