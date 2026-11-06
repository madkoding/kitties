package gateway

import (
	"context"
	"encoding/json"

	"github.com/madkoding/kitties/internal/mcp"
	"github.com/madkoding/kitties/internal/memory"
)

// mcpMemoryTools let Claude read and tend what a kitty knows about the person and the work: the
// same notes the Memory panel shows. A note written here counts as the person's (pinned).
func (s *Server) mcpMemoryTools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "memory_list", Description: "What a kitty knows about the person and the work: its notes (id, scope, text, strength), how demanding the person is, and the trust earned per action.",
			InputSchema: obj([]string{"ref"}, map[string]any{"ref": str("the kitty's id or name"),
				"scope": str("only this scope: user, work, shared or project:<name>; empty is all")}),
			Annotations: mcp.Annotations{ReadOnly: true},
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref   string `json:"ref"`
					Scope string `json:"scope"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if s.memories == nil {
					return mcp.Result{}, errNoMemory
				}
				view, err := s.memories.MemoryOf(a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				if a.Scope != "" {
					kept := []memory.Note{}
					for _, n := range view.Notes {
						if n.Scope == a.Scope {
							kept = append(kept, n)
						}
					}
					view.Notes = kept
				}
				return jsonResult(map[string]any{"memory": view})
			}},
		{Name: "memory_remember", Description: "Tell a kitty something to keep in mind from now on (a preference, a correction, a fact of a project). Short, one thing each, never secrets. It is pinned: a distillation never drops it.",
			InputSchema: obj([]string{"ref", "text"}, map[string]any{"ref": str("the kitty's id or name"),
				"text":  str("the note, up to 240 characters"),
				"scope": str("user (how the person likes things, the default), work (a lesson about the work), shared (every kitty sees it) or project:<name>")}),
			Annotations: mcp.Annotations{},
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref   string `json:"ref"`
					Text  string `json:"text"`
					Scope string `json:"scope"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("text", a.Text); err != nil {
					return mcp.Result{}, err
				}
				if s.memories == nil {
					return mcp.Result{}, errNoMemory
				}
				n, err := s.memories.Remember(a.Ref, a.Scope, a.Text)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"note": n})
			}},
		{Name: "memory_edit", Description: "Correct one of a kitty's notes (or a shared one) in place; it becomes the person's, pinned. Read the ids with memory_list.",
			InputSchema: obj([]string{"ref", "id", "text"}, map[string]any{"ref": str("the kitty's id or name"),
				"id": str("the note's id"), "text": str("the note as it should read, up to 240 characters")}),
			Annotations: mcp.Annotations{},
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref  string `json:"ref"`
					ID   string `json:"id"`
					Text string `json:"text"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("id", a.ID); err != nil {
					return mcp.Result{}, err
				}
				if err := need("text", a.Text); err != nil {
					return mcp.Result{}, err
				}
				if s.memories == nil {
					return mcp.Result{}, errNoMemory
				}
				n, err := s.memories.EditNote(a.Ref, a.ID, a.Text)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"note": n})
			}},
		{Name: "memory_forget", Description: "Drop a kitty's note that is wrong or out of date. Read the ids with memory_list.",
			InputSchema: obj([]string{"ref", "id"}, map[string]any{"ref": str("the kitty's id or name"), "id": str("the note's id")}),
			Annotations: mcp.Annotations{Destructive: true},
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref string `json:"ref"`
					ID  string `json:"id"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("id", a.ID); err != nil {
					return mcp.Result{}, err
				}
				if s.memories == nil {
					return mcp.Result{}, errNoMemory
				}
				if err := s.memories.ForgetNote(a.Ref, a.ID); err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"forgotten": a.ID})
			}},
	}
}
