package chat

import (
	"fmt"
	"strings"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/kitty"
)

// A kitty acts on its connections as far as their accounts allow: what an account may do is the
// service's to limit. It limits itself only when the person asks it to, and the limit goes to
// the same list the person edits (approvals.Rule).

const limitHelp = "Limit your own actions on a connection, only when the person asks you to (\"ask me before posting\", " +
	"\"never delete anything\"): ask makes the action wait for their yes, never refuses it, none lifts the limit."

// limitPrompt tells the kitty how to limit itself, when there is something to limit.
func (s *Service) limitPrompt(allowed map[string]string) string {
	if s.approvals == nil || len(allowed) == 0 {
		return ""
	}
	return "\nYour actions on your connections are not limited: do what the task needs. When the person asks you to hold " +
		"back (to ask first, or never to do something), keep it as a limit with " +
		"@limit set {\"connector\": \"name\", \"action\": \"post\", \"mode\": \"ask\"} (mode ask, never, or none to lift it; " +
		"leave the action out for all of the connection's), and tell them in plain words. Never add a limit they did not ask for.\n"
}

// runLimit sets, changes or lifts a limit on one of the kitty's own connections.
func (s *Service) runLimit(id string, a action, k *kitty.Kitty, allowed map[string]string, turn *Turn) outcome {
	if s.approvals == nil {
		return outcome{action: a, err: "limits are not kept here"}
	}
	conn := argText(a.args, "connector", "connection", "name")
	if conn == "" {
		return outcome{action: a, err: "say which connection the limit is for"}
	}
	if _, ok := allowed[conn]; !ok {
		return outcome{action: a, err: fmt.Sprintf("%q is not one of your connections", conn)}
	}
	action := strings.ToLower(argText(a.args, "action"))
	mode := strings.ToLower(argText(a.args, "mode"))
	switch mode {
	case "none", "off", "lift", "remove", "free":
		mode = ""
	case "":
		mode = approvals.ModeAsk
	}
	previous := s.approvals.ModeOf(k.ID, conn, action)
	if _, err := s.approvals.SetRule(approvals.Rule{Kitty: k.ID, KittyName: k.Name, Connector: conn, Action: action, Mode: mode}); err != nil {
		return outcome{action: a, err: err.Error()}
	}
	if action == "" {
		action = approvals.AnyAction
	}
	turn.Limits = append(turn.Limits, LimitRef{Connector: conn, Action: action, Mode: mode, Previous: previous})
	what := action + " on " + conn
	if action == approvals.AnyAction {
		what = "any action on " + conn
	}
	var text string
	switch mode {
	case approvals.ModeAsk:
		text = "from now on, " + what + " waits for the person's yes"
	case approvals.ModeNever:
		text = "from now on, " + what + " is never done"
	case approvals.ModeAuto:
		text = "from now on, " + what + " goes ahead without asking"
	default:
		text = "the limit on " + strings.TrimPrefix(what, "any action on ") + " is lifted"
	}
	s.note(k, agenda.KindApproval, s.goalOf(id), id, "limit: "+text)
	return outcome{action: a, text: text}
}
