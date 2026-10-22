package connector

import (
	"strings"
)

// Risk says how far an action may go without the person's say.
type Risk int

// The levels: nothing to ask; ask until the person trusts the kitty with it; never unasked.
const (
	RiskNone Risk = iota
	RiskAsk
	RiskAlways
)

// String is the level as the catalogue and the page name it.
func (r Risk) String() string {
	switch r {
	case RiskAsk:
		return "ask"
	case RiskAlways:
		return "always"
	}
	return ""
}

// askActions are the actions that reach other people, publish, or move money: the ones a person
// wants to see before they happen, until the kitty has earned their trust with them.
var askActions = map[string]bool{
	"send": true, "post": true, "reply": true, "comment": true, "like": true, "boost": true, "repost": true,
	"send_mail": true, "send_invoice": true, "create_pr": true, "run": true,
}

// alwaysActions are never done without approval, whatever the trust.
var alwaysActions = map[string]bool{"refund": true, "delete": true, "delete_file": true}

// RiskOf is the risk of an action of a kind. Reading and the person's own tools (a card, a
// task, a row, a diagram) carry none.
func RiskOf(kind, action string) Risk {
	action = strings.ToLower(action)
	switch {
	case alwaysActions[action]:
		return RiskAlways
	case kind == "shell":
		return RiskAlways
	case askActions[action]:
		return RiskAsk
	}
	return RiskNone
}

// RiskOfCall is RiskOf for a call: an HTTP call is judged by its method (a GET reads, a DELETE
// is never unasked, any other writes), the rest by their action.
func RiskOfCall(kind string, args map[string]any) Risk {
	action := ActionOf(kind, args)
	if kind == "http" {
		switch strings.ToUpper(str(args, "method")) {
		case "", "GET", "HEAD":
			return RiskNone
		case "DELETE":
			return RiskAlways
		}
		return RiskAsk
	}
	return RiskOf(kind, action)
}

// ActionOf is the action a call asks for: its "action", or the kind's first when it names none.
func ActionOf(kind string, args map[string]any) string {
	if a := strings.ToLower(str(args, "action")); a != "" {
		return a
	}
	if k, ok := LookupKind(kind); ok && len(k.Actions) > 0 {
		return k.Actions[0].Name
	}
	return ""
}
