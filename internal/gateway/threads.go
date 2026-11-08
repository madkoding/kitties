package gateway

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/madkoding/kitties/internal/chat"
)

// THE THREADS THAT ARE NOT CHATS.
//
// A goal's thread is a scheduled task's history of runs (GET /v1/goals/{id}/runs); a hand-over
// thread between two kitties goes a while after its work ended unless the person keeps it
// (PUT /v1/chats/{id}/keep). GET /v1/chats?kind= lists one kind: chat (the ones the sidebar
// shows: plain chats and the hand-overs kept), goal, handover; all, or nothing, lists every one.

// Threads is what the chats offer for the threads that are no chat.
type Threads interface {
	KeepChat(id string, keep bool) (*chat.Session, error)
	Runs(goal string, n int) (string, []chat.Run, error)
	TidyGoals(exists func(goal string) bool) int
}

var errNoThreads = errors.New("this gateway does not tell its threads apart")

func (s *Server) threadRoutes() {
	api := s.auth
	s.mux.Handle("PUT /v1/chats/{id}/keep", api(s.handleKeepChat))
	s.mux.Handle("GET /v1/goals/{id}/runs", api(s.handleGoalRuns))
}

// chatsOfKind is the chats of one kind: chat is the ones listed in the sidebar.
func chatsOfKind(list []*chat.Session, kind string) []*chat.Session {
	if kind == "" || kind == "all" {
		return list
	}
	out := []*chat.Session{}
	for _, c := range list {
		if (kind == chat.KindChat && c.Listed()) || (kind != chat.KindChat && chat.KindOf(c) == kind) {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) handleKeepChat(w http.ResponseWriter, r *http.Request) {
	t, ok := s.chats.(Threads)
	if !ok {
		fail(w, http.StatusNotImplemented, errNoThreads)
		return
	}
	var body struct {
		Keep bool `json:"keep"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	sess, err := t.KeepChat(r.PathValue("id"), body.Keep)
	if errors.Is(err, chat.ErrNotHandOver) {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"chat": sess})
}

func (s *Server) handleGoalRuns(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	g, err := s.agenda.Goal(r.PathValue("id"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	n := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && v > 0 && v <= 500 {
		n = v
	}
	chatID, runs, err := s.goalRuns(g.ID, n)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"goal": g, "chat": chatID, "runs": runs})
}

// goalRuns is a goal's history of runs, empty when the chats do not keep one.
func (s *Server) goalRuns(goal string, n int) (string, []chat.Run, error) {
	t, ok := s.chats.(Threads)
	if !ok {
		return "", []chat.Run{}, nil
	}
	return t.Runs(goal, n)
}

// tidyGoals gives the threads of removed goals back to the chats.
func (s *Server) tidyGoals() {
	t, ok := s.chats.(Threads)
	if !ok || s.agenda == nil {
		return
	}
	t.TidyGoals(func(id string) bool { _, err := s.agenda.Goal(id); return err == nil })
}
