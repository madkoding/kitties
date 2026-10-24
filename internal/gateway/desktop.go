package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/madkoding/kitties/internal/keep"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/orchestrator"
)

// Maintainer is what the gateway needs to look after a kitty's desktop beyond running it: whether
// a newer image waits, the backup of its sessions, and moving it to the newest image.
type Maintainer interface {
	DesktopInfo(ctx context.Context, ref string) (orchestrator.DesktopInfo, error)
	Backup(ctx context.Context, ref string) (keep.Info, error)
	UpdateInBackground(ref string) (*kitty.Kitty, error)
}

// WithMaintenance serves the desktop's update and the backup of its sessions.
func (s *Server) WithMaintenance(m Maintainer) *Server {
	s.maintainer = m
	return s
}

var errNoMaintenance = errors.New("this gateway cannot look after desktops")

func (s *Server) desktopRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/kitties/{ref}/desktop", api(s.handleDesktopInfo))
	s.mux.Handle("POST /v1/kitties/{ref}/backup", api(s.handleBackup))
	s.mux.Handle("POST /v1/kitties/{ref}/update", api(s.handleUpdate))
}

func (s *Server) handleDesktopInfo(w http.ResponseWriter, r *http.Request) {
	if s.maintainer == nil {
		fail(w, http.StatusNotFound, errNoMaintenance)
		return
	}
	info, err := s.maintainer.DesktopInfo(r.Context(), r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, info)
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.maintainer == nil {
		fail(w, http.StatusNotFound, errNoMaintenance)
		return
	}
	info, err := s.maintainer.Backup(r.Context(), r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"backup": info})
}

// busyChats is what the chats say about a kitty that is in the middle of a task.
type busyChats interface{ KittyBusy(kittyID string) bool }

// handleUpdate moves a desktop to the newest image in the background: the kitty is starting
// until it is back. A kitty in the middle of a task is not interrupted.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if s.maintainer == nil {
		fail(w, http.StatusNotFound, errNoMaintenance)
		return
	}
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if b, ok := s.chats.(busyChats); ok && b.KittyBusy(k.ID) {
		fail(w, http.StatusConflict, errors.New(k.Name+" is working on a task: wait until it finishes"))
		return
	}
	started, err := s.maintainer.UpdateInBackground(k.ID)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"kitty": started})
}
