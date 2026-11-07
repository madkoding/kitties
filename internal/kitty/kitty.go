// Package kitty is the model of one bot: its identity, its brain and its body.
package kitty

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/soul"
)

// The states a kitty goes through.
const (
	StatusCreated = "created" // known, container not started
	StatusRunning = "running"
	// StatusStarting is a desktop coming up: its image may be downloading for minutes.
	StatusStarting = "starting"
	StatusStopped  = "stopped"
	StatusError    = "error"
)

// Kitty is one bot.
type Kitty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Provider is the profile the brain uses.
	Provider string    `json:"provider"`
	Soul     soul.Soul `json:"soul"`
	// Avatar is the pixel cat the interface draws for it.
	Avatar Avatar `json:"avatar"`
	// Connectors the kitty may use; empty means every configured one.
	Connectors []string `json:"connectors,omitempty"`
	// Desktop is true when the kitty has a container of its own.
	Desktop bool `json:"desktop"`
	// BodyLater is true when the kitty wanted a container but no engine answered when it was
	// made: it gets one as soon as Docker or Podman does.
	BodyLater bool `json:"body_later,omitempty"`
	// Theme is the look of its desktop, one of Themes; empty is the image's own (Numix, dark).
	Theme string `json:"theme,omitempty"`
	// Container is the container name, empty without a desktop.
	Container string `json:"container,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	// AutoOff is the minutes its machine may stay on without use before it turns itself off: nil
	// takes the gateway's default (containers.auto_off_minutes, 15), 0 is never.
	AutoOff *int `json:"auto_off_minutes,omitempty"`
	// MemoryMB and SwapMB are the RAM and swap of its machine, in MB: nil takes the gateway's
	// defaults (containers.memory_mb and swap_mb, or what the settings chose for every kitty).
	MemoryMB *int `json:"memory_mb,omitempty"`
	SwapMB   *int `json:"swap_mb,omitempty"`
	// MemoryPending is true when new limits could not reach the running machine: they apply the
	// next time it is turned on.
	MemoryPending bool `json:"memory_pending,omitempty"`
	// OffReason is why the machine is off (OffIdle, OffManual, OffKitty), empty when it is on or
	// the reason is not known.
	OffReason string    `json:"off_reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Workspace is the host folder mounted into the container.
	Workspace string `json:"workspace,omitempty"`
	// Limits bound what one task of this kitty may spend; a zero field takes the default.
	Limits Limits `json:"limits"`
	// Backup names the kitty a task is handed to when this one gives up; empty asks the person.
	Backup string `json:"backup,omitempty"`
	// Lead marks the principal kitty, the fleet's orchestrator: the person talks to it, it hands
	// the work to the others and the reports of every kitty come back to the person through it.
	// At most one kitty of a fleet is the lead.
	Lead bool `json:"lead,omitempty"`
	// Approvals is empty (or "auto"): the kitty acts as far as its accounts allow, held back
	// only by the limits the person set; "ask": an action that reaches other people waits for a
	// yes until the kitty has earned the right to do it alone.
	Approvals string `json:"approvals,omitempty"`
}

// NoConnectors, as a kitty's only connection, means it may use none: an empty list means all
// of them (the configuration file's default), so "none" needs a name of its own.
const NoConnectors = "-"

// ValidApprovals reports whether s is a mode of approvals.
func ValidApprovals(s string) bool { return s == "" || s == "ask" || s == "auto" }

// Limits is what a kitty may spend on one task before it stops and says so.
type Limits struct {
	// MaxTokens is the input plus output tokens one task may use.
	MaxTokens int `json:"max_tokens,omitempty" yaml:"max_tokens"`
	// MaxSeconds is how long one task may run.
	MaxSeconds int `json:"max_seconds,omitempty" yaml:"max_seconds"`
	// MaxRounds is how many rounds of actions one task may take.
	MaxRounds int `json:"max_rounds,omitempty" yaml:"max_rounds"`
	// Retries is how many times a failed task is tried another way before giving up.
	Retries int `json:"retries,omitempty" yaml:"retries"`
	// DayTokens is the input plus output tokens the kitty may use in a day, over all its work;
	// when they are spent it stops until tomorrow. Zero means no daily limit.
	DayTokens int `json:"day_tokens,omitempty" yaml:"day_tokens"`
	// DayActions is how many actions that reach other people (posts, replies, mail, refunds) it
	// may take in a day; past it each one waits for the person even when the kitty acts alone.
	DayActions int `json:"day_actions,omitempty" yaml:"day_actions"`
	// Attended brings back the cautious way: a kitty stops at the end of its rounds and waits to
	// be told to go on. Left false (the default) the kitty works unattended: it goes on by itself
	// through many windows of rounds, until the task is done or a safety cap (tokens, seconds, the
	// windows) is reached, and only asks when something truly blocks it.
	Attended bool `json:"attended,omitempty" yaml:"attended"`
}

// The most a kitty may be given per task by another (the lead): enough for a long task, not a
// runaway one. The person may still set more themselves.
const (
	MaxRoundsCap  = 500
	MaxTokensCap  = 2_000_000
	MaxSecondsCap = 7200
)

// TaskLimits is a change to a kitty's limits per task, as the lead makes it: a nil field stays
// as it was, zero goes back to the default.
type TaskLimits struct {
	MaxRounds  *int `json:"max_rounds,omitempty"`
	MaxTokens  *int `json:"max_tokens,omitempty"`
	MaxSeconds *int `json:"max_seconds,omitempty"`
}

// Empty reports whether the change changes nothing.
func (t TaskLimits) Empty() bool {
	return t.MaxRounds == nil && t.MaxTokens == nil && t.MaxSeconds == nil
}

// Validate keeps each field between 0 and its cap.
func (t TaskLimits) Validate() error {
	for _, f := range []struct {
		name string
		v    *int
		cap  int
	}{{"max_rounds", t.MaxRounds, MaxRoundsCap}, {"max_tokens", t.MaxTokens, MaxTokensCap}, {"max_seconds", t.MaxSeconds, MaxSecondsCap}} {
		if f.v != nil && (*f.v < 0 || *f.v > f.cap) {
			return fmt.Errorf("%s goes from 1 to %d (0 for the default)", f.name, f.cap)
		}
	}
	return nil
}

// Apply is l with the change made.
func (t TaskLimits) Apply(l Limits) Limits {
	if t.MaxRounds != nil {
		l.MaxRounds = *t.MaxRounds
	}
	if t.MaxTokens != nil {
		l.MaxTokens = *t.MaxTokens
	}
	if t.MaxSeconds != nil {
		l.MaxSeconds = *t.MaxSeconds
	}
	return l
}

// Describe says the change in words, against the limits before it ("rounds 40 → 120").
func (t TaskLimits) Describe(before Limits) string {
	say := func(v int) string {
		if v == 0 {
			return "default"
		}
		return fmt.Sprint(v)
	}
	var parts []string
	if t.MaxRounds != nil {
		parts = append(parts, fmt.Sprintf("rounds %s → %s", say(before.MaxRounds), say(*t.MaxRounds)))
	}
	if t.MaxTokens != nil {
		parts = append(parts, fmt.Sprintf("tokens %s → %s", say(before.MaxTokens), say(*t.MaxTokens)))
	}
	if t.MaxSeconds != nil {
		parts = append(parts, fmt.Sprintf("seconds %s → %s", say(before.MaxSeconds), say(*t.MaxSeconds)))
	}
	return strings.Join(parts, ", ")
}

// Validate refuses a negative limit.
func (l Limits) Validate() error {
	if l.MaxTokens < 0 || l.MaxSeconds < 0 || l.MaxRounds < 0 || l.Retries < 0 || l.DayTokens < 0 || l.DayActions < 0 {
		return errors.New("limits cannot be negative")
	}
	if l.Retries > 10 {
		return errors.New("limits.retries: at most 10")
	}
	return nil
}

// Or fills the zero fields of l from d.
func (l Limits) Or(d Limits) Limits {
	if l.MaxTokens == 0 {
		l.MaxTokens = d.MaxTokens
	}
	if l.MaxSeconds == 0 {
		l.MaxSeconds = d.MaxSeconds
	}
	if l.MaxRounds == 0 {
		l.MaxRounds = d.MaxRounds
	}
	if l.Retries == 0 {
		l.Retries = d.Retries
	}
	if l.DayTokens == 0 {
		l.DayTokens = d.DayTokens
	}
	if l.DayActions == 0 {
		l.DayActions = d.DayActions
	}
	l.Attended = l.Attended || d.Attended
	return l
}

// ContainerName is the container a kitty called name runs in.
func ContainerName(name string) string { return "kitty-" + name }

// Themes are the looks a kitty's desktop can wear: Openbox-format themes the kitty-desktop image carries.
var Themes = []string{"Numix", "Greybird-dark"}

// ValidTheme reports whether a theme is one of Themes, or empty (the image's own).
func ValidTheme(name string) bool {
	if name == "" {
		return true
	}
	for _, t := range Themes {
		if t == name {
			return true
		}
	}
	return false
}
