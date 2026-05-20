package store

import (
	"cod2-statistics/internal/model"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	if err := migrateWallTimeColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate wall-time columns: %w", err)
	}
	return &Store{db: db}, nil
}

func migrateWallTimeColumns(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE matches ADD COLUMN started_at_time_ns INTEGER`,
		`ALTER TABLE matches ADD COLUMN ended_at_time_ns INTEGER`,
		`ALTER TABLE kill_events ADD COLUMN time_ns INTEGER`,
		`ALTER TABLE damage_events ADD COLUMN time_ns INTEGER`,
		`ALTER TABLE weapon_events ADD COLUMN time_ns INTEGER`,
		`ALTER TABLE match_player_stats ADD COLUMN first_seen_time_ns INTEGER`,
		`ALTER TABLE match_player_stats ADD COLUMN last_seen_time_ns INTEGER`,
		`ALTER TABLE matches DROP COLUMN started_at`,
		`ALTER TABLE matches DROP COLUMN ended_at`,
		`ALTER TABLE kill_events DROP COLUMN clock`,
		`ALTER TABLE damage_events DROP COLUMN clock`,
		`ALTER TABLE weapon_events DROP COLUMN clock`,
		`ALTER TABLE match_player_stats DROP COLUMN first_seen`,
		`ALTER TABLE match_player_stats DROP COLUMN last_seen`,
		`CREATE INDEX IF NOT EXISTS idx_matches_started_time_ns ON matches(started_at_time_ns DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_kill_events_time_ns ON kill_events(match_id, time_ns)`,
		`CREATE INDEX IF NOT EXISTS idx_damage_events_time_ns ON damage_events(match_id, time_ns)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			errLower := strings.ToLower(err.Error())
			if strings.Contains(errLower, "duplicate column name") || strings.Contains(errLower, "no such column") {
				continue
			}
			return err
		}
	}

	// Drop old open-match state keys that used in-game clock seconds.
	if _, err := db.Exec(`DELETE FROM poll_state WHERE key IN ('open_started_at','open_last_clock')`); err != nil {
		return err
	}
	return nil
}

func toUnixNS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func toUnixNSValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().UnixNano()
}

func fromUnixNS(ns int64) time.Time {
	if ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
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
	_, err = tx.Exec(`INSERT INTO matches (id, map_name, game_type, started_at_time_ns, ended_at_time_ns)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			started_at_time_ns = CASE
				WHEN excluded.started_at_time_ns IS NULL THEN matches.started_at_time_ns
				WHEN matches.started_at_time_ns IS NULL THEN excluded.started_at_time_ns
				ELSE MIN(matches.started_at_time_ns, excluded.started_at_time_ns)
			END,
			ended_at_time_ns   = CASE
				WHEN excluded.ended_at_time_ns IS NULL THEN matches.ended_at_time_ns
				WHEN matches.ended_at_time_ns IS NULL THEN excluded.ended_at_time_ns
				ELSE MAX(matches.ended_at_time_ns, excluded.ended_at_time_ns)
			END,
			map_name   = CASE
				WHEN matches.map_name = '' AND excluded.map_name != '' THEN excluded.map_name
				ELSE matches.map_name
			END,
			game_type  = CASE
				WHEN matches.game_type = '' AND excluded.game_type != '' THEN excluded.game_type
				ELSE matches.game_type
			END`,
		m.ID, m.MapName, m.GameType, toUnixNSValue(m.StartedAt), toUnixNSValue(m.EndedAt))
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
	}

	// --- kill events ---
	for _, ev := range m.KillEvents {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO kill_events
				(match_id, time_ns, victim_name, victim_team, killer_name, killer_team,
				 weapon, damage, mod, hit_loc, idempotency_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, toUnixNSValue(ev.Time),
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
				(match_id, time_ns, victim_name, victim_team, attacker_name, attacker_team,
				 weapon, damage, mod, hit_loc, idempotency_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, toUnixNSValue(ev.Time),
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
				(match_id, time_ns, player_name, weapon, idempotency_key)
			VALUES (?, ?, ?, ?, ?)`,
			m.ID, toUnixNSValue(ev.Time), ev.PlayerNameNorm, ev.Weapon, ev.IdempotencyKey); err != nil {
			return fmt.Errorf("insert weapon event: %w", err)
		}
	}

	if err := s.rebuildMatchPlayerStats(tx, m.ID); err != nil {
		return fmt.Errorf("rebuild stats: %w", err)
	}

	return tx.Commit()
}

type playerAggregate struct {
	Kills       int
	Deaths      int
	DamageDealt int
	DamageTaken int
	Headshots   int
	WeaponKills map[string]int
	FirstSeen   time.Time
	LastSeen    time.Time
	EventCount  int
}

func (ps *playerAggregate) markSeen(eventTime time.Time) {
	if eventTime.IsZero() {
		return
	}
	if ps.FirstSeen.IsZero() || eventTime.Before(ps.FirstSeen) {
		ps.FirstSeen = eventTime
	}
	if ps.LastSeen.IsZero() || eventTime.After(ps.LastSeen) {
		ps.LastSeen = eventTime
	}
}

func ensureAggPlayer(stats map[string]*playerAggregate, name string) *playerAggregate {
	p := stats[name]
	if p == nil {
		p = &playerAggregate{WeaponKills: make(map[string]int)}
		stats[name] = p
	}
	return p
}

func (s *Store) rebuildMatchPlayerStats(tx *sql.Tx, matchID string) error {
	stats := make(map[string]*playerAggregate)

	killRows, err := tx.Query(`
		SELECT COALESCE(time_ns,0), COALESCE(victim_name,''), COALESCE(killer_name,''),
		       COALESCE(weapon,''), COALESCE(mod,''), COALESCE(hit_loc,'')
		FROM kill_events
		WHERE match_id = ?`, matchID)
	if err != nil {
		return fmt.Errorf("query kill events: %w", err)
	}
	for killRows.Next() {
		var timeNS int64
		var victim, killer, weapon, mod, hitLoc string
		if err := killRows.Scan(&timeNS, &victim, &killer, &weapon, &mod, &hitLoc); err != nil {
			killRows.Close()
			return fmt.Errorf("scan kill event: %w", err)
		}
		eventTime := fromUnixNS(timeNS)
		if victim != "" {
			v := ensureAggPlayer(stats, victim)
			v.Deaths++
			v.EventCount++
			v.markSeen(eventTime)
		}
		if killer != "" {
			k := ensureAggPlayer(stats, killer)
			k.Kills++
			k.EventCount++
			k.markSeen(eventTime)
			k.WeaponKills[weapon]++
			if mod == "MOD_HEAD_SHOT" || hitLoc == "head" {
				k.Headshots++
			}
		}
	}
	if err := killRows.Close(); err != nil {
		return fmt.Errorf("close kill events: %w", err)
	}

	damageRows, err := tx.Query(`
		SELECT COALESCE(time_ns,0), COALESCE(victim_name,''), COALESCE(attacker_name,''),
		       COALESCE(damage,0)
		FROM damage_events
		WHERE match_id = ?`, matchID)
	if err != nil {
		return fmt.Errorf("query damage events: %w", err)
	}
	for damageRows.Next() {
		var timeNS int64
		var damage int
		var victim, attacker string
		if err := damageRows.Scan(&timeNS, &victim, &attacker, &damage); err != nil {
			damageRows.Close()
			return fmt.Errorf("scan damage event: %w", err)
		}
		eventTime := fromUnixNS(timeNS)
		if victim != "" {
			v := ensureAggPlayer(stats, victim)
			v.DamageTaken += damage
			v.EventCount++
			v.markSeen(eventTime)
		}
		if attacker != "" {
			a := ensureAggPlayer(stats, attacker)
			a.DamageDealt += damage
			a.EventCount++
			a.markSeen(eventTime)
		}
	}
	if err := damageRows.Close(); err != nil {
		return fmt.Errorf("close damage events: %w", err)
	}

	weaponRows, err := tx.Query(`
		SELECT COALESCE(time_ns,0), COALESCE(player_name,'')
		FROM weapon_events
		WHERE match_id = ?`, matchID)
	if err != nil {
		return fmt.Errorf("query weapon events: %w", err)
	}
	for weaponRows.Next() {
		var timeNS int64
		var player string
		if err := weaponRows.Scan(&timeNS, &player); err != nil {
			weaponRows.Close()
			return fmt.Errorf("scan weapon event: %w", err)
		}
		eventTime := fromUnixNS(timeNS)
		if player != "" {
			p := ensureAggPlayer(stats, player)
			p.EventCount++
			p.markSeen(eventTime)
		}
	}
	if err := weaponRows.Close(); err != nil {
		return fmt.Errorf("close weapon events: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM match_player_stats WHERE match_id = ?`, matchID); err != nil {
		return fmt.Errorf("delete old stats: %w", err)
	}

	for name, ps := range stats {
		if _, err := tx.Exec(
			`INSERT INTO players (normalized_name, aliases) VALUES (?, '[]')
			 ON CONFLICT(normalized_name) DO NOTHING`,
			name,
		); err != nil {
			return fmt.Errorf("ensure player %q: %w", name, err)
		}

		var playerID int64
		if err := tx.QueryRow(`SELECT id FROM players WHERE normalized_name = ?`, name).Scan(&playerID); err != nil {
			return fmt.Errorf("get player id %q: %w", name, err)
		}

		weaponJSON, _ := json.Marshal(ps.WeaponKills)
		if _, err := tx.Exec(`
			INSERT INTO match_player_stats
				(match_id, player_id, kills, deaths, damage_dealt, damage_taken,
				 headshots, weapon_kills, first_seen_time_ns, last_seen_time_ns, event_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			matchID, playerID,
			ps.Kills, ps.Deaths, ps.DamageDealt, ps.DamageTaken,
			ps.Headshots, string(weaponJSON),
			toUnixNSValue(ps.FirstSeen), toUnixNSValue(ps.LastSeen), ps.EventCount,
		); err != nil {
			return fmt.Errorf("insert rebuilt stats for %q: %w", name, err)
		}
	}

	return nil
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
		"open_started_at_ns",
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
		"open_match_id":      open.MatchID,
		"open_map_name":      open.MapName,
		"open_game_type":     open.GameType,
		"open_started_at_ns": strconv.FormatInt(toUnixNS(open.StartedAt), 10),
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
