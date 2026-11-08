package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/madkoding/kitties/internal/selfupdate"
)

// Updating is what the gateway needs from whoever keeps Kitties up to date.
type Updating interface {
	Status() selfupdate.Status
	Check(ctx context.Context) (selfupdate.Status, error)
	Begin(ctx context.Context, done func(to string, err error)) (string, error)
	Relaunch() error
}

// WithUpdater serves the version of Kitties, the news of a newer one, and the update.
func (s *Server) WithUpdater(u Updating) *Server {
	s.updater = u
	return s
}

var errNoUpdater = errors.New("this gateway does not look for new versions")

func (s *Server) updateRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/update", api(s.handleUpdateStatus))
	s.mux.Handle("POST /v1/update/apply", api(s.handleApplyUpdate))
}

// handleUpdateStatus says which version runs and whether a newer one is out; ?refresh=1 asks
// GitHub now instead of answering with what the last look found.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		fail(w, http.StatusNotFound, errNoUpdater)
		return
	}
	st := s.updater.Status()
	if r.URL.Query().Get("refresh") != "" {
		st, _ = s.updater.Check(r.Context())
	}
	reply(w, http.StatusOK, st)
}

// restartAfter is the pause between finishing an update and restarting into it, for the page to see
// that it is done.
var restartAfter = 700 * time.Millisecond

// handleApplyUpdate starts installing the newer version and answers at once: GET /v1/update tells
// how far it has gone (progress), and once it is in place the gateway restarts into it.
func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		fail(w, http.StatusNotFound, errNoUpdater)
		return
	}
	st, err := s.updater.Check(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	switch {
	case !st.CanApply:
		fail(w, http.StatusBadRequest, errors.New(st.Why))
		return
	case !st.Available:
		fail(w, http.StatusConflict, errors.New(st.Current+" is already the newest version"))
		return
	}
	to, err := s.updater.Begin(r.Context(), func(_ string, err error) {
		if err != nil {
			return
		}
		time.Sleep(restartAfter)
		_ = s.updater.Relaunch()
	})
	switch {
	case errors.Is(err, selfupdate.ErrBusy), errors.Is(err, selfupdate.ErrInProgress):
		fail(w, http.StatusConflict, err)
		return
	case err != nil:
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"updating_to": to})
}
