package store

import (
	"cod2-statistics/internal/model"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite DB at path and runs schema migrations.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer

	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragma: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// SaveMatch persists a match and all its events to the database.
// If the match already exists (idempotency via PRIMARY KEY), the match row
// itself is skipped but individual events are still attempted via
// INSERT OR IGNORE, so the call is safe to repeat.
func (s *Store) SaveMatch(m *model.Match) error {

	// Matches with no players are skipped.
	if len(m.Players) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// --- match row ---
	_, err = tx.Exec(`INSERT INTO matches (id, map_name, game_type, started_at, ended_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			started_at = MIN(COALESCE(matches.started_at, excluded.started_at), excluded.started_at),
			ended_at   = MAX(COALESCE(matches.ended_at, excluded.ended_at), excluded.ended_at),
			map_name   = CASE
				WHEN matches.map_name = '' AND excluded.map_name != '' THEN excluded.map_name
				ELSE matches.map_name
			END,
			game_type  = CASE
				WHEN matches.game_type = '' AND excluded.game_type != '' THEN excluded.game_type
				ELSE matches.game_type
			END`,
		m.ID, m.MapName, m.GameType, m.StartedAt, m.EndedAt)
	if err != nil {
		return fmt.Errorf("insert match: %w", err)
	}

	// --- players + stats ---
	for _, ps := range m.Players {
		aliasJSON, _ := json.Marshal(ps.Aliases)

		// Upsert player (merge aliases).
		if _, err := tx.Exec(`
			INSERT INTO players (normalized_name, aliases) VALUES (?, ?)
			ON CONFLICT(normalized_name) DO UPDATE SET
				aliases = json(excluded.aliases)
			WHERE json_array_length(excluded.aliases) > json_array_length(players.aliases)`,
			ps.NormalizedName, string(aliasJSON)); err != nil {
			return fmt.Errorf("upsert player %q: %w", ps.NormalizedName, err)
		}

		var playerID int64
		if err := tx.QueryRow(`SELECT id FROM players WHERE normalized_name = ?`,
			ps.NormalizedName).Scan(&playerID); err != nil {
			return fmt.Errorf("get player id %q: %w", ps.NormalizedName, err)
		}

		weaponJSON, _ := json.Marshal(ps.WeaponKills)
		if _, err := tx.Exec(`
			INSERT INTO match_player_stats
				(match_id, player_id, kills, deaths, damage_dealt, damage_taken,
				 headshots, weapon_kills, first_seen, last_seen, event_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(match_id, player_id) DO UPDATE SET
				kills        = kills + excluded.kills,
				deaths       = deaths + excluded.deaths,
				damage_dealt = damage_dealt + excluded.damage_dealt,
				damage_taken = damage_taken + excluded.damage_taken,
				headshots    = headshots + excluded.headshots,
				last_seen    = MAX(last_seen, excluded.last_seen),
				event_count  = event_count + excluded.event_count`,
			m.ID, playerID,
			ps.Kills, ps.Deaths, ps.DamageDealt, ps.DamageTaken,
			ps.Headshots, string(weaponJSON),
			ps.FirstSeen, ps.LastSeen, ps.EventCount); err != nil {
			return fmt.Errorf("upsert stats: %w", err)
		}
	}

	// --- kill events ---
	for _, ev := range m.KillEvents {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO kill_events
				(match_id, clock, victim_name, victim_team, killer_name, killer_team,
				 weapon, damage, mod, hit_loc, idempotency_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, ev.ClockSec,
			ev.VictimNameNorm, ev.VictimTeam,
			ev.KillerNameNorm, ev.KillerTeam,
			ev.Weapon, ev.Damage, ev.Mod, ev.HitLoc,
			ev.IdempotencyKey); err != nil {
			return fmt.Errorf("insert kill event: %w", err)
		}
	}

	// --- damage events ---
	for _, ev := range m.DamageEvents {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO damage_events
				(match_id, clock, victim_name, victim_team, attacker_name, attacker_team,
				 weapon, damage, mod, hit_loc, idempotency_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, ev.ClockSec,
			ev.VictimNameNorm, ev.VictimTeam,
			ev.KillerNameNorm, ev.KillerTeam,
			ev.Weapon, ev.Damage, ev.Mod, ev.HitLoc,
			ev.IdempotencyKey); err != nil {
			return fmt.Errorf("insert damage event: %w", err)
		}
	}

	// --- weapon events ---
	for _, ev := range m.WeaponEvents {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO weapon_events
				(match_id, clock, player_name, weapon, idempotency_key)
			VALUES (?, ?, ?, ?, ?)`,
			m.ID, ev.ClockSec, ev.PlayerNameNorm, ev.Weapon, ev.IdempotencyKey); err != nil {
			return fmt.Errorf("insert weapon event: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) SetOpenMatch(open *OpenMatch) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	keys := []string{
		"open_match_id",
		"open_map_name",
		"open_game_type",
		"open_started_at",
		"open_last_clock",
	}

	if open == nil || open.MatchID == "" {
		for _, k := range keys {
			if _, err := tx.Exec(`DELETE FROM poll_state WHERE key = ?`, k); err != nil {
				return fmt.Errorf("clear open match state %s: %w", k, err)
			}
		}
		return tx.Commit()
	}

	values := map[string]string{
		"open_match_id":   open.MatchID,
		"open_map_name":   open.MapName,
		"open_game_type":  open.GameType,
		"open_started_at": strconv.Itoa(open.StartedAt),
		"open_last_clock": strconv.Itoa(open.LastClock),
	}
	for _, k := range keys {
		if _, err := tx.Exec(
			`INSERT INTO poll_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, values[k],
		); err != nil {
			return fmt.Errorf("set open match state %s: %w", k, err)
		}
	}

	return tx.Commit()
}
