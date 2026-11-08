package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/mcp"
)

// Computer says where a kitty's desktop runs: local or cloud.
type Computer interface {
	Compute(ref string) (string, error)
}

// mcpMoreTools are the tools for the machine, the approvals and the goals. They are looked up
// when called, so the order in which the gateway is wired does not matter. Approving is not among
// them unless the person allows it (mcpApproveTool).
func (s *Server) mcpMoreTools() []mcp.Tool {
	read := mcp.Annotations{ReadOnly: true}
	write := mcp.Annotations{}
	danger := mcp.Annotations{Destructive: true}
	return []mcp.Tool{
		{Name: "kitty_compute", Description: "Where a kitty's desktop runs, local (a container on the gateway's machine) or cloud, its power state, its RAM and swap limits and, when on, the memory it uses and has free. Compute follows the configuration; it is not switched from here.",
			InputSchema: refSchema, Annotations: read,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a refArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				c, ok := s.fleet.(Computer)
				if !ok {
					return mcp.Result{}, errors.New("this gateway cannot tell where desktops run")
				}
				where, err := c.Compute(a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				out := map[string]any{"compute": where}
				if p, ok := s.fleet.(Powerer); ok {
					if power, err := p.Power(a.Ref); err == nil {
						out["power"] = power
						if m := s.memoryOf(ctx, a.Ref, power); m != nil {
							out["memory"] = m
						}
					}
				}
				return jsonResult(out)
			}},
		{Name: "kitty_power", Description: "Turn a kitty's machine on or off. Off fails while something uses it, unless force is true.",
			InputSchema: obj([]string{"ref", "on"}, map[string]any{"ref": str("the kitty's id or name"),
				"on":    map[string]any{"type": "boolean"},
				"force": map[string]any{"type": "boolean", "description": "turn it off even if in use"}}),
			Annotations: danger,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref   string `json:"ref"`
					On    *bool  `json:"on"`
					Force bool   `json:"force"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if a.On == nil {
					return mcp.Result{}, &mcp.ArgError{Msg: "on is required"}
				}
				p, ok := s.fleet.(Powerer)
				if !ok {
					return mcp.Result{}, errors.New("this gateway cannot turn machines on and off")
				}
				var err error
				if *a.On {
					_, err = p.PowerOn(a.Ref)
				} else {
					err = p.PowerOff(ctx, a.Ref, kitty.OffOptions{Reason: kitty.OffManual, Force: a.Force})
				}
				if err != nil {
					return mcp.Result{}, err
				}
				power, err := p.Power(a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"power": power})
			}},
		{Name: "approval_list", Description: "Actions held for the person's yes (status: pending, by default).",
			InputSchema: obj(nil, map[string]any{"status": str("pending, approved or rejected; empty is all")}), Annotations: read,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Status string `json:"status"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.approver == nil {
					return mcp.Result{}, errNoApprovals
				}
				list, err := s.approver.ListApprovals(a.Status)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"approvals": list})
			}},
		{Name: "approval_reject", Description: "Reject a held action. Approving stays with the person.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": str("the approval's id"), "reason": str("why, told to the kitty")}),
			Annotations: write,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					ID     string `json:"id"`
					Reason string `json:"reason"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.approver == nil {
					return mcp.Result{}, errNoApprovals
				}
				ap, err := s.approver.DecideApproval(ctx, a.ID, false, a.Reason, false)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"approval": ap})
			}},
		{Name: "goal_list", Description: "The goals kitties pursue on their own, of one kitty or all.",
			InputSchema: obj(nil, map[string]any{"kitty": str("a kitty's id or name; empty is all")}), Annotations: read,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Kitty string `json:"kitty"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.agenda == nil {
					return mcp.Result{}, errNoAgenda
				}
				id := ""
				if a.Kitty != "" {
					k, err := s.fleet.Get(a.Kitty)
					if err != nil {
						return mcp.Result{}, err
					}
					id = k.ID
				}
				goals := s.agenda.Goals(id)
				if goals == nil {
					goals = []*agenda.Goal{}
				}
				return jsonResult(map[string]any{"goals": goals})
			}},
		{Name: "goal_add", Description: "Give a kitty a goal: a task it runs on a cron schedule (every), when a connector reports something (on), or when asked (goal_run).",
			InputSchema: obj([]string{"kitty", "title"}, map[string]any{"kitty": str("a kitty's id or name"), "title": str("short name"),
				"text":  str("what the kitty is told each run; defaults to the title"),
				"every": str("cron expression, e.g. '0 9 * * 1-5' or '@hourly'"),
				"on":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "connector names whose events run it"},
				"flow":  str("a page flow the kitty saved (page_flow save): each run plays it first without the model; the kitty takes over only when it fails")}),
			Annotations: write,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a goalBody
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.agenda == nil {
					return mcp.Result{}, errNoAgenda
				}
				k, err := s.fleet.Get(a.Kitty)
				if err != nil {
					return mcp.Result{}, err
				}
				g := a.goal()
				g.Kitty = k.ID
				if err := s.checkTriggers(g); err != nil {
					return mcp.Result{}, err
				}
				saved, err := s.agenda.AddGoal(g)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"goal": saved})
			}},
		{Name: "goal_run", Description: "Run a goal now; returns the chat it is worked in (follow it with chat_get).",
			InputSchema: obj([]string{"id"}, map[string]any{"id": str("the goal's id")}), Annotations: write,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a idArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.agenda == nil {
					return mcp.Result{}, errNoAgenda
				}
				chatID, err := s.agenda.RunNow(ctx, a.ID)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"chat": chatID})
			}},
		{Name: "goal_remove", Description: "Delete a goal.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": str("the goal's id")}), Annotations: danger,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a idArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.agenda == nil {
					return mcp.Result{}, errNoAgenda
				}
				if err := s.agenda.RemoveGoal(a.ID); err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"removed": a.ID})
			}},
		{Name: "kitty_journal", Description: "A kitty's recent journal: what it did on its goals.",
			InputSchema: obj([]string{"ref"}, map[string]any{"ref": str("the kitty's id or name"),
				"n": map[string]any{"type": "integer", "description": "entries, 1 to 500 (default 50)"}}), Annotations: read,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref string `json:"ref"`
					N   int    `json:"n"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.agenda == nil {
					return mcp.Result{}, errNoAgenda
				}
				k, err := s.fleet.Get(a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				if a.N <= 0 || a.N > 500 {
					a.N = 50
				}
				entries := s.agenda.Recent(k.ID, a.N)
				if entries == nil {
					entries = []agenda.Entry{}
				}
				return jsonResult(map[string]any{"journal": entries})
			}},
	}
}

// mcpApproveTool lets Claude say yes to one held action. It is added only when the person turned
// on gateway.mcp.allow_approve; it never makes the approval permanent ("always").
func (s *Server) mcpApproveTool() mcp.Tool {
	return mcp.Tool{Name: "approval_approve", Description: "Approve one action held for the person's yes (this time only). Read it with approval_list first.",
		InputSchema: obj([]string{"id"}, map[string]any{"id": str("the approval's id")}), Annotations: mcp.Annotations{Destructive: true},
		Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
			var a idArgs
			if err := args(raw, &a); err != nil {
				return mcp.Result{}, err
			}
			if err := need("id", a.ID); err != nil {
				return mcp.Result{}, err
			}
			if s.approver == nil {
				return mcp.Result{}, errNoApprovals
			}
			ap, err := s.approver.DecideApproval(ctx, a.ID, true, "", false)
			if err != nil {
				return mcp.Result{}, err
			}
			return jsonResult(map[string]any{"approval": ap})
		}}
}
