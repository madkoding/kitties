package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/madkoding/kitties/internal/desktop"
)

// TAKING THE DESKTOP OVER.
//
// The person can take a kitty's display over: they watch it live and click and type in it, for
// what the kitty cannot do alone (a password, a captcha, a code sent to them). Meanwhile the
// kitty's own actions on the display wait, so the two never fight over the mouse. A hold left
// alone ends by itself, so a closed window never keeps the kitty waiting for good.

// holdIdle is how long a hold lasts without the person doing anything.
const holdIdle = 5 * time.Minute

// Hold gives the person the kitty's display (on) or gives it back to the kitty.
func (m *Manager) Hold(ref string, on bool) (bool, error) {
	k, err := m.Get(ref)
	if err != nil {
		return false, err
	}
	if on && !k.Desktop {
		return false, fmt.Errorf("%s has no desktop", k.Name)
	}
	m.holdMu.Lock()
	defer m.holdMu.Unlock()
	if m.holds == nil {
		m.holds = map[string]time.Time{}
	}
	if on {
		m.holds[k.ID] = m.now()
	} else {
		delete(m.holds, k.ID)
	}
	return on, nil
}

// Held reports whether the person has the kitty's display, by id.
func (m *Manager) Held(id string) bool {
	m.holdMu.Lock()
	defer m.holdMu.Unlock()
	at, ok := m.holds[id]
	if ok && m.now().Sub(at) > holdIdle {
		delete(m.holds, id)
		return false
	}
	return ok
}

// Input is what the person does on the display: a click, keys, text, the wheel or a drag. It
// takes the display over, or keeps it taken.
func (m *Manager) Input(ctx context.Context, ref string, a desktop.Action) error {
	argv, err := desktop.Person(a)
	if err != nil {
		return err
	}
	k, err := m.Get(ref)
	if err != nil {
		return err
	}
	if _, err := m.Hold(k.ID, true); err != nil {
		return err
	}
	_, err = m.Exec(ctx, k.ID, argv...)
	return err
}
