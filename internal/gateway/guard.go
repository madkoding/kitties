package gateway

import (
	"errors"
	"net/http"

	"github.com/madkoding/kitties/internal/guard"
)

// Guard is the guardrails of the exposed kitties (internal/guard), as Settings shows and changes
// them.
type Guard interface {
	Status() guard.Status
	Apply(s guard.Settings) error
	ClearLog() error
}

// exposer marks a chat as exposed (chat.Service.Expose).
type exposer interface {
	Expose(id, who string, on bool) error
}

// WithGuard serves the guardrails: their settings and their record.
func (s *Server) WithGuard(g Guard) *Server {
	s.guard = g
	return s
}

var errNoGuard = errors.New("this gateway has no guardrails")

func (s *Server) guardRoutes() {
	s.mux.Handle("GET /v1/guard", s.auth(s.handleGuard))
	s.mux.Handle("PUT /v1/guard", s.auth(s.handleSetGuard))
	s.mux.Handle("DELETE /v1/guard/log", s.auth(s.handleClearGuardLog))
	s.mux.Handle("PUT /v1/chats/{id}/exposed", s.auth(s.handleExpose))
}

// handleGuard is the settings with the latest of what the guard took out or refused.
func (s *Server) handleGuard(w http.ResponseWriter, _ *http.Request) {
	if s.guard == nil {
		fail(w, http.StatusNotFound, errNoGuard)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, http.StatusOK, s.guard.Status())
}

func (s *Server) handleSetGuard(w http.ResponseWriter, r *http.Request) {
	if s.guard == nil {
		fail(w, http.StatusNotFound, errNoGuard)
		return
	}
	var in guard.Settings
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.guard.Apply(in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, s.guard.Status())
}

func (s *Server) handleClearGuardLog(w http.ResponseWriter, _ *http.Request) {
	if s.guard == nil {
		fail(w, http.StatusNotFound, errNoGuard)
		return
	}
	if err := s.guard.ClearLog(); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, s.guard.Status())
}

// handleExpose marks a chat as answering people who are not the person (a public chat), or takes
// the mark off: {"exposed": true, "audience": "who writes there"}.
func (s *Server) handleExpose(w http.ResponseWriter, r *http.Request) {
	e, ok := s.chats.(exposer)
	if !ok {
		fail(w, http.StatusNotFound, errNoGuard)
		return
	}
	var in struct {
		Exposed  bool   `json:"exposed"`
		Audience string `json:"audience"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	id := r.PathValue("id")
	if err := e.Expose(id, in.Audience, in.Exposed); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	sess, err := s.chats.Get(id)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, sess)
}
