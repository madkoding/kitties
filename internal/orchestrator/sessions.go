package orchestrator

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/keep"
	"github.com/madkoding/kitties/internal/kitty"
)

// WHAT A KITTY KEEPS WHEN ITS DESKTOP IS MADE ANEW.
//
// The browser's cookies, sessions and saved passwords, and the keys of its programs, are backed up
// away from the container (package keep, sealed). The backup is made when the desktop stops or
// is updated, after the browser is closed in an orderly way so it has written everything, and on
// request; it is given back to a container that is made anew, so an update of the desktop (or a
// lost workspace) does not log the kitty out of everything.

// Keeper keeps the backups of the kitties' sessions.
type Keeper interface {
	Save(kitty string, data []byte) error
	Load(kitty string) ([]byte, keep.Info, error)
	Info(kitty string) (keep.Info, bool)
	Forget(kitty string) error
}

// SetKeeper gives the manager the place the backups are kept.
func (m *Manager) SetKeeper(k Keeper) { m.keeper = k }

// sessionsTimeout bounds each step of a backup or a restore.
var sessionsTimeout = 60 * time.Second

// DesktopInfo is what is known about a kitty's desktop beyond its status.
type DesktopInfo struct {
	// Image is the image the desktop is made from, by its reference.
	Image string `json:"image"`
	// UpdateAvailable is true when a newer image than the container's is on this machine.
	UpdateAvailable bool `json:"update_available"`
	// Backup says when its sessions were last backed up, when they were.
	Backup *keep.Info `json:"backup,omitempty"`
}

func (m *Manager) desktopKitty(ref string) (*kitty.Kitty, error) {
	k, err := m.Get(ref)
	if err != nil {
		return nil, err
	}
	if !k.Desktop {
		return nil, fmt.Errorf("%s has no desktop", k.Name)
	}
	return k, nil
}

// DesktopInfo describes a kitty's desktop: its image, whether a newer one waits, its backup.
func (m *Manager) DesktopInfo(ctx context.Context, ref string) (DesktopInfo, error) {
	k, err := m.desktopKitty(ref)
	if err != nil {
		return DesktopInfo{}, err
	}
	info := DesktopInfo{Image: m.cfg.Containers.Image}
	if exists, _, err := m.containerState(ctx, k.Container); err == nil && exists {
		info.UpdateAvailable, _ = m.ctr().ImageStale(ctx, k.Container, info.Image)
	}
	if m.keeper != nil {
		if b, ok := m.keeper.Info(k.ID); ok {
			info.Backup = &b
		}
	}
	return info, nil
}

// Backup backs up a running kitty's sessions now: what its browser has written to disk (it writes
// its cookies every half minute).
func (m *Manager) Backup(ctx context.Context, ref string) (keep.Info, error) {
	k, err := m.desktopKitty(ref)
	if err != nil {
		return keep.Info{}, err
	}
	if m.keeper == nil {
		return keep.Info{}, errors.New("this gateway keeps no backups")
	}
	if _, running, err := m.containerState(ctx, k.Container); err != nil {
		return keep.Info{}, err
	} else if !running {
		return keep.Info{}, fmt.Errorf("%s is not running: start it to back it up", k.Name)
	}
	return m.backup(ctx, k)
}

func (m *Manager) backup(ctx context.Context, k *kitty.Kitty) (keep.Info, error) {
	ctx, cancel := context.WithTimeout(ctx, sessionsTimeout)
	defer cancel()
	out, err := m.ctr().Exec(ctx, k.Container, "kitty-backup")
	if err != nil {
		return keep.Info{}, errors.New("this desktop's image cannot back up yet: update the desktop")
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return keep.Info{}, errors.New("there is nothing to back up yet: its browser has not been used")
	}
	data, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		return keep.Info{}, errors.New("the backup the desktop made is not readable")
	}
	if err := m.keeper.Save(k.ID, data); err != nil {
		return keep.Info{}, err
	}
	info, _ := m.keeper.Info(k.ID)
	return info, nil
}

// saveSessions closes the browser in an orderly way, so it has written its cookies, sessions and
// passwords, and backs them up. It is the best that can be done before a stop or an update: what
// fails is not allowed to stop either.
func (m *Manager) saveSessions(ctx context.Context, k *kitty.Kitty) {
	if m.keeper == nil {
		return
	}
	quit, cancel := context.WithTimeout(ctx, sessionsTimeout)
	_, _ = m.ctr().Exec(quit, k.Container, "kitty-page", "quit")
	cancel()
	_, _ = m.backup(ctx, k)
}

// restoreSessions gives a container made anew what its kitty's last backup held.
func (m *Manager) restoreSessions(ctx context.Context, k *kitty.Kitty) {
	if m.keeper == nil {
		return
	}
	data, _, err := m.keeper.Load(k.ID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sessionsTimeout)
	defer cancel()
	_, _ = m.ctr().ExecInput(ctx, k.Container, string(data), "kitty-restore")
}

// UpdateInBackground moves a kitty's desktop to the newest image without making the caller wait
// (the image may be downloaded): the kitty is marked starting and comes up again, or fails with the
// reason on it. Its sessions are backed up first and given back to the new container; its
// workspace is untouched.
func (m *Manager) UpdateInBackground(ref string) (*kitty.Kitty, error) {
	k, err := m.desktopKitty(ref)
	if err != nil {
		return nil, err
	}
	if m.Held(k.ID) {
		return nil, fmt.Errorf("the person has the desktop of %s: give it back first", k.Name)
	}
	if err := m.setStatus(k.ID, kitty.StatusStarting, ""); err != nil {
		return nil, err
	}
	starting, err := m.Get(k.ID)
	if err != nil {
		return nil, err
	}
	go func(id string) {
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		_ = m.update(ctx, id)
	}(k.ID)
	return starting, nil
}

func (m *Manager) update(ctx context.Context, id string) error {
	k, err := m.Get(id)
	if err != nil {
		return err
	}
	image := m.cfg.Containers.Image
	// A reference that is only built here has nothing to pull: what is on this machine is the news.
	_ = m.ctr().Pull(ctx, image)
	exists, running, err := m.containerState(ctx, k.Container)
	if err != nil {
		return m.fail(k.ID, err)
	}
	if exists {
		if stale, _ := m.ctr().ImageStale(ctx, k.Container, image); !stale {
			return m.setStatus(k.ID, kitty.StatusRunning, "")
		}
		if running {
			m.saveSessions(ctx, k)
		}
		if err := m.ctr().Remove(ctx, k.Container); err != nil {
			return m.fail(k.ID, err)
		}
	}
	return m.Start(ctx, k.ID)
}
