package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// THE DESKTOPS OF ALL THE KITTIES, MOVED TO A NEWER IMAGE TOGETHER.
//
// A new desktop image is news for every kitty whose desktop is a container on this machine (a
// desktop in the cloud has no image here). The person looks for it (a pull: what changed is
// downloaded), sees which desktops are of an older image, and moves them all, or some, in one go:
// one after the other, each one's sessions backed up and given back, its home, its browser's
// profile and its Shared folder kept on the host (home.go). A kitty in the middle of a task, or
// whose desktop the person holds, is left for later and said so; a stopped one has its old
// container removed and gets the new one when it starts, without being turned on now.

// The state of each desktop in an update of them all.
const (
	DeskWaiting  = "waiting"
	DeskUpdating = "updating"
	DeskDone     = "done"
	DeskOnStart  = "on_start" // stopped: the new desktop is made when it starts
	DeskSkipped  = "skipped"
	DeskFailed   = "failed"
)

// The phases of an update of the desktops.
const (
	DesksDownload = "download"
	DesksUpdate   = "update"
	DesksDone     = "done"
)

// errDesktopsRunning is what a second update (or look) says while one is being made.
var errDesktopsRunning = errors.New("an update of the desktops is already running")

// FleetDesktop is one kitty's desktop as the update of them all sees it.
type FleetDesktop struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// UpdateAvailable is true when its container is of an older image than the newest here.
	UpdateAvailable bool `json:"update_available"`
	// Busy says why it would be left for later now (working, or held by the person).
	Busy string `json:"busy,omitempty"`
}

// DeskJobItem is how one desktop fares in an update.
type DeskJobItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// DesktopJob is an update of several desktops: its phase, how far it has gone, each desktop.
type DesktopJob struct {
	Phase    string        `json:"phase"`
	Percent  int           `json:"percent"`
	Done     int           `json:"done"`
	Total    int           `json:"total"`
	Started  time.Time     `json:"started"`
	Finished *time.Time    `json:"finished,omitempty"`
	Kitties  []DeskJobItem `json:"kitties"`
	// Freed is what deleting the images left behind gave back, as the runtime said it.
	Freed string `json:"freed,omitempty"`
}

// DesktopFleet is what is known about the desktops' image: when it was last looked for, the
// desktops, and the update in the making (or the last one).
type DesktopFleet struct {
	Image      string         `json:"image"`
	AutoUpdate bool           `json:"auto_update"`
	CheckedAt  *time.Time     `json:"checked_at,omitempty"`
	Checking   bool           `json:"checking"`
	CheckError string         `json:"check_error,omitempty"`
	Available  int            `json:"available"`
	Kitties    []FleetDesktop `json:"kitties"`
	Job        *DesktopJob    `json:"job,omitempty"`
}

// deskState is the look for a new image and the update of the desktops, shared by the page and
// the watcher.
type deskState struct {
	mu        sync.Mutex
	checking  bool
	checkedAt time.Time
	checkErr  string
	job       *DesktopJob
}

func (s *deskState) running() bool {
	return s.checking || (s.job != nil && s.job.Finished == nil)
}

// deskTimeout bounds a look for a new image, and the update of all the desktops.
var deskTimeout = 2 * time.Hour

// localDesktops are the kitties whose desktop is a container on this machine.
func (m *Manager) localDesktops() []*kitty.Kitty {
	var out []*kitty.Kitty
	for _, k := range m.List() {
		if k.Desktop && !m.cfg.IsCloudContainer(k.Container) {
			out = append(out, k)
		}
	}
	return out
}

// whyBusy says why a desktop is not updated now ("" when it can be).
func (m *Manager) whyBusy(k *kitty.Kitty) string {
	switch {
	case m.Held(k.ID):
		return "held"
	case m.busy(k.ID):
		return "working"
	}
	return ""
}

// DesktopUpdates is the news of the desktops' image: which desktops are of an older one, and how
// the update of them goes.
func (m *Manager) DesktopUpdates(ctx context.Context) DesktopFleet {
	f := DesktopFleet{Image: m.cfg.Containers.Image, AutoUpdate: m.cfg.Containers.AutoUpdate, Kitties: []FleetDesktop{}}
	c := m.ctr()
	for _, k := range m.localDesktops() {
		d := FleetDesktop{ID: k.ID, Name: k.Name, Status: k.Status, Busy: m.whyBusy(k)}
		if c != nil {
			if exists, _, err := m.containerState(ctx, k.Container); err == nil && exists {
				d.UpdateAvailable, _ = c.ImageStale(ctx, k.Container, f.Image)
			}
		}
		if d.UpdateAvailable {
			f.Available++
		}
		f.Kitties = append(f.Kitties, d)
	}
	s := &m.desks
	s.mu.Lock()
	defer s.mu.Unlock()
	f.Checking, f.CheckError = s.checking, s.checkErr
	if !s.checkedAt.IsZero() {
		at := s.checkedAt
		f.CheckedAt = &at
	}
	if s.job != nil {
		j := *s.job
		j.Kitties = append([]DeskJobItem(nil), s.job.Kitties...)
		f.Job = &j
	}
	return f
}

// lookedForImage notes a look for a new image (a pull) and how it went.
func (m *Manager) lookedForImage(err error) {
	s := &m.desks
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt, s.checkErr = m.now(), ""
	if err != nil {
		s.checkErr = err.Error()
	}
}

// CheckDesktopUpdates looks for a new desktop image in the background (a pull, which downloads
// what changed): DesktopUpdates says when it is done.
func (m *Manager) CheckDesktopUpdates() error {
	c := m.ctr()
	if c == nil {
		return errors.New("there is no container engine to look for a new desktop with")
	}
	s := &m.desks
	s.mu.Lock()
	if s.running() {
		s.mu.Unlock()
		return errDesktopsRunning
	}
	s.checking = true
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), deskTimeout)
		defer cancel()
		err := c.Pull(ctx, m.cfg.Containers.Image)
		m.lookedForImage(err)
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()
	return nil
}

// UpdateDesktops moves the desktops of the kitties named (all those of an older image when none
// is) to the newest image in the background, one after the other: DesktopUpdates says how it goes.
func (m *Manager) UpdateDesktops(ctx context.Context, ids []string) (*DesktopJob, error) {
	if m.ctr() == nil {
		return nil, errors.New("there is no container engine to update the desktops with")
	}
	s := &m.desks
	s.mu.Lock()
	running := s.running()
	s.mu.Unlock()
	if running {
		return nil, errDesktopsRunning
	}
	want := map[string]bool{}
	for _, id := range ids {
		k, err := m.Get(id)
		if err != nil {
			return nil, err
		}
		want[k.ID] = true
	}
	job := &DesktopJob{Phase: DesksDownload, Percent: 2, Started: m.now(), Kitties: []DeskJobItem{}}
	for _, d := range m.DesktopUpdates(ctx).Kitties {
		if (len(want) == 0 && d.UpdateAvailable) || want[d.ID] {
			job.Kitties = append(job.Kitties, DeskJobItem{ID: d.ID, Name: d.Name, State: DeskWaiting})
		}
	}
	if len(job.Kitties) == 0 {
		return nil, errors.New("no desktop on this machine waits for an update")
	}
	job.Total = len(job.Kitties)
	s.mu.Lock()
	if s.running() {
		s.mu.Unlock()
		return nil, errDesktopsRunning
	}
	s.job = job
	out := *job
	out.Kitties = append([]DeskJobItem(nil), job.Kitties...)
	s.mu.Unlock()
	go m.runDesktopJob(job)
	return &out, nil
}

// setDesk changes the job under the lock.
func (m *Manager) setDesk(fn func(j *DesktopJob)) {
	m.desks.mu.Lock()
	defer m.desks.mu.Unlock()
	fn(m.desks.job)
}

func (m *Manager) runDesktopJob(job *DesktopJob) {
	ctx, cancel := context.WithTimeout(context.Background(), deskTimeout)
	defer cancel()
	// A reference that is only built here has nothing to pull: what is on this machine is the news.
	m.lookedForImage(m.ctr().Pull(ctx, m.cfg.Containers.Image))
	m.setDesk(func(j *DesktopJob) { j.Phase, j.Percent = DesksUpdate, 10 })
	renewed := 0
	for i := range job.Kitties {
		m.setDesk(func(j *DesktopJob) { j.Kitties[i].State = DeskUpdating })
		state, err := m.updateOne(ctx, job.Kitties[i].ID)
		if state == DeskDone || state == DeskOnStart {
			renewed++
		}
		m.setDesk(func(j *DesktopJob) {
			j.Kitties[i].State = state
			if err != nil {
				j.Kitties[i].Error = err.Error()
			}
			j.Done = i + 1
			j.Percent = 10 + 88*j.Done/j.Total
		})
	}
	// The image the desktops left behind is 2 to 3 GB that nothing uses any more.
	freed := ""
	if renewed > 0 {
		freed, _ = m.PruneImages(ctx)
	}
	m.setDesk(func(j *DesktopJob) {
		now := m.now()
		j.Phase, j.Percent, j.Finished, j.Freed = DesksDone, 100, &now, freed
	})
}

// updateOne moves one desktop to the newest image and says how it went.
func (m *Manager) updateOne(ctx context.Context, id string) (string, error) {
	k, err := m.Get(id)
	if err != nil {
		return DeskFailed, err
	}
	switch m.whyBusy(k) {
	case "held":
		return DeskSkipped, errors.New("you have its desktop: give it back and update it then")
	case "working":
		return DeskSkipped, errors.New("it is working on a task: update it when it finishes")
	}
	exists, running, err := m.containerState(ctx, k.Container)
	if err != nil {
		return DeskFailed, err
	}
	if !exists {
		return DeskOnStart, nil
	}
	if stale, _ := m.ctr().ImageStale(ctx, k.Container, m.cfg.Containers.Image); !stale {
		return DeskDone, nil
	}
	if !running {
		// Its sessions were backed up when it stopped; it is not turned on to be updated.
		if err := m.dropContainer(ctx, k, false); err != nil {
			return DeskFailed, err
		}
		return DeskOnStart, nil
	}
	if err := m.setStatus(k.ID, kitty.StatusStarting, ""); err != nil {
		return DeskFailed, err
	}
	if err := m.renew(ctx, k, false); err != nil {
		return DeskFailed, err
	}
	return DeskDone, nil
}
