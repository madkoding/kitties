// Package kitty is the model of one bot: its identity, its brain and its body.
package kitty

import (
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
}

// ContainerName is the container a kitty called name runs in.
func ContainerName(name string) string { return "kitty-" + name }
