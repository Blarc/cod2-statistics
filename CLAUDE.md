# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build ./...               # build
go run main.go               # run server (needs LOKI_URL + LOKI_QUERY env vars or CONFIG_FILE)
go test ./...                # all tests
go test ./... -run TestName  # single test
go vet ./...                 # lint
```

## Project Purpose

Long-running HTTP server that ingests Call of Duty 2 dedicated server logs from Loki into SQLite and exposes REST endpoints for match stats and per-player-pair kill/damage queries. Designed to run on Kubernetes with a PVC-backed SQLite database.

## Architecture

```
main.go
├── internal/config    — env var + YAML config loading (12-factor)
├── internal/poller    — background goroutine: Loki → parser → matcher → store
├── internal/api       — HTTP server (net/http ServeMux, Go 1.22 path params)
│   ├── server.go      — route registration
│   └── handlers.go    — one handler per endpoint
├── internal/store     — SQLite via modernc.org/sqlite
│   ├── schema.go      — CREATE TABLE migrations (incl. poll_state)
│   ├── store.go       — SaveMatch (idempotent writes)
│   └── read.go        — query methods for REST endpoints
├── internal/loki      — Loki query_range API client (paginated)
├── internal/parser    — tokenises raw log lines into model.RawLine
├── internal/matcher   — groups RawLines into model.Match structs
├── internal/model     — shared types (RawLine, Match, KDEvent, …)
└── internal/stats     — pretty-print / JSON helpers (legacy CLI use)
```

## Configuration

| Env var | Default | Description |
|---|---|---|
| `LOKI_URL` | — | Required |
| `LOKI_QUERY` | — | Required |
| `LOKI_USERNAME` / `LOKI_PASSWORD` | — | Basic auth |
| `POLL_INTERVAL` | `10s` | Loki poll cadence |
| `LOKI_INITIAL_LOOKBACK` | `24h` | Lookback on first poll |
| `DB_PATH` | `/data/cod2stats.db` | SQLite path |
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `CONFIG_FILE` | — | Optional YAML override (see `config.example.yaml`) |

## REST Endpoints

```
GET /health                          liveness probe → 200 {}
GET /ready                           readiness (≥1 poll done) → 200 / 503

GET /api/v1/matches                  ?map=&game_type=&limit=20&offset=0
GET /api/v1/matches/{id}             match detail + per-player stats
GET /api/v1/matches/{id}/kills       ?killer=&victim=&limit=100&offset=0
GET /api/v1/matches/{id}/damage      ?attacker=&victim=&limit=100&offset=0
GET /api/v1/players                  ?limit=50&offset=0
GET /api/v1/players/{name}           player detail + per-match breakdown
GET /api/v1/poll/status              last poll time, poll count, last error
```

Paginated responses: `{"data": [...], "total": 42, "limit": 20, "offset": 0}`
Error responses: `{"error": "…", "code": 404}`

## Database Schema

Tables: `matches`, `players`, `match_player_stats`, `kill_events`, `damage_events`, `weapon_events`, `poll_state`.

`poll_state` stores three keys: `last_poll_ns` (Unix ns timestamp of last successful poll), `poll_count`, `last_error`. Persisted on PVC so pod restarts resume from where they left off.

`SaveMatch` is idempotent — uses `INSERT OR IGNORE` on all event tables keyed by `idempotency_key` (sha256 of match+clock+raw line).

## Log Format

Each line: `MM:SS EventType;field1;field2;...`

Some lines are plain-text server messages with no semicolon structure and must be skipped. Player names can contain color codes (`^0`–`^9`); strip these when displaying names.

### Event types

**InitGame** — server/map/round start; fields are `\key\value\...` pairs inline:
```
56:38 InitGame: \g_gametype\ctf\mapname\mp_decoy\...
```

**J** — player join: `J;GUID;clientId;clientName`

**Q** — player quit: `Q;GUID;clientId;clientName`

**say** / **sayteam** — chat: `say;GUID;clientId;clientName;message`

**K** — kill (victim first, then killer):
`K;victimGUID;victimId;victimTeam;victimName;killerGUID;killerId;killerTeam;killerName;weapon;damage;modType;hitLocation`

**D** — damage (same field order as K):
`D;victimGUID;victimId;victimTeam;victimName;attackerGUID;attackerId;attackerTeam;attackerName;weapon;damage;modType;hitLocation`

**W** — round win: `W;team;GUID1;playerName1;GUID2;playerName2;...`

**L** — round loss: `L;team;GUID1;playerName1;GUID2;playerName2;...`
