package store

const schema = `
CREATE TABLE IF NOT EXISTS matches (
    id        TEXT PRIMARY KEY,
    map_name  TEXT NOT NULL,
    game_type TEXT NOT NULL,
    started_at INTEGER,
    ended_at   INTEGER
);

CREATE TABLE IF NOT EXISTS players (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    normalized_name TEXT NOT NULL UNIQUE,
    aliases         TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS match_player_stats (
    match_id     TEXT    NOT NULL REFERENCES matches(id),
    player_id    INTEGER NOT NULL REFERENCES players(id),
    kills        INTEGER NOT NULL DEFAULT 0,
    deaths       INTEGER NOT NULL DEFAULT 0,
    damage_dealt INTEGER NOT NULL DEFAULT 0,
    damage_taken INTEGER NOT NULL DEFAULT 0,
    headshots    INTEGER NOT NULL DEFAULT 0,
    weapon_kills TEXT    NOT NULL DEFAULT '{}',
    first_seen   INTEGER,
    last_seen    INTEGER,
    event_count  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (match_id, player_id)
);

CREATE TABLE IF NOT EXISTS kill_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    match_id        TEXT    NOT NULL REFERENCES matches(id),
    clock           INTEGER,
    victim_name     TEXT,
    victim_team     TEXT,
    killer_name     TEXT,
    killer_team     TEXT,
    weapon          TEXT,
    damage          INTEGER,
    mod             TEXT,
    hit_loc         TEXT,
    idempotency_key TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS damage_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    match_id        TEXT    NOT NULL REFERENCES matches(id),
    clock           INTEGER,
    victim_name     TEXT,
    victim_team     TEXT,
    attacker_name   TEXT,
    attacker_team   TEXT,
    weapon          TEXT,
    damage          INTEGER,
    mod             TEXT,
    hit_loc         TEXT,
    idempotency_key TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS weapon_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    match_id        TEXT NOT NULL REFERENCES matches(id),
    clock           INTEGER,
    player_name     TEXT,
    weapon          TEXT,
    idempotency_key TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS poll_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_kill_events_names ON kill_events(killer_name, victim_name);
`
