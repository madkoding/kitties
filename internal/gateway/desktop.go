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
	s.mux.Handle("GET /v1/desktops/updates", api(s.handleDesktopUpdates))
	s.mux.Handle("POST /v1/desktops/updates/check", api(s.handleCheckDesktops))
	s.mux.Handle("POST /v1/desktops/updates", api(s.handleUpdateDesktops))
	s.mux.Handle("GET /v1/kitties/{ref}/power", api(s.handlePower))
	s.mux.Handle("POST /v1/kitties/{ref}/power", api(s.handleSetPower))
	s.mux.Handle("POST /v1/kitties/{ref}/browser/visit", api(s.handleVisitBrowser))
	s.mux.Handle("GET /v1/settings/machines", api(s.handleMachines))
	s.mux.Handle("PUT /v1/settings/machines", api(s.handleSetMachines))
}

// MemoryReader is what a kitty's machine has and uses now: RAM and swap, and what holds the most.
type MemoryReader interface {
	Memory(ctx context.Context, ref string) (kitty.Memory, error)
}

// VisitReader is what the last pass over a machine's browser tabs did (kitty-refresh).
type VisitReader interface {
	BrowserVisits(ctx context.Context, ref string) (kitty.BrowserVisits, bool)
}

// Visitor visits a machine's browser tabs again now.
type Visitor interface {
	VisitBrowserNow(ctx context.Context, ref string) (kitty.BrowserVisits, error)
}

// handleVisitBrowser visits the open tabs of a kitty's browser again now, so their sessions are
// renewed: {"browser_visits": {...}} with what it did.
func (s *Server) handleVisitBrowser(w http.ResponseWriter, r *http.Request) {
	v, ok := s.fleet.(Visitor)
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot visit the browser's tabs"))
		return
	}
	visits, err := v.VisitBrowserNow(r.Context(), r.PathValue("ref"))
	if err != nil {
		fail(w, powerCode(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"browser_visits": visits})
}

// Machiner is the RAM and swap every kitty's machine gets unless it has its own.
type Machiner interface {
	Machines() orchestrator.MachinesView
	SetMachineDefaults(ctx context.Context, d orchestrator.MachineDefaults) error
}

// memoryOf is the memory of a machine that is on, nil when it cannot be told.
func (s *Server) memoryOf(ctx context.Context, ref string, p kitty.Power) *kitty.Memory {
	r, ok := s.fleet.(MemoryReader)
	if !ok || p.State != kitty.PowerOn {
		return nil
	}
	m, err := r.Memory(ctx, ref)
	if err != nil {
		return nil
	}
	return &m
}

func (s *Server) machiner(w http.ResponseWriter) Machiner {
	m, ok := s.fleet.(Machiner)
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot size machines"))
	}
	return m
}

// handleMachines is the RAM and swap of the machines without values of their own, and what the
// engine has.
func (s *Server) handleMachines(w http.ResponseWriter, _ *http.Request) {
	m := s.machiner(w)
	if m == nil {
		return
	}
	reply(w, http.StatusOK, m.Machines())
}

// handleSetMachines changes them ({"memory_mb": 2048, "swap_mb": 1024}).
func (s *Server) handleSetMachines(w http.ResponseWriter, r *http.Request) {
	m := s.machiner(w)
	if m == nil {
		return
	}
	var d orchestrator.MachineDefaults
	if err := decode(r, &d); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := m.SetMachineDefaults(r.Context(), d); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, m.Machines())
}

// Powerer is the machine of a kitty: whether it is on, and turning it on and off.
type Powerer interface {
	Power(ref string) (kitty.Power, error)
	PowerOn(ref string) (*kitty.Kitty, error)
	PowerOff(ctx context.Context, ref string, o kitty.OffOptions) error
}

func (s *Server) powerer(w http.ResponseWriter) Powerer {
	p, ok := s.fleet.(Powerer)
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot turn machines on and off"))
	}
	return p
}

// powerCode is the status of a failed power call: a machine in use is a conflict, a kitty
// without a machine a bad request.
func powerCode(err error) int {
	var busy *kitty.InUseError
	switch {
	case errors.As(err, &busy):
		return http.StatusConflict
	case errors.Is(err, kitty.ErrNoMachine):
		return http.StatusBadRequest
	}
	return codeFor(err)
}

// handlePower tells whether a kitty's machine is on, what keeps it on and when it turns itself off.
func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	p := s.powerer(w)
	if p == nil {
		return
	}
	power, err := p.Power(r.PathValue("ref"))
	if err != nil {
		fail(w, powerCode(err), err)
		return
	}
	out := map[string]any{"power": power}
	if m := s.memoryOf(r.Context(), r.PathValue("ref"), power); m != nil {
		out["memory"] = m
	}
	if v, ok := s.fleet.(VisitReader); ok && power.State == kitty.PowerOn {
		if visits, found := v.BrowserVisits(r.Context(), r.PathValue("ref")); found {
			out["browser_visits"] = visits
		}
	}
	reply(w, http.StatusOK, out)
}

// handleSetPower turns a kitty's machine on ({"on": true}, 202: it comes up in the background) or
// off ({"on": false}; 409 when it is in use, unless "force" is true).
func (s *Server) handleSetPower(w http.ResponseWriter, r *http.Request) {
	p := s.powerer(w)
	if p == nil {
		return
	}
	var body struct {
		On    *bool `json:"on"`
		Force bool  `json:"force"`
	}
	if err := decode(r, &body); err != nil || body.On == nil {
		fail(w, http.StatusBadRequest, errors.New(`send {"on": true} or {"on": false}`))
		return
	}
	ref := r.PathValue("ref")
	status := http.StatusOK
	if *body.On {
		if _, err := p.PowerOn(ref); err != nil {
			fail(w, powerCode(err), err)
			return
		}
		status = http.StatusAccepted
	} else if err := p.PowerOff(r.Context(), ref, kitty.OffOptions{Reason: kitty.OffManual, Force: body.Force}); err != nil {
		fail(w, powerCode(err), err)
		return
	}
	power, err := p.Power(ref)
	if err != nil {
		fail(w, powerCode(err), err)
		return
	}
	k, _ := s.fleet.Get(ref)
	reply(w, status, map[string]any{"power": power, "kitty": k})
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

// DesktopFleeter looks for a new desktop image and moves the desktops of all the kitties (or some)
// to it in one go.
type DesktopFleeter interface {
	DesktopUpdates(ctx context.Context) orchestrator.DesktopFleet
	CheckDesktopUpdates() error
	UpdateDesktops(ctx context.Context, ids []string) (*orchestrator.DesktopJob, error)
}

func (s *Server) fleeter(w http.ResponseWriter) DesktopFleeter {
	f, ok := s.maintainer.(DesktopFleeter)
	if !ok {
		fail(w, http.StatusNotFound, errNoMaintenance)
	}
	return f
}

// handleDesktopUpdates is the news of the desktops' image: which desktops are of an older one,
// when it was last looked for, and how the update of them goes.
func (s *Server) handleDesktopUpdates(w http.ResponseWriter, r *http.Request) {
	if f := s.fleeter(w); f != nil {
		reply(w, http.StatusOK, f.DesktopUpdates(r.Context()))
	}
}

// handleCheckDesktops looks for a new desktop image in the background (202).
func (s *Server) handleCheckDesktops(w http.ResponseWriter, r *http.Request) {
	f := s.fleeter(w)
	if f == nil {
		return
	}
	if err := f.CheckDesktopUpdates(); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusAccepted, f.DesktopUpdates(r.Context()))
}

// handleUpdateDesktops moves desktops to the newest image in the background (202):
// {"kitties": ["id", …]}, or every desktop of an older image when it names none.
func (s *Server) handleUpdateDesktops(w http.ResponseWriter, r *http.Request) {
	f := s.fleeter(w)
	if f == nil {
		return
	}
	var body struct {
		Kitties []string `json:"kitties"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	}
	if _, err := f.UpdateDesktops(r.Context(), body.Kitties); err != nil {
		code := codeFor(err)
		if code == http.StatusInternalServerError {
			code = http.StatusBadRequest
		}
		fail(w, code, err)
		return
	}
	reply(w, http.StatusAccepted, f.DesktopUpdates(r.Context()))
}
