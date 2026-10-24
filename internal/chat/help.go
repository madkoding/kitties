package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/vault"
)

// WHAT THE PERSON ANSWERS WHEN A KITTY ASKS FOR HELP.
//
// A kitty that calls ask_help waits. The person can take its desktop and give it back, or answer
// right there: write what it needs (a code, a password, a yes) and either tell it, or, when it is
// a secret, have it typed on the kitty's desktop without the kitty being shown it.

// HelpAnswer is what the person gives a kitty that asked.
type HelpAnswer struct {
	// Text is what the kitty needs.
	Text string
	// Secret has the text typed on the kitty's desktop, and not told to the kitty.
	Secret bool
	// Enter presses Enter after typing it (only with Secret).
	Enter bool
	// Save keeps the secret in the vault under this name, for the kitty that asked, so it can type
	// it again when it needs it without asking (only with Secret).
	Save string
}

// maxHelpText is the longest answer.
const maxHelpText = 2000

// ErrNoHelp is what an answer gets when nobody is waiting for it.
var ErrNoHelp = errors.New("no such help request: the kitty is not waiting for an answer")

// helpKey names a request for help: the turn in which a kitty is waiting.
func helpKey(id string, index int) string { return fmt.Sprintf("%s:%d", id, index) }

// AnswerHelp gives the kitty waiting in a turn the person's answer.
func (s *Service) AnswerHelp(id string, index int, a HelpAnswer) error {
	a.Text = strings.TrimSpace(a.Text)
	switch {
	case a.Text == "":
		return errors.New("the answer is empty")
	case len(a.Text) > maxHelpText:
		return fmt.Errorf("the answer is too long (%d characters at most)", maxHelpText)
	}
	s.mu.RLock()
	wait, ok := s.helps[helpKey(id, index)]
	s.mu.RUnlock()
	if !ok {
		return ErrNoHelp
	}
	select {
	case wait <- a:
		return nil
	default:
		return errors.New("the kitty already has an answer")
	}
}

// expectHelp starts waiting for an answer for a turn; the returned function stops waiting.
func (s *Service) expectHelp(id string, index int) (<-chan HelpAnswer, func()) {
	wait := make(chan HelpAnswer, 1)
	key := helpKey(id, index)
	s.mu.Lock()
	if s.helps == nil {
		s.helps = map[string]chan HelpAnswer{}
	}
	s.helps[key] = wait
	s.mu.Unlock()
	return wait, func() {
		s.mu.Lock()
		delete(s.helps, key)
		s.mu.Unlock()
	}
}

// apply does what the answer says and returns what the kitty is told.
func (s *Service) applyHelp(ctx context.Context, id string, index int, k *kitty.Kitty, a HelpAnswer) string {
	if !a.Secret {
		s.appendStep(id, index, Step{At: s.now(), Action: "helped", Args: map[string]any{"told": true}, Result: a.Text})
		return "the person answered: " + a.Text + "\nLook at the screenshot and continue."
	}
	step := Step{At: s.now(), Action: "helped", Args: map[string]any{"secret": true}, Result: "typed on its desktop"}
	if a.Save != "" && s.vault != nil {
		if err := s.vault.Put(a.Save, a.Text, []string{k.ID}); err != nil {
			step.Error = "it was not saved: " + err.Error()
		} else {
			step.Args["saved"] = a.Save
		}
	}
	s.appendStep(id, index, step)
	if err := desktop.TypeSecret(ctx, s.runner(), k.ID, a.Text, a.Enter); err != nil {
		return "the person tried to type it on your desktop and it failed: " + err.Error()
	}
	return "the person typed what you asked for on your desktop (you are not shown it, and must not ask for it again). Look at the screenshot and continue."
}

// SetVault gives the chats the vault of the secrets a kitty may type without reading them.
func (s *Service) SetVault(v *vault.Store) { s.vault = v }

// typeSecret is a kitty typing a secret the person saved for it, by name: it goes from the vault
// to the keyboard, and neither the kitty nor its model, the chat's steps or the logs ever hold it.
func (s *Service) typeSecret(ctx context.Context, id string, index int, k *kitty.Kitty, a action) outcome {
	name := strings.TrimSpace(argText(a.args, "name", "text"))
	if s.vault == nil {
		return outcome{action: a, err: "there are no saved secrets here: ask the person with ask_help"}
	}
	text, err := s.vault.Get(name, k.ID)
	if err != nil {
		msg := err.Error()
		if names := s.vault.Names(k.ID); len(names) > 0 {
			msg += "; the ones you may use: " + strings.Join(names, ", ")
		} else {
			msg += "; none is saved for you: ask the person with ask_help"
		}
		return outcome{action: a, err: msg}
	}
	s.waitWhileHeld(ctx, id, index, k.ID)
	enter, _ := a.args["enter"].(bool)
	step := Step{At: s.now(), Action: "type_secret", Args: map[string]any{"name": name}, Result: "typed"}
	o := outcome{action: a, text: "typed the secret " + name + " (you are not shown it)"}
	if err := desktop.TypeSecret(ctx, s.runner(), k.ID, text, enter); err != nil {
		step.Result, step.Error = "", err.Error()
		o = outcome{action: a, err: err.Error()}
	}
	s.appendStep(id, index, step)
	return o
}
