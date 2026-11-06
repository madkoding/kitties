package kitty

import (
	"errors"
	"fmt"
)

// THE POWER OF A KITTY'S MACHINE.
//
// A kitty with a desktop has a machine: its container. It can be turned on and off by the person
// (the interface, the API), by the kitty itself (the machine tool), and it turns itself off after
// a time without use, so a desktop nobody needs does not hold memory and CPU for nothing.

// The states of the machine, as the person and the kitty see them.
const (
	PowerOn       = "on"
	PowerStarting = "starting"
	PowerOff      = "off"
	PowerError    = "error"
)

// Why a machine is off, when the gateway knows.
const (
	OffIdle   = "idle"   // turned itself off after the idle time
	OffManual = "manual" // the person turned it off
	OffKitty  = "kitty"  // the kitty turned it off
)

// The ways a machine is in use, which keep it from turning itself off (and from being turned
// off by mistake).
const (
	UseTask    = "task"    // the kitty is in the middle of a task
	UseControl = "control" // the person has taken the display over
	UseWatch   = "watch"   // the person is watching the display live
	UseCommand = "command" // a command is running in the machine
)

// DefaultAutoOffMinutes is how long a machine stays on without use when nothing says otherwise.
const DefaultAutoOffMinutes = 15

// MaxAutoOffMinutes is the longest idle time that can be set (a week); more is "never" (0).
const MaxAutoOffMinutes = 7 * 24 * 60

// ValidAutoOff checks an idle time: 0 is never, up to a week otherwise.
func ValidAutoOff(minutes int) error {
	if minutes < 0 || minutes > MaxAutoOffMinutes {
		return fmt.Errorf("auto_off_minutes is between 0 (never) and %d", MaxAutoOffMinutes)
	}
	return nil
}

// StepDesktop is a starting machine whose container is up and whose desktop is coming up.
const StepDesktop = "desktop"

// Power is what is known about a kitty's machine now.
type Power struct {
	State string `json:"state"` // on | starting | off | error
	// Step is what a starting machine is doing: StepDesktop once its container is up and its
	// desktop is still coming up (empty before, while the container is made or its image pulled).
	Step  string `json:"step,omitempty"`
	Error string `json:"error,omitempty"`
	// OffReason is why it is off: idle, manual or kitty (empty when unknown or on).
	OffReason string `json:"off_reason,omitempty"`
	// AutoOffMinutes is the idle time before it turns itself off; 0 is never. AutoOffCustom is
	// whether the kitty has a value of its own (otherwise it is the gateway's default).
	AutoOffMinutes int  `json:"auto_off_minutes"`
	AutoOffCustom  bool `json:"auto_off_custom"`
	// InUse is whether something keeps it on right now, and InUseBy what (the Use constants).
	InUse   bool   `json:"in_use"`
	InUseBy string `json:"in_use_by,omitempty"`
	// IdleSeconds is the time since it was last used; OffInSeconds the time left before it turns
	// itself off (only when on, idle counting and not "never").
	IdleSeconds  int  `json:"idle_seconds"`
	OffInSeconds *int `json:"off_in_seconds,omitempty"`
}

// OffOptions say who turns a machine off and how far the check for use goes.
type OffOptions struct {
	// Reason is one of the Off constants.
	Reason string
	// Self is the kitty turning off its own machine in the middle of its task: the task itself
	// does not count as use, the person watching or controlling still does.
	Self bool
	// Force turns it off whatever is using it.
	Force bool
}

// InUseError is a machine that something is using, so it was not turned off.
type InUseError struct {
	Kitty string
	By    string
}

func (e *InUseError) Error() string {
	what := map[string]string{
		UseTask:    "is working on a task",
		UseControl: "has the person controlling its desktop",
		UseWatch:   "has the person watching its desktop",
		UseCommand: "is running a command",
	}[e.By]
	if what == "" {
		what = "is in use"
	}
	return fmt.Sprintf("%s %s: wait for it to finish, or turn it off anyway", e.Kitty, what)
}

// ErrNoMachine is returned for a kitty without a desktop.
var ErrNoMachine = errors.New("this kitty has no machine (no desktop)")
