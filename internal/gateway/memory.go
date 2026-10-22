package gateway

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/memory"
	"github.com/madkoding/kitties/internal/notify"
)

// Memories is what the gateway needs from whoever keeps what the kitties know about the person.
type Memories interface {
	MemoryOf(ref string) (*chat.MemoryView, error)
	Remember(ref, scope, text string) (*memory.Note, error)
	ForgetNote(ref, id string) error
	SetProfile(ref string, demand int, auto bool) (memory.Profile, error)
	Distill(ctx context.Context, ref string) ([]memory.Note, error)
	Feedback(chatID string, index int, rating, note string) error
}

// NotifyTester sends a test notification through a connector.
type NotifyTester interface {
	Test(ctx context.Context, connector string) error
}

// WithMemory serves what the kitties know about the person, and the feedback that teaches it.
func (s *Server) WithMemory(m Memories) *Server {
	s.memories = m
	return s
}

// Digester sends the summary of the day now.
type Digester interface {
	SendNow(ctx context.Context) error
}

// WithDigest serves the button that sends the daily summary at once.
func (s *Server) WithDigest(d Digester) *Server {
	s.digester = d
	return s
}

// WithNotifier serves the test of a notification.
func (s *Server) WithNotifier(n NotifyTester) *Server {
	s.notifier = n
	return s
}

func (s *Server) memoryRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/kitties/{ref}/memory", api(s.handleMemory))
	s.mux.Handle("POST /v1/kitties/{ref}/memory", api(s.handleRemember))
	s.mux.Handle("DELETE /v1/kitties/{ref}/memory/{id}", api(s.handleForgetNote))
	s.mux.Handle("PUT /v1/kitties/{ref}/memory/profile", api(s.handleSetProfile))
	s.mux.Handle("POST /v1/kitties/{ref}/memory/distill", api(s.handleDistill))
	s.mux.Handle("POST /v1/chats/{id}/turns/{index}/feedback", api(s.handleFeedback))
	s.mux.Handle("PUT /v1/settings/notify", api(s.handleSetNotify))
	s.mux.Handle("POST /v1/settings/notify/test", api(s.handleTestNotify))
	s.mux.Handle("POST /v1/settings/notify/digest", api(s.handleSendDigest))
}

var errNoMemory = errors.New("this gateway keeps no memory of the person")

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	view, err := s.memories.MemoryOf(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, view)
}

func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	var body struct {
		Scope string `json:"scope"`
		Text  string `json:"text"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	note, err := s.memories.Remember(r.PathValue("ref"), body.Scope, body.Text)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"note": note})
}

func (s *Server) handleForgetNote(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	if err := s.memories.ForgetNote(r.PathValue("ref"), r.PathValue("id")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetProfile(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	var body struct {
		Demand int   `json:"demand"`
		Auto   *bool `json:"auto"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	auto := false
	if body.Auto != nil {
		auto = *body.Auto
	}
	p, err := s.memories.SetProfile(r.PathValue("ref"), body.Demand, auto)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"profile": p})
}

func (s *Server) handleDistill(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	notes, err := s.memories.Distill(r.Context(), r.PathValue("ref"))
	if err != nil {
		code := codeFor(err)
		if code == http.StatusInternalServerError {
			code = http.StatusBadGateway // the brain failed or answered nonsense
		}
		fail(w, code, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"notes": notes})
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		fail(w, http.StatusNotFound, errNoMemory)
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		fail(w, http.StatusBadRequest, errors.New("the turn is a number"))
		return
	}
	var body struct {
		Rating string `json:"rating"`
		Note   string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.memories.Feedback(r.PathValue("id"), index, body.Rating, body.Note); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetNotify(w http.ResponseWriter, r *http.Request) {
	var c notify.Config
	if err := decode(r, &c); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.SetNotify(c); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot send notifications"))
		return
	}
	var body struct {
		Connector string `json:"connector"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if body.Connector == "" {
		body.Connector = s.fleet.Notify().Connector
	}
	if body.Connector == "" {
		fail(w, http.StatusBadRequest, errors.New("pick a connector to send the test through"))
		return
	}
	if err := s.notifier.Test(r.Context(), body.Connector); err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSendDigest(w http.ResponseWriter, r *http.Request) {
	if s.digester == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway sends no daily summary"))
		return
	}
	if err := s.digester.SendNow(r.Context()); err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}
