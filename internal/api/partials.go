package api

import (
	"cod2-statistics/internal/store"
	"net/http"
)

func requireHTMX(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("HX-Request") != "true" {
		http.Error(w, "HTMX request required", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) handlePartialMatches(w http.ResponseWriter, r *http.Request) {
	if !requireHTMX(w, r) {
		return
	}
	q := r.URL.Query()
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)
	mapFilter := q.Get("map")
	typeFilter := q.Get("game_type")

	matches, total, err := s.store.ListMatches(mapFilter, typeFilter, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "partials/match_rows", MatchesData{
		Matches:    matches,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		MapFilter:  mapFilter,
		TypeFilter: typeFilter,
	})
}

func (s *Server) handlePartialKills(w http.ResponseWriter, r *http.Request) {
	if !requireHTMX(w, r) {
		return
	}
	id := r.PathValue("id")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	kills, total, err := s.store.ListKillEvents(id, "", "", limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if kills == nil {
		kills = []store.KillEventRow{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "partials/kill_rows", KillsData{
		MatchID: id,
		Kills:   kills,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *Server) handlePartialDamage(w http.ResponseWriter, r *http.Request) {
	if !requireHTMX(w, r) {
		return
	}
	id := r.PathValue("id")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	damage, total, err := s.store.ListDamageEvents(id, "", "", limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if damage == nil {
		damage = []store.DamageEventRow{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "partials/damage_rows", DamageData{
		MatchID: id,
		Damage:  damage,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *Server) handlePartialPlayers(w http.ResponseWriter, r *http.Request) {
	if !requireHTMX(w, r) {
		return
	}
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	players, total, err := s.store.ListPlayers(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if players == nil {
		players = []store.PlayerSummary{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "partials/player_rows", PlayersData{
		Players: players,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}
