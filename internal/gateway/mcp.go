package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/mcp"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/soul"
)

// MCPPath is where the MCP server (docs/MCP.md) answers.
const MCPPath = "/mcp"

// defaultTurns is how many of a chat's last turns chat_get returns.
const defaultTurns = 10

// WithMCP serves the MCP connector at /mcp, so Claude can run the fleet as an orchestrator. It has
// a token of its own (the gateway's token is never accepted there, so it can be revoked alone);
// an empty token leaves the connector off. exec lets it run commands inside a kitty's container,
// which is off unless the person turned it on.
func (s *Server) WithMCP(token string, exec bool) *Server {
	return s.WithMCPOptions(token, MCPOptions{Exec: exec})
}

// WithMCPOAuth is WithMCP that, with oauth, also lets a remote client such as claude.ai sign in
// (docs/MCP.md): the person types the connector's token once on a consent page of the gateway and
// the client gets tokens of its own. Put https in front first (public_url).
func (s *Server) WithMCPOAuth(token string, exec, oauth bool) *Server {
	return s.WithMCPOptions(token, MCPOptions{Exec: exec, OAuth: oauth})
}

// MCPOptions say what the connector offers beyond reading and driving the fleet.
type MCPOptions struct {
	// Exec adds kitty_exec, a command in a kitty's container.
	Exec bool
	// OAuth lets remote clients sign in (claude.ai).
	OAuth bool
	// Approve adds approval_approve: Claude may say yes to an action held for the person.
	Approve bool
	// ClientsFile remembers the OAuth clients and the ones revoked (empty: in memory only).
	ClientsFile string
}

// WithMCPOptions serves the connector with these options; an empty token leaves it off.
func (s *Server) WithMCPOptions(token string, opt MCPOptions) *Server {
	if token == "" {
		return s
	}
	var o *mcp.OAuth
	if opt.OAuth {
		o = mcp.NewOAuth(token, s.issuer)
		o.Path = opt.ClientsFile
		o.Allow = func(r *http.Request) bool { return s.limit.wait(s.clientIP(r)) == 0 }
		o.Failed = func(r *http.Request) { s.limit.fail(s.clientIP(r)) }
		o.Register(s.mux)
		// The person lists and cuts off the clients from the gateway's own API, with its own token.
		s.mux.Handle("GET /v1/mcp/clients", s.auth(func(w http.ResponseWriter, _ *http.Request) {
			reply(w, http.StatusOK, map[string]any{"clients": o.Connections()})
		}))
		s.mux.Handle("DELETE /v1/mcp/clients/{id}", s.auth(func(w http.ResponseWriter, r *http.Request) {
			found, err := o.Revoke(r.PathValue("id"))
			switch {
			case err != nil:
				fail(w, http.StatusInternalServerError, err)
			case !found:
				fail(w, http.StatusNotFound, errors.New("no such client"))
			default:
				reply(w, http.StatusOK, map[string]any{"revoked": r.PathValue("id")})
			}
		}))
	}
	srv := mcp.New("kitties", s.version)
	tools := append(append(s.mcpTools(opt.Exec), s.mcpMoreTools()...), s.mcpMemoryTools()...)
	if opt.Approve {
		tools = append(tools, s.mcpApproveTool())
	}
	for _, t := range tools {
		srv.Add(t)
	}
	// Methods are named so the page's "GET /" does not swallow them; GET and DELETE are answered 405.
	h := s.mcpAuth(token, o, srv)
	for _, m := range []string{"POST", "GET", "DELETE"} {
		s.mux.Handle(m+" "+MCPPath, h)
	}
	return s
}

// mcpAuth lets in a bearer token that is the MCP one, nothing else: no cookie, no query token. A
// browser page cannot reach it (a request that names another origin is refused, which stops a
// page from rebinding its own name to this address), and wrong guesses are counted like the
// gateway's.
func (s *Server) mcpAuth(token string, o *mcp.OAuth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				fail(w, http.StatusForbidden, errors.New("the MCP connector does not take requests from a web page"))
				return
			}
		}
		ip := s.clientIP(r)
		if wait := s.limit.wait(ip); wait > 0 {
			tooMany(w, wait)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == r.Header.Get("Authorization") || (subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 && (o == nil || !o.ValidAccess(got))) {
			if got != r.Header.Get("Authorization") {
				s.limit.fail(ip)
			}
			challenge := `Bearer realm="kitties"`
			if o != nil {
				challenge = o.Challenge(r)
			}
			w.Header().Set("WWW-Authenticate", challenge)
			fail(w, http.StatusUnauthorized, errors.New("a valid MCP bearer token is required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// obj is a JSON Schema object with the given properties and required names.
func obj(required []string, props map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

// jsonResult answers with v as JSON text.
func jsonResult(v any) (mcp.Result, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Content: []mcp.Content{mcp.Text(string(data))}}, nil
}

// args reads the arguments of a call; a wrong shape is the caller's error, not the tool's.
func args(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return &mcp.ArgError{Msg: "invalid arguments: " + err.Error()}
	}
	return nil
}

func need(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return &mcp.ArgError{Msg: name + " is required"}
	}
	return nil
}

type refArgs struct {
	Ref string `json:"ref"`
}

type idArgs struct {
	ID string `json:"id"`
}

var refSchema = obj([]string{"ref"}, map[string]any{"ref": str("the kitty's id or name")})

// mcpTools are the tools of the connector. Reading tools come first; the ones that change
// something say so in their annotations, and remove asks for confirm.
func (s *Server) mcpTools(exec bool) []mcp.Tool {
	read := mcp.Annotations{ReadOnly: true}
	write := mcp.Annotations{}
	danger := mcp.Annotations{Destructive: true}
	tools := []mcp.Tool{
		{Name: "fleet_status", Description: "Version, uptime, number of kitties and whether containers are available on this gateway.",
			InputSchema: obj(nil, map[string]any{}), Annotations: read,
			Call: func(context.Context, json.RawMessage) (mcp.Result, error) {
				has, err := s.fleet.HasContainers()
				out := map[string]any{"version": s.version, "uptime": time.Since(s.started).Round(time.Second).String(),
					"kitties": len(s.fleet.List()), "containers": has}
				if err != nil {
					out["containers_error"] = err.Error()
				}
				return jsonResult(out)
			}},
		{Name: "provider_list", Description: "The models the kitties can think with (names and kinds, never keys).",
			InputSchema: obj(nil, map[string]any{}), Annotations: read,
			Call: func(context.Context, json.RawMessage) (mcp.Result, error) {
				return jsonResult(map[string]any{"providers": s.fleet.Providers().List()})
			}},
		{Name: "kitty_list", Description: "Every kitty of the fleet with its status.",
			InputSchema: obj(nil, map[string]any{}), Annotations: read,
			Call: func(context.Context, json.RawMessage) (mcp.Result, error) {
				return jsonResult(map[string]any{"kitties": s.fleet.List()})
			}},
		{Name: "kitty_get", Description: "One kitty: its soul, provider, desktop and status.",
			InputSchema: refSchema, Annotations: read,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a refArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				k, err := s.fleet.Get(a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(k)
			}},
		{Name: "kitty_create", Description: "Create a kitty and, unless start is false, start it (a first start may download its desktop image, so it comes up in the background).",
			InputSchema: obj([]string{"name"}, map[string]any{
				"name":     str("lowercase letters, digits and dashes"),
				"provider": str("a provider name from provider_list; empty is the default one"),
				"soul":     map[string]any{"type": "object", "description": "who the kitty is: name, persona, tone, traits (list), instructions, language"},
				"desktop":  map[string]any{"type": "boolean", "description": "give it a desktop container; unset follows the deployment"},
				"start":    map[string]any{"type": "boolean", "description": "start it after creating (default true)"},
			}), Annotations: write,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Name     string    `json:"name"`
					Provider string    `json:"provider"`
					Soul     soul.Soul `json:"soul"`
					Desktop  *bool     `json:"desktop"`
					Start    *bool     `json:"start"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("name", a.Name); err != nil {
					return mcp.Result{}, err
				}
				k, err := s.fleet.Create(ctx, orchestrator.Spec{Name: a.Name, Provider: a.Provider, Soul: a.Soul, Desktop: a.Desktop})
				if err != nil {
					return mcp.Result{}, err
				}
				if a.Start == nil || *a.Start {
					started, err := s.fleet.StartInBackground(k.ID)
					if err != nil {
						return jsonResult(map[string]any{"kitty": k, "warning": "created but not started: " + err.Error()})
					}
					k = started
				}
				return jsonResult(map[string]any{"kitty": k})
			}},
		s.lifecycleTool("kitty_start", "Start a kitty and wait for its desktop.", write, s.fleet.Start),
		s.lifecycleTool("kitty_stop", "Stop a kitty (its desktop goes off).", write, s.fleet.Stop),
		{Name: "kitty_remove", Description: "Delete a kitty with its goals and journal. Needs confirm: true.",
			InputSchema: obj([]string{"ref", "confirm"}, map[string]any{"ref": str("the kitty's id or name"),
				"confirm": map[string]any{"type": "boolean", "description": "must be true"}}), Annotations: danger,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref     string `json:"ref"`
					Confirm bool   `json:"confirm"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if !a.Confirm {
					return mcp.Result{}, errors.New("removing a kitty cannot be undone: call again with confirm: true")
				}
				id := ""
				if k, err := s.fleet.Get(a.Ref); err == nil {
					id = k.ID
				}
				if err := s.fleet.Remove(ctx, a.Ref); err != nil {
					return mcp.Result{}, err
				}
				if f, ok := s.agenda.(Forgetter); ok && id != "" {
					_ = f.Forget(id)
				}
				return jsonResult(map[string]any{"removed": a.Ref})
			}},
		{Name: "kitty_screenshot", Description: "A picture of a kitty's desktop right now.",
			InputSchema: refSchema, Annotations: read,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a refArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				png, err := s.fleet.Screenshot(ctx, a.Ref)
				if err != nil {
					return mcp.Result{}, err
				}
				return mcp.Result{Content: []mcp.Content{mcp.Image("image/png", base64.StdEncoding.EncodeToString(png))}}, nil
			}},
		{Name: "chat_list", Description: "The threads (id, title, members, last update), without their turns.",
			InputSchema: obj(nil, map[string]any{}), Annotations: read,
			Call: func(context.Context, json.RawMessage) (mcp.Result, error) {
				type row struct {
					ID        string    `json:"id"`
					Title     string    `json:"title"`
					Members   []string  `json:"members"`
					Turns     int       `json:"turns"`
					UpdatedAt time.Time `json:"updated_at"`
				}
				rows := []row{}
				for _, c := range s.chats.List() {
					rows = append(rows, row{c.ID, c.Title, c.Members, len(c.Turns), c.UpdatedAt})
				}
				return jsonResult(map[string]any{"chats": rows})
			}},
		{Name: "chat_create", Description: "Open a thread with some kitties (by name or id).",
			InputSchema: obj([]string{"members"}, map[string]any{"title": str("optional title"),
				"members": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "kitty names or ids"}}),
			Annotations: write,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Title   string   `json:"title"`
					Members []string `json:"members"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if len(a.Members) == 0 {
					return mcp.Result{}, &mcp.ArgError{Msg: "members is required"}
				}
				c, err := s.chats.Create(a.Title, a.Members)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"chat": c})
			}},
		{Name: "chat_get", Description: "A thread with its last turns (default 10; a desktop task goes on in the background, so poll this to follow it).",
			InputSchema: obj([]string{"id"}, map[string]any{"id": str("the thread's id"),
				"last": map[string]any{"type": "integer", "description": "how many of the last turns to return"}}), Annotations: read,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					ID   string `json:"id"`
					Last int    `json:"last"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				c, err := s.chats.Get(a.ID)
				if err != nil {
					return mcp.Result{}, err
				}
				n := a.Last
				if n <= 0 {
					n = defaultTurns
				}
				view := *c
				if len(view.Turns) > n {
					view.Turns = append([]chat.Turn(nil), view.Turns[len(view.Turns)-n:]...)
				}
				return jsonResult(map[string]any{"chat": view, "total_turns": len(c.Turns)})
			}},
		{Name: "chat_send", Description: "Send a message or task to a thread and return the kitties' turns. A long desktop task keeps going after this returns: follow it with chat_get.",
			InputSchema: obj([]string{"id", "text"}, map[string]any{"id": str("the thread's id"), "text": str("what to tell the kitties")}),
			Annotations: write,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					ID   string `json:"id"`
					Text string `json:"text"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if err := need("text", a.Text); err != nil {
					return mcp.Result{}, err
				}
				turns, err := s.chats.Send(ctx, a.ID, a.Text)
				if err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"turns": turns})
			}},
		{Name: "chat_cancel", Description: "Stop the task a thread is working on.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": str("the thread's id")}), Annotations: write,
			Call: func(_ context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a idArgs
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				return jsonResult(map[string]any{"cancelled": s.chats.Cancel(a.ID)})
			}},
	}
	if exec {
		tools = append(tools, mcp.Tool{Name: "kitty_exec", Description: "Run a command inside a kitty's container (argv, no shell) and return its output.",
			InputSchema: obj([]string{"ref", "argv"}, map[string]any{"ref": str("the kitty's id or name"),
				"argv": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the program and its arguments"}}),
			Annotations: danger,
			Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
				var a struct {
					Ref  string   `json:"ref"`
					Argv []string `json:"argv"`
				}
				if err := args(raw, &a); err != nil {
					return mcp.Result{}, err
				}
				if len(a.Argv) == 0 {
					return mcp.Result{}, &mcp.ArgError{Msg: "argv is required"}
				}
				out, err := s.fleet.Exec(ctx, a.Ref, a.Argv...)
				if err != nil {
					return mcp.Result{Content: []mcp.Content{mcp.Text(out + "\n" + err.Error())}, IsError: true}, nil
				}
				return mcp.Result{Content: []mcp.Content{mcp.Text(out)}}, nil
			}})
	}
	return tools
}

// lifecycleTool is start or stop: run op, answer with the kitty as it is after.
func (s *Server) lifecycleTool(name, desc string, ann mcp.Annotations, op func(context.Context, string) error) mcp.Tool {
	return mcp.Tool{Name: name, Description: desc, InputSchema: refSchema, Annotations: ann,
		Call: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
			var a refArgs
			if err := args(raw, &a); err != nil {
				return mcp.Result{}, err
			}
			if err := op(ctx, a.Ref); err != nil {
				return mcp.Result{}, err
			}
			k, err := s.fleet.Get(a.Ref)
			if err != nil {
				return jsonResult(map[string]any{"ok": true})
			}
			return jsonResult(map[string]any{"kitty": k})
		}}
}

// issuer is the public address of a request: public_url when set, else what the client typed.
func (s *Server) issuer(r *http.Request) string {
	if s.publicURL != "" {
		return strings.TrimRight(s.publicURL, "/")
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + s.requestHost(r)
}
