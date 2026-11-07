package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/mcp"
	"github.com/madkoding/kitties/internal/orchestrator"
)

// The lead kitty (docs/AUTONOMY.md#the-lead-kitty): one kitty of the fleet that orchestrates
// the others and reports for all of them. It is a flag of the kitty (PATCH /v1/kitties/{ref}
// {"lead": true}); these routes and tools name it, change it and reach its Reports thread.

// Reporter opens the lead's Reports thread, where the other kitties' news land.
type Reporter interface {
	ReportsChat(lead *kitty.Kitty) (string, error)
}

// lead is the fleet's lead kitty, nil when none is.
func (s *Server) lead() *kitty.Kitty {
	for _, k := range s.fleet.List() {
		if k.Lead {
			return k
		}
	}
	return nil
}

// leadView is the lead and its Reports thread, for the API and the MCP tools.
func (s *Server) leadView() map[string]any {
	out := map[string]any{"lead": nil}
	lead := s.lead()
	if lead == nil {
		return out
	}
	out["lead"] = lead
	if r, ok := s.chats.(Reporter); ok {
		if id, err := r.ReportsChat(lead); err == nil {
			out["reports"] = id
		}
	}
	return out
}

// setLead makes the kitty ref the lead; an empty ref leaves the fleet without one.
func (s *Server) setLead(ref string) (map[string]any, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if lead := s.lead(); lead != nil {
			no := false
			if _, err := s.fleet.Edit(lead.ID, orchestrator.Update{Lead: &no}); err != nil {
				return nil, err
			}
		}
		return s.leadView(), nil
	}
	yes := true
	if _, err := s.fleet.Edit(ref, orchestrator.Update{Lead: &yes}); err != nil {
		return nil, err
	}
	return s.leadView(), nil
}

// routesLead serves GET and PUT /v1/lead.
func (s *Server) routesLead(api func(http.HandlerFunc) http.Handler) {
	s.mux.Handle("GET /v1/lead", api(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, s.leadView())
	}))
	s.mux.Handle("PUT /v1/lead", api(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kitty string `json:"kitty"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		out, err := s.setLead(body.Kitty)
		if err != nil {
			fail(w, codeFor(err), err)
			return
		}
		reply(w, http.StatusOK, out)
	}))
}

// mcpLeadTools let Claude see and change the lead, and talk to it.
func (s *Server) mcpLeadTools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "fleet_lead", Description: "The lead kitty (the principal one, which orchestrates the others and reports for all of them) and the id of its Reports thread; lead is null when there is none.",
			InputSchema: obj(nil, map[string]any{}), Annotations: mcp.Annotations{ReadOnly: true},
			Call: func(context.Context, json.RawMessage) (mcp.Result, error) {
				return jsonResult(s.leadView())
			}},
		{Name: "fleet_lead_set", Description: "Make a kitty the lead (the one before stops being it), or pass an empty kitty to leave the fleet without one.",
			InputSchema: obj([]string{"kitty"}, map[string]any{"kitty": str("the kitty's id or name; empty for no lead")}),
			Annotations: mcp.Annotations{},
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Kitty string `json:"kitty"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				out, err := s.setLead(a.Kitty)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(out)
			}},
		{Name: "lead_ask", Description: "Give the lead kitty a task for the whole fleet, in its Reports thread: it hands the pieces to the others and answers with one report. A desktop task goes on after this returns: follow the thread with chat_get.",
			InputSchema: obj([]string{"text"}, map[string]any{"text": str("what the fleet should do, or a question about how things are going")}),
			Annotations: mcp.Annotations{},
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Text string `json:"text"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("text", a.Text); err != nil {
					return mcp.Result{}, err
				}
				view := s.leadView()
				id, _ := view["reports"].(string)
				if id == "" {
					return mcp.Result{}, errors.New("the fleet has no lead kitty: make one the lead first (fleet_lead_set)")
				}
				turns, err := s.chats.Send(ctx, id, a.Text)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"chat": id, "turns": turns})
			}},
	}
}
