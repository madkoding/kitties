package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// WHEN THE NEW VERSION DOES NOT HOLD.
//
// An update leaves a marker beside the binary: which version it moved from and to. The new version
// must be up for Probation before it is confirmed (and the marker is removed). If it is started
// again before that, it did not hold (it crashed, or hung and was restarted), and this start goes
// back to the version that was kept as <name>.prev, remembers that the failed version was
// refused, and does not offer it again; a newer one is. A supervisor that restarts a crashed
// program (systemd, a container's restart policy) is what brings the failed start back for this to
// see it; without one, the person's own restart does.

// Probation is how long a new version must stay up to be confirmed.
var Probation = 3 * time.Minute

// pending is the marker of an update that is not confirmed yet.
type pending struct {
	From, To string
	Boots    int
	BootAt   time.Time
}

// refused is what is kept of a version that was rolled back.
type refused struct {
	Tag  string
	At   time.Time
	Told bool
}

func (u *Updater) markerPath() string  { return u.Exe + ".update" }
func (u *Updater) refusedPath() string { return u.Exe + ".refused" }

func readJSON(path string, v any) bool {
	raw, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(raw, v) == nil
}

func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// BootResult says what the start found out about the last update.
type BootResult struct {
	// RolledBack is the version that did not hold and was gone back from, to be told once.
	RolledBack string
	// Confirmed is true when a restart found the new version had held long enough.
	Confirmed bool
}

// Boot is called first thing when the gateway starts. It counts the start of a version that is not
// confirmed yet; when that version already started and did not hold, it goes back to the previous
// one and restarts into it (and does not return on systems where the restart replaces the
// process). It returns what it found, for the program to tell.
func (u *Updater) Boot() (BootResult, error) {
	var res BootResult
	var r refused
	if readJSON(u.refusedPath(), &r) && !r.Told {
		res.RolledBack = r.Tag
		r.Told = true
		_ = writeJSON(u.refusedPath(), r)
	}
	var m pending
	if !readJSON(u.markerPath(), &m) {
		return res, nil
	}
	now := u.Now()
	switch {
	case m.To != u.Current:
		// Not the version that was installed: replaced by hand since, or this is the old one.
		_ = os.Remove(u.markerPath())
	case m.Boots >= 1 && now.Sub(m.BootAt) >= Probation:
		// Restarted after it had held long enough: it is the version now.
		_ = os.Remove(u.markerPath())
		res.Confirmed = true
	case m.Boots >= 1:
		return res, u.rollback(m)
	default:
		m.Boots, m.BootAt = m.Boots+1, now
		_ = writeJSON(u.markerPath(), m)
	}
	return res, nil
}

// Confirm waits out the probation and then confirms the running version: the marker goes.
func (u *Updater) Confirm(ctx context.Context) {
	var m pending
	if !readJSON(u.markerPath(), &m) || m.To != u.Current {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(Probation):
		_ = os.Remove(u.markerPath())
	}
}

// rollback puts the previous binary back in place of the one that did not hold and restarts.
func (u *Updater) rollback(m pending) error {
	_ = os.Remove(u.markerPath())
	prev := u.Exe + ".prev"
	run, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(run, prev, "version").Output()
	if err != nil || !strings.Contains(string(out), strings.TrimPrefix(m.From, "v")) {
		return fmt.Errorf("%s did not hold, and the previous version cannot be run to go back to it: %v", m.To, err)
	}
	failed := u.Exe + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(u.Exe, failed); err != nil {
		return fmt.Errorf("going back from %s: %w", m.To, err)
	}
	if err := os.Rename(prev, u.Exe); err != nil {
		_ = os.Rename(failed, u.Exe)
		return fmt.Errorf("going back from %s: %w", m.To, err)
	}
	_ = writeJSON(u.refusedPath(), refused{Tag: m.To, At: u.Now()})
	if err := u.Restart(u.Exe, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("went back from %s to %s but could not restart: %w", m.To, m.From, err)
	}
	return nil
}

// skipped is the version this installation refused after it did not hold, or "".
func (u *Updater) skipped() string {
	var r refused
	if readJSON(u.refusedPath(), &r) {
		return r.Tag
	}
	return ""
}
