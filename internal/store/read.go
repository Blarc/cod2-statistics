package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type MatchSummary struct {
	ID        string `json:"id"`
	MapName   string `json:"map_name"`
	GameType  string `json:"game_type"`
	StartedAt int    `json:"started_at"`
	EndedAt   int    `json:"ended_at"`
}

type PlayerMatchStat struct {
	MatchID     string         `json:"match_id,omitempty"`
	Name        string         `json:"name"`
	Aliases     []string       `json:"aliases,omitempty"`
	Kills       int            `json:"kills"`
	Deaths      int            `json:"deaths"`
	DamageDealt int            `json:"damage_dealt"`
	DamageTaken int            `json:"damage_taken"`
	Headshots   int            `json:"headshots"`
	EventCount  int            `json:"event_count"`
	WeaponKills map[string]int `json:"weapon_kills,omitempty"`
}

type MatchDetail struct {
	MatchSummary
	Players []PlayerMatchStat `json:"players"`
}

type KillEventRow struct {
	Clock      int    `json:"clock"`
	VictimName string `json:"victim_name"`
	VictimTeam string `json:"victim_team"`
	KillerName string `json:"killer_name"`
	KillerTeam string `json:"killer_team"`
	Weapon     string `json:"weapon"`
	Mod        string `json:"mod"`
	HitLoc     string `json:"hit_loc"`
	Damage     int    `json:"damage"`
}

type DamageEventRow struct {
	Clock        int    `json:"clock"`
	VictimName   string `json:"victim_name"`
	VictimTeam   string `json:"victim_team"`
	AttackerName string `json:"attacker_name"`
	AttackerTeam string `json:"attacker_team"`
	Weapon       string `json:"weapon"`
	Mod          string `json:"mod"`
	HitLoc       string `json:"hit_loc"`
	Damage       int    `json:"damage"`
}

type PlayerSummary struct {
	Name           string   `json:"name"`
	Aliases        []string `json:"aliases,omitempty"`
	TotalKills     int      `json:"total_kills"`
	TotalDeaths    int      `json:"total_deaths"`
	TotalDamage    int      `json:"total_damage"`
	TotalHeadshots int      `json:"total_headshots"`
}

type PlayerDetail struct {
	PlayerSummary
	Matches []PlayerMatchStat `json:"matches"`
}

type PollStatus struct {
	LastPollTime *time.Time `json:"last_poll_time"`
	PollCount    int        `json:"poll_count"`
	LastError    string     `json:"last_error"`
	Ready        bool       `json:"ready"`
}

func (s *Store) ListMatches(mapName, gameType string, limit, offset int) ([]MatchSummary, int, error) {
	where, args := buildMatchWhere(mapName, gameType)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM matches "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count matches: %w", err)
	}

	rows, err := s.db.Query(
		"SELECT id, map_name, game_type, COALESCE(started_at,0), COALESCE(ended_at,0) FROM matches "+
			where+" ORDER BY started_at DESC LIMIT ? OFFSET ?",
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list matches: %w", err)
	}
	defer rows.Close()

	var out []MatchSummary
	for rows.Next() {
		var m MatchSummary
		if err := rows.Scan(&m.ID, &m.MapName, &m.GameType, &m.StartedAt, &m.EndedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *Store) GetLatestMatch() (*MatchSummary, error) {
	var m MatchSummary
	err := s.db.QueryRow(
		`SELECT id, map_name, game_type, COALESCE(started_at,0), COALESCE(ended_at,0)
		 FROM matches
		 ORDER BY started_at DESC, ended_at DESC
		 LIMIT 1`,
	).Scan(&m.ID, &m.MapName, &m.GameType, &m.StartedAt, &m.EndedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest match: %w", err)
	}
	return &m, nil
}

func buildMatchWhere(mapName, gameType string) (string, []any) {
	where := "WHERE 1=1"
	var args []any
	if mapName != "" {
		where += " AND map_name = ?"
		args = append(args, mapName)
	}
	if gameType != "" {
		where += " AND game_type = ?"
		args = append(args, gameType)
	}
	return where, args
}

func (s *Store) GetMatch(id string) (*MatchDetail, error) {
	var m MatchDetail
	err := s.db.QueryRow(
		"SELECT id, map_name, game_type, COALESCE(started_at,0), COALESCE(ended_at,0) FROM matches WHERE id = ?", id,
	).Scan(&m.ID, &m.MapName, &m.GameType, &m.StartedAt, &m.EndedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get match: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT p.normalized_name, p.aliases,
			mps.kills, mps.deaths, mps.damage_dealt, mps.damage_taken,
			mps.headshots, mps.weapon_kills, mps.event_count
		FROM match_player_stats mps
		JOIN players p ON p.id = mps.player_id
		WHERE mps.match_id = ?
		ORDER BY mps.kills DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("get match players: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ps PlayerMatchStat
		var aliasesJSON, weaponJSON string
		if err := rows.Scan(
			&ps.Name, &aliasesJSON,
			&ps.Kills, &ps.Deaths, &ps.DamageDealt, &ps.DamageTaken,
			&ps.Headshots, &weaponJSON, &ps.EventCount,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &ps.Aliases)
		_ = json.Unmarshal([]byte(weaponJSON), &ps.WeaponKills)
		m.Players = append(m.Players, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if m.Players == nil {
		m.Players = []PlayerMatchStat{}
	}
	return &m, nil
}

func (s *Store) ListKillEvents(matchID, killer, victim string, limit, offset int) ([]KillEventRow, int, error) {
	where, args := buildEventWhere(matchID, "killer_name", killer, "victim_name", victim)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM kill_events "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count kill events: %w", err)
	}

	rows, err := s.db.Query(
		"SELECT COALESCE(clock,0), COALESCE(victim_name,''), COALESCE(victim_team,''),"+
			" COALESCE(killer_name,''), COALESCE(killer_team,''),"+
			" COALESCE(weapon,''), COALESCE(damage,0), COALESCE(mod,''), COALESCE(hit_loc,'')"+
			" FROM kill_events "+where+" ORDER BY clock LIMIT ? OFFSET ?",
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list kill events: %w", err)
	}
	defer rows.Close()

	var out []KillEventRow
	for rows.Next() {
		var e KillEventRow
		if err := rows.Scan(&e.Clock, &e.VictimName, &e.VictimTeam,
			&e.KillerName, &e.KillerTeam, &e.Weapon, &e.Damage, &e.Mod, &e.HitLoc); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *Store) ListDamageEvents(matchID, attacker, victim string, limit, offset int) ([]DamageEventRow, int, error) {
	where, args := buildEventWhere(matchID, "attacker_name", attacker, "victim_name", victim)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM damage_events "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count damage events: %w", err)
	}

	rows, err := s.db.Query(
		"SELECT COALESCE(clock,0), COALESCE(victim_name,''), COALESCE(victim_team,''),"+
			" COALESCE(attacker_name,''), COALESCE(attacker_team,''),"+
			" COALESCE(weapon,''), COALESCE(damage,0), COALESCE(mod,''), COALESCE(hit_loc,'')"+
			" FROM damage_events "+where+" ORDER BY clock LIMIT ? OFFSET ?",
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list damage events: %w", err)
	}
	defer rows.Close()

	var out []DamageEventRow
	for rows.Next() {
		var e DamageEventRow
		if err := rows.Scan(&e.Clock, &e.VictimName, &e.VictimTeam,
			&e.AttackerName, &e.AttackerTeam, &e.Weapon, &e.Damage, &e.Mod, &e.HitLoc); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func buildEventWhere(matchID, col1, val1, col2, val2 string) (string, []any) {
	where := "WHERE match_id = ?"
	args := []any{matchID}
	if val1 != "" {
		where += " AND " + col1 + " = ?"
		args = append(args, val1)
	}
	if val2 != "" {
		where += " AND " + col2 + " = ?"
		args = append(args, val2)
	}
	return where, args
}

func (s *Store) ListPlayers(limit, offset int) ([]PlayerSummary, int, error) {
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM players").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count players: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT p.normalized_name, p.aliases,
			COALESCE(SUM(mps.kills),0),
			COALESCE(SUM(mps.deaths),0),
			COALESCE(SUM(mps.damage_dealt),0),
			COALESCE(SUM(mps.headshots),0)
		FROM players p
		LEFT JOIN match_player_stats mps ON mps.player_id = p.id
		GROUP BY p.id, p.normalized_name, p.aliases
		ORDER BY SUM(mps.kills) DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list players: %w", err)
	}
	defer rows.Close()

	var out []PlayerSummary
	for rows.Next() {
		var ps PlayerSummary
		var aliasesJSON string
		if err := rows.Scan(&ps.Name, &aliasesJSON,
			&ps.TotalKills, &ps.TotalDeaths, &ps.TotalDamage, &ps.TotalHeadshots); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &ps.Aliases)
		out = append(out, ps)
	}
	return out, total, rows.Err()
}

func (s *Store) GetPlayer(name string) (*PlayerDetail, error) {
	var pd PlayerDetail
	var aliasesJSON string
	err := s.db.QueryRow(`
		SELECT p.normalized_name, p.aliases,
			COALESCE(SUM(mps.kills),0),
			COALESCE(SUM(mps.deaths),0),
			COALESCE(SUM(mps.damage_dealt),0),
			COALESCE(SUM(mps.headshots),0)
		FROM players p
		LEFT JOIN match_player_stats mps ON mps.player_id = p.id
		WHERE p.normalized_name = ?
		GROUP BY p.id`, name,
	).Scan(&pd.Name, &aliasesJSON,
		&pd.TotalKills, &pd.TotalDeaths, &pd.TotalDamage, &pd.TotalHeadshots)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get player: %w", err)
	}
	_ = json.Unmarshal([]byte(aliasesJSON), &pd.Aliases)

	rows, err := s.db.Query(`
		SELECT m.id, mps.kills, mps.deaths, mps.damage_dealt, mps.damage_taken,
			mps.headshots, mps.weapon_kills, mps.event_count
		FROM match_player_stats mps
		JOIN matches m ON m.id = mps.match_id
		JOIN players p ON p.id = mps.player_id
		WHERE p.normalized_name = ?
		ORDER BY m.started_at DESC`, name)
	if err != nil {
		return nil, fmt.Errorf("get player matches: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ps PlayerMatchStat
		var weaponJSON string
		if err := rows.Scan(&ps.MatchID, &ps.Kills, &ps.Deaths,
			&ps.DamageDealt, &ps.DamageTaken, &ps.Headshots, &weaponJSON, &ps.EventCount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(weaponJSON), &ps.WeaponKills)
		pd.Matches = append(pd.Matches, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if pd.Matches == nil {
		pd.Matches = []PlayerMatchStat{}
	}
	return &pd, nil
}

func (s *Store) GetPollStatus() (PollStatus, error) {
	var status PollStatus

	var lastNS string
	err := s.db.QueryRow(`SELECT value FROM poll_state WHERE key = 'last_poll_ns'`).Scan(&lastNS)
	if err == nil {
		if ns, parseErr := strconv.ParseInt(lastNS, 10, 64); parseErr == nil {
			t := time.Unix(0, ns).UTC()
			status.LastPollTime = &t
		}
	} else if err != sql.ErrNoRows {
		return status, fmt.Errorf("get last_poll_ns: %w", err)
	}

	var pollCountStr string
	if err := s.db.QueryRow(`SELECT value FROM poll_state WHERE key = 'poll_count'`).Scan(&pollCountStr); err == nil {
		status.PollCount, _ = strconv.Atoi(pollCountStr)
	} else if err != sql.ErrNoRows {
		return status, fmt.Errorf("get poll_count: %w", err)
	}

	if err := s.db.QueryRow(`SELECT value FROM poll_state WHERE key = 'last_error'`).Scan(&status.LastError); err != nil && err != sql.ErrNoRows {
		return status, fmt.Errorf("get last_error: %w", err)
	}

	status.Ready = status.LastPollTime != nil
	return status, nil
}

type KillPair struct {
	Killer string
	Victim string
	Count  int
}

func (s *Store) GetPlayerKillPairs(normalizedName string) (asKiller []KillPair, asVictim []KillPair, err error) {
	rows, err := s.db.Query(`
		SELECT killer_name, victim_name, COUNT(*) AS cnt
		FROM kill_events
		WHERE (killer_name = ? OR victim_name = ?) AND killer_name != ''
		GROUP BY killer_name, victim_name
		ORDER BY cnt DESC
		LIMIT 20`, normalizedName, normalizedName)
	if err != nil {
		return nil, nil, fmt.Errorf("get kill pairs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p KillPair
		if err := rows.Scan(&p.Killer, &p.Victim, &p.Count); err != nil {
			return nil, nil, err
		}
		if p.Killer == normalizedName {
			asKiller = append(asKiller, p)
		} else {
			asVictim = append(asVictim, p)
		}
	}
	return asKiller, asVictim, rows.Err()
}

func (s *Store) SetLastPollNS(ns int64) error {
	_, err := s.db.Exec(
		`INSERT INTO poll_state(key,value) VALUES('last_poll_ns',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		strconv.FormatInt(ns, 10),
	)
	return err
}

func (s *Store) IncrementPollCount() error {
	_, err := s.db.Exec(`
		INSERT INTO poll_state(key,value) VALUES('poll_count','1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(CAST(value AS INTEGER)+1 AS TEXT)`)
	return err
}

func (s *Store) SetLastPollError(msg string) error {
	_, err := s.db.Exec(
		`INSERT INTO poll_state(key,value) VALUES('last_error',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		msg,
	)
	return err
}
