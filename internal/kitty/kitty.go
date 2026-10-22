// Package kitty is the model of one bot: its identity, its brain and its body.
package kitty

import (
	"errors"
	"time"

	"github.com/madkoding/kitties/internal/soul"
)

// The states a kitty goes through.
const (
	StatusCreated = "created" // known, container not started
	StatusRunning = "running"
	StatusStopped = "stopped"
	StatusError   = "error"
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
	// Container is the container name, empty without a desktop.
	Container string    `json:"container,omitempty"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Workspace is the host folder mounted into the container.
	Workspace string `json:"workspace,omitempty"`
	// Limits bound what one task of this kitty may spend; a zero field takes the default.
	Limits Limits `json:"limits"`
	// Backup names the kitty a task is handed to when this one gives up; empty asks the person.
	Backup string `json:"backup,omitempty"`
	// Approvals is "ask" (or empty): an action that reaches other people waits for a yes until
	// the kitty has earned the right to do it alone; "auto": it acts without asking, except
	// for what is never done unasked.
	Approvals string `json:"approvals,omitempty"`
}

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
}

// Validate refuses a negative limit.
func (l Limits) Validate() error {
	if l.MaxTokens < 0 || l.MaxSeconds < 0 || l.MaxRounds < 0 || l.Retries < 0 {
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
	return l
}

// ContainerName is the container a kitty called name runs in.
func ContainerName(name string) string { return "kitty-" + name }
