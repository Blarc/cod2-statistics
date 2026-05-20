package api

import (
	"cod2-statistics/internal/poller"
	"cod2-statistics/internal/store"
	"html/template"
	"io/fs"
	"net/http"
)

type Server struct {
	store     *store.Store
	poller    *poller.Poller
	mux       *http.ServeMux
	templates *template.Template
}

func New(st *store.Store, p *poller.Poller, webFS fs.FS) *Server {
	s := &Server{
		store:     st,
		poller:    p,
		mux:       http.NewServeMux(),
		templates: mustParseTemplates(webFS),
	}
	s.registerRoutes(webFS)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) registerRoutes(webFS fs.FS) {
	staticFS, _ := fs.Sub(webFS, "web/static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /ready", s.handleReady)

	s.mux.HandleFunc("GET /api/v1/matches", s.handleListMatches)
	s.mux.HandleFunc("GET /api/v1/matches/{id}/kills", s.handleListKills)
	s.mux.HandleFunc("GET /api/v1/matches/{id}/damage", s.handleListDamage)
	s.mux.HandleFunc("GET /api/v1/matches/{id}", s.handleGetMatch)
	s.mux.HandleFunc("GET /api/v1/players", s.handleListPlayers)
	s.mux.HandleFunc("GET /api/v1/players/{name}", s.handleGetPlayer)
	s.mux.HandleFunc("GET /api/v1/poll/status", s.handlePollStatus)

	s.mux.HandleFunc("GET /", s.handleWebIndex)
	s.mux.HandleFunc("GET /matches", s.handleWebMatches)
	s.mux.HandleFunc("GET /matches/{id}", s.handleWebMatch)
	s.mux.HandleFunc("GET /players", s.handleWebPlayers)
	s.mux.HandleFunc("GET /players/{name}", s.handleWebPlayer)

	s.mux.HandleFunc("GET /partials/matches", s.handlePartialMatches)
	s.mux.HandleFunc("GET /partials/matches/{id}/kills", s.handlePartialKills)
	s.mux.HandleFunc("GET /partials/matches/{id}/damage", s.handlePartialDamage)
	s.mux.HandleFunc("GET /partials/players", s.handlePartialPlayers)
}
