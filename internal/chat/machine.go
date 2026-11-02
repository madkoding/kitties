package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

// THE KITTY KNOWS ITS MACHINE.
//
// A kitty's machine (its container) can be off: the person turned it off, the kitty did, or it
// turned itself off after a while without use. The kitty is told its state in a message that
// goes last in each request and is marked volatile, so the provider's prompt cache ends before
// it and the stable prefix (the system prompt, the tools and the conversation) is never spoiled
// by a state that changes. It can ask for the state, and turn the machine on or off, with the
// machine tool; and a desktop tool used while the machine is off turns it on first.

// Machine is the fleet as the power of a kitty's machine, when it is one (the orchestrator is).
type Machine interface {
	Power(ref string) (kitty.Power, error)
	PowerOnWait(ctx context.Context, ref string, wait time.Duration) error
	PowerOff(ctx context.Context, ref string, o kitty.OffOptions) error
}

func (s *Service) machine() Machine {
	m, _ := s.fleet.(Machine)
	return m
}

// machineWait is how long a desktop tool waits for a machine it turned on.
var machineWait = 3 * time.Minute

// describePower says the state of a machine in a sentence.
func describePower(p kitty.Power) string {
	switch p.State {
	case kitty.PowerOn:
		out := "on"
		switch {
		case p.InUse:
			out += " (in use)"
		case p.AutoOffMinutes == 0:
			out += "; it never turns itself off"
		case p.OffInSeconds != nil:
			out += fmt.Sprintf("; it turns itself off after %d minutes without use (in about %d min)", p.AutoOffMinutes, (*p.OffInSeconds+59)/60)
		default:
			out += fmt.Sprintf("; it turns itself off after %d minutes without use", p.AutoOffMinutes)
		}
		return out
	case kitty.PowerStarting:
		return "starting (its desktop is coming up; the first start downloads an image)"
	case kitty.PowerError:
		return "failed to start: " + p.Error
	}
	switch p.OffReason {
	case kitty.OffIdle:
		return "off (it turned itself off after a time without use)"
	case kitty.OffManual:
		return "off (the person turned it off)"
	case kitty.OffKitty:
		return "off (you turned it off)"
	}
	return "off"
}

// machineNote is the message that tells the kitty the state of its machine, empty when it has
// none to know about.
func (s *Service) machineNote(k *kitty.Kitty) string {
	m := s.machine()
	if m == nil || !k.Desktop {
		return ""
	}
	p, err := m.Power(k.ID)
	if err != nil {
		return ""
	}
	note := "[Your machine, now: " + describePower(p) + "."
	if p.State != kitty.PowerOn {
		note += " A desktop tool turns it on by itself and waits for it; the machine tool turns it on or off, or tells you its state."
	}
	return note + "]"
}

// withMachine adds the machine's state at the end of the messages of one request, as a volatile
// message: it is not kept in the conversation, and the prompt cache ends before it.
func (s *Service) withMachine(messages []provider.Message, k *kitty.Kitty) []provider.Message {
	note := s.machineNote(k)
	if note == "" {
		return messages
	}
	out := make([]provider.Message, 0, len(messages)+1)
	out = append(out, messages...)
	return append(out, provider.Message{Role: "user", Content: note, Volatile: true})
}

// runMachine runs the machine tool: status, on or off.
func (s *Service) runMachine(ctx context.Context, a action, k *kitty.Kitty) outcome {
	m := s.machine()
	if m == nil || !k.Desktop {
		return outcome{action: a, err: "you have no machine"}
	}
	verb := strings.ToLower(strings.TrimSpace(argText(a.args, "action", "text")))
	if verb == "" {
		verb = "status"
	}
	switch verb {
	case "status":
	case "on":
		if err := m.PowerOnWait(ctx, k.ID, machineWait); err != nil {
			return outcome{action: a, err: err.Error()}
		}
	case "off":
		if err := m.PowerOff(ctx, k.ID, kitty.OffOptions{Reason: kitty.OffKitty, Self: true}); err != nil {
			return outcome{action: a, err: err.Error()}
		}
	default:
		return outcome{action: a, err: fmt.Sprintf("machine: %q is not status, on or off", verb)}
	}
	p, err := m.Power(k.ID)
	if err != nil {
		return outcome{action: a, err: err.Error()}
	}
	return outcome{action: a, text: "your machine is " + describePower(p)}
}

// ensureMachine turns the kitty's machine on, and waits for it, before a desktop tool runs. The
// turn shows a step when it had to. The error says plainly why the tool cannot run.
func (s *Service) ensureMachine(ctx context.Context, id string, index int, k *kitty.Kitty) error {
	m := s.machine()
	if m == nil {
		return nil
	}
	p, err := m.Power(k.ID)
	if err != nil || p.State == kitty.PowerOn {
		return nil
	}
	step := Step{At: s.now(), Action: "machine", Args: map[string]any{"action": "on"}}
	if err := m.PowerOnWait(ctx, k.ID, machineWait); err != nil {
		step.Error = err.Error()
		s.appendStep(id, index, step)
		return fmt.Errorf("your machine is %s and could not be turned on: %v", describePower(p), err)
	}
	step.Result = "the machine was " + describePower(p) + "; it is on now"
	s.appendStep(id, index, step)
	return nil
}
