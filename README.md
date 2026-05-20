# Call of Duty 2 Statistics

Long-running HTTP server that ingests **Call of Duty 2** dedicated-server logs from [Loki](https://grafana.com/oss/loki/), parses match events (kills, damage, weapon switches), and stores them in SQLite. Exposes a dark-themed web UI and a JSON REST API.

## Features

- Polls Loki on a configurable interval; resumes from last position after restart
- Parses `K` (kill), `D` (damage), and `Weapon` events
- Splits log streams into matches via `InitGame` / clock-reset detection
- Identity based on **normalised username** (color codes stripped); raw aliases tracked
- Per-match stats: kills, deaths, damage dealt/taken, headshots, weapon kill counts
- Individual kill and damage rows — supports "how many times did A kill B" queries
- Idempotent writes — safe to re-poll the same log window
- **Web UI** — match list, match detail with kill/damage feed and weapon chart, player leaderboard, player profile with fun stats
- **REST API** — all data available as JSON via `/api/v1/`
- Designed for Kubernetes with a PVC-backed SQLite database

## Quick start (local)

```bash
DB_PATH=/tmp/cod2stats.db \
LOKI_URL=http://localhost:3100 \
LOKI_QUERY='{job="cod2"}' \
go run main.go
```

Open [http://localhost:8080](http://localhost:8080).

Without a real Loki instance the server still boots and serves the UI; the poller logs an error each interval and `/ready` returns 503 until the first successful poll.

## Configuration

Settings are read from environment variables. A YAML file can be supplied via `CONFIG_FILE` to override any of them (see `config.example.yaml`).

| Env var | Default | Description |
|---|---|---|
| `LOKI_URL` | — | Required — Loki base URL |
| `LOKI_QUERY` | — | Required — LogQL selector |
| `LOKI_USERNAME` / `LOKI_PASSWORD` | — | Basic auth credentials |
| `POLL_INTERVAL` | `10s` | How often to query Loki |
| `LOKI_INITIAL_LOOKBACK` | `24h` | Lookback window on first poll |
| `DB_PATH` | `/data/cod2stats.db` | SQLite file path |
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `CONFIG_FILE` | — | Optional YAML config file path |

## Web UI

| Path | Description |
|---|---|
| `/` | Dashboard — recent matches, top players, poll status |
| `/matches` | Paginated match list with map/type filter |
| `/matches/{id}` | Match detail — player leaderboard, weapon chart, kill/damage feed |
| `/players` | Global player leaderboard |
| `/players/{name}` | Player profile — K/D, HS%, nemesis, fav weapon, match history |

## REST API

All endpoints return `application/json`. Paginated list endpoints return `{"data": [...], "total": N, "limit": N, "offset": N}`.

```
GET /health
GET /ready

GET /api/v1/matches                   ?map=&game_type=&limit=20&offset=0
GET /api/v1/matches/{id}
GET /api/v1/matches/{id}/kills        ?killer=&victim=&limit=100&offset=0
GET /api/v1/matches/{id}/damage       ?attacker=&victim=&limit=100&offset=0
GET /api/v1/players                   ?limit=50&offset=0
GET /api/v1/players/{name}
GET /api/v1/poll/status
```

## Database schema

| Table | Purpose |
|---|---|
| `matches` | One row per match (map, game type, clock range) |
| `players` | One row per unique normalised name + JSON alias list |
| `match_player_stats` | Per-match aggregated stats (kills, deaths, damage, headshots, weapon kills) |
| `kill_events` | One row per kill |
| `damage_events` | One row per damage tick |
| `weapon_events` | One row per weapon pickup/switch |
| `poll_state` | Persisted poll cursor (last poll time, count, last error) |

## Kubernetes

Manifests are in `k8s/`. The deployment mounts a PVC at `/data` for the SQLite database and reads Loki credentials from a Secret referenced in the ConfigMap.

```bash
kubectl apply -f k8s/
```

## Docker

```bash
docker build -t cod2-stats .

docker run --rm -p 8080:8080 \
  -v /tmp/cod2stats.db:/data/cod2stats.db \
  -e LOKI_URL=http://loki:3100 \
  -e LOKI_QUERY='{job="cod2"}' \
  cod2-stats
```

If SQLite WAL sidecar files cause permission issues, mount the parent directory instead:

```bash
docker run --rm -p 8080:8080 \
  -v /tmp/cod2data:/data \
  -e LOKI_URL=http://loki:3100 \
  -e LOKI_QUERY='{job="cod2"}' \
  cod2-stats
```

## Development

```bash
go build ./...          # build
go run main.go          # run server (needs DB_PATH + LOKI_URL + LOKI_QUERY)
go test ./...           # all tests
go vet ./...            # lint
```

### Seeding from a log file

```bash
go run ./cmd/seed -input testdata/log.txt -db /tmp/cod2stats.db
```

Then start the server against the same database:

```bash
DB_PATH=/tmp/cod2stats.db LOKI_URL=http://localhost:3100 LOKI_QUERY='{job="cod2"}' go run main.go
```
