package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/tidy"
)

// Storage is what Settings shows of the disk: what the data takes, the retention of the
// screenshots, and a sweep on request (internal/tidy).
type Storage interface {
	Usage() tidy.Usage
	Clean(ctx context.Context) tidy.Result
	SetRetention(r config.Retention) error
}

// WithStorage lets Settings show what the data takes and change what is kept.
func (s *Server) WithStorage(st Storage) *Server {
	s.storage = st
	return s
}

func (s *Server) storageRoutes() {
	s.mux.Handle("GET /v1/settings/storage", s.auth(s.handleStorage))
	s.mux.Handle("PUT /v1/settings/storage", s.auth(s.handleSetStorage))
	s.mux.Handle("POST /v1/settings/storage/clean", s.auth(s.handleCleanStorage))
}

var errNoStorage = errors.New("this gateway cannot tell what its data takes")

// handleStorage is what the data takes on disk, by area, and the retention.
func (s *Server) handleStorage(w http.ResponseWriter, _ *http.Request) {
	if s.storage == nil {
		fail(w, http.StatusNotFound, errNoStorage)
		return
	}
	reply(w, http.StatusOK, s.storage.Usage())
}

// handleSetStorage changes the retention ({"shot_days": 30, "shots_mb": 2048}; 0 the default, -1
// forever or no cap) and sweeps at once, so the page shows what it freed.
func (s *Server) handleSetStorage(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		fail(w, http.StatusNotFound, errNoStorage)
		return
	}
	var ret config.Retention
	if err := decode(r, &ret); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.storage.SetRetention(ret); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.storage.Clean(r.Context())
	reply(w, http.StatusOK, s.storage.Usage())
}

// handleCleanStorage sweeps now: the screenshots past the retention and the old desktop images.
func (s *Server) handleCleanStorage(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		fail(w, http.StatusNotFound, errNoStorage)
		return
	}
	s.storage.Clean(r.Context())
	reply(w, http.StatusOK, s.storage.Usage())
}
