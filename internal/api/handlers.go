package api

import (
	"cod2-statistics/internal/store"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg, "code": status})
}

type pageResp struct {
	Data   any `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func queryInt(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	status, err := s.store.GetPollStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !status.Ready {
		writeError(w, http.StatusServiceUnavailable, "no poll completed yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}

func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	data, total, err := s.store.ListMatches(q.Get("map"), q.Get("game_type"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data == nil {
		data = []store.MatchSummary{}
	}
	writeJSON(w, http.StatusOK, pageResp{Data: data, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.store.GetMatch(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleListKills(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	limit := queryInt(r, "limit", 100)
	offset := queryInt(r, "offset", 0)

	data, total, err := s.store.ListKillEvents(id, q.Get("killer"), q.Get("victim"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data == nil {
		data = []store.KillEventRow{}
	}
	writeJSON(w, http.StatusOK, pageResp{Data: data, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleListDamage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	limit := queryInt(r, "limit", 100)
	offset := queryInt(r, "offset", 0)

	data, total, err := s.store.ListDamageEvents(id, q.Get("attacker"), q.Get("victim"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data == nil {
		data = []store.DamageEventRow{}
	}
	writeJSON(w, http.StatusOK, pageResp{Data: data, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleListPlayers(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	data, total, err := s.store.ListPlayers(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data == nil {
		data = []store.PlayerSummary{}
	}
	writeJSON(w, http.StatusOK, pageResp{Data: data, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleGetPlayer(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid player name encoding")
		return
	}
	pd, err := s.store.GetPlayer(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pd == nil {
		writeError(w, http.StatusNotFound, "player not found")
		return
	}
	writeJSON(w, http.StatusOK, pd)
}

func (s *Server) handlePollStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := s.store.GetPollStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}
