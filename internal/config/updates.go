package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Updates is the block about keeping Kitties up to date: when the automatic updates run, and what
// the gateway does about a new version of itself.
type Updates struct {
	// Window is the time of day ("02:00-06:00", the gateway's clock, across midnight when the end
	// is before the start) in which the automatic updates, of the desktops and of Kitties itself,
	// are done. Empty is any time. What the person asks for by hand is not held to it.
	Window string `yaml:"window"`
	// Gateway is what Kitties does when a new version of itself is out: off (does not look),
	// notify (tells the person, who can update from Settings) or auto (downloads it, checks it, and
	// restarts into it when nothing is in use).
	Gateway string `yaml:"gateway"`
	// CheckHours is how often it looks for a new version of itself.
	CheckHours int `yaml:"check_hours"`
	// Repo is the GitHub repository whose releases are looked at.
	Repo string `yaml:"repo"`
	// PublicKey is the key releases must be signed with (base64, 32 bytes): when there is one (this
	// or the one built into the binary), a release whose checksums are not signed by it is never
	// installed, and the automatic update needs one.
	PublicKey string `yaml:"public_key"`
	// API is GitHub's API address (changed for a GitHub Enterprise server, or a test).
	API string `yaml:"api"`
}

// Gateway update modes.
const (
	UpdateOff    = "off"
	UpdateNotify = "notify"
	UpdateAuto   = "auto"
)

// Window is a span of the day.
type Window struct {
	from, to int // minutes since midnight; equal means the whole day
	set      bool
}

// ParseWindow reads "HH:MM-HH:MM"; empty is every time of the day.
func ParseWindow(s string) (Window, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Window{}, nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return Window{}, fmt.Errorf("%q is not a window like 02:00-06:00", s)
	}
	from, err1 := time.Parse("15:04", strings.TrimSpace(a))
	to, err2 := time.Parse("15:04", strings.TrimSpace(b))
	if err1 != nil || err2 != nil {
		return Window{}, fmt.Errorf("%q is not a window like 02:00-06:00", s)
	}
	return Window{from: from.Hour()*60 + from.Minute(), to: to.Hour()*60 + to.Minute(), set: true}, nil
}

// Contains says whether the time of day of t is inside the window. No window holds every time.
func (w Window) Contains(t time.Time) bool {
	if !w.set || w.from == w.to {
		return true
	}
	m := t.Hour()*60 + t.Minute()
	if w.from < w.to {
		return m >= w.from && m < w.to
	}
	return m >= w.from || m < w.to
}

func (u Updates) validate() error {
	if _, err := ParseWindow(u.Window); err != nil {
		return fmt.Errorf("updates.window: %w", err)
	}
	switch u.Gateway {
	case UpdateOff, UpdateNotify, UpdateAuto:
	default:
		return fmt.Errorf("updates.gateway: %q is not off, notify or auto", u.Gateway)
	}
	if u.CheckHours < 1 || u.CheckHours > 24*30 {
		return fmt.Errorf("updates.check_hours is between 1 and 720")
	}
	if u.PublicKey != "" {
		if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(u.PublicKey)); err != nil || len(raw) != 32 {
			return errors.New("updates.public_key is not a key: 32 bytes in base64 (kitties release-key makes one)")
		}
	}
	if !strings.Contains(u.Repo, "/") || strings.ContainsAny(u.Repo, " ?#") {
		return fmt.Errorf("updates.repo: %q is not owner/name", u.Repo)
	}
	if !strings.HasPrefix(u.API, "https://") && !strings.HasPrefix(u.API, "http://127.0.0.1") && !strings.HasPrefix(u.API, "http://localhost") {
		return fmt.Errorf("updates.api: %q is not an https address", u.API)
	}
	return nil
}
