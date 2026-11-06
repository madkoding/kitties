// Package mcp is a Model Context Protocol server: JSON-RPC 2.0 over the Streamable HTTP
// transport, answering each POST with one JSON body (no server-sent events, no sessions). It
// knows nothing about Kitties; the gateway registers the tools. Only the standard library.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Protocol versions this server speaks, newest first.
var versions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// maxBody is the biggest request taken.
const maxBody = 1 << 20

// Content is one piece of a tool's answer: text, or a picture (Data in base64).
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// Text is a text piece.
func Text(s string) Content { return Content{Type: "text", Text: s} }

// Image is a picture piece; data is base64.
func Image(mime, data string) Content { return Content{Type: "image", MIMEType: mime, Data: data} }

// Result is what a tool returns. IsError marks a failure the model should read and adapt to.
type Result struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Annotations hint at what a tool does, so a client can ask before the risky ones.
type Annotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
}

// Tool is one thing the model may call.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema any         `json:"inputSchema"`
	Annotations Annotations `json:"annotations"`
	// Call runs the tool with the arguments object as sent.
	Call func(ctx context.Context, args json.RawMessage) (Result, error) `json:"-"`
}

// Server holds the tools and answers requests.
type Server struct {
	name, version string
	tools         []Tool
	byName        map[string]int
}

// New makes a server that introduces itself with a name and a version.
func New(name, version string) *Server {
	return &Server{name: name, version: version, byName: map[string]int{}}
}

// Add registers a tool; a name already taken is replaced.
func (s *Server) Add(t Tool) {
	if i, ok := s.byName[t.Name]; ok {
		s.tools[i] = t
		return
	}
	s.byName[t.Name] = len(s.tools)
	s.tools = append(s.tools, t)
}

// Len is the number of tools.
func (s *Server) Len() int { return len(s.tools) }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes.
const (
	codeParse   = -32700
	codeRequest = -32600
	codeMethod  = -32601
	codeParams  = -32602
)

// ServeHTTP takes one JSON-RPC message (or a batch) per POST. A notification is answered 202
// with no body; GET and DELETE are refused 405 (this server opens no stream and keeps no session).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "this endpoint takes POST", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		write(w, http.StatusRequestEntityTooLarge, response{JSONRPC: "2.0", ID: null, Error: &rpcError{codeRequest, "request too big"}})
		return
	}
	if len(data) > 0 && data[0] == '[' {
		s.batch(w, r.Context(), data)
		return
	}
	resp, ok := s.handleOne(r.Context(), data)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	write(w, http.StatusOK, resp)
}

var null = json.RawMessage("null")

func (s *Server) batch(w http.ResponseWriter, ctx context.Context, data []byte) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil || len(items) == 0 {
		write(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: null, Error: &rpcError{codeParse, "parse error"}})
		return
	}
	out := []response{}
	for _, it := range items {
		if resp, ok := s.handleOne(ctx, it); ok {
			out = append(out, resp)
		}
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	write(w, http.StatusOK, out)
}

func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleOne answers a message; ok is false for a notification (a message without id).
func (s *Server) handleOne(ctx context.Context, data []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		return response{JSONRPC: "2.0", ID: null, Error: &rpcError{codeParse, "parse error"}}, true
	}
	if req.ID == nil {
		return response{}, false
	}
	resp := response{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || req.Method == "" {
		resp.Error = &rpcError{codeRequest, "invalid request"}
		return resp, true
	}
	res, rerr := s.dispatch(ctx, req)
	resp.Result, resp.Error = res, rerr
	return resp, true
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := versions[0]
		for _, known := range versions {
			if known == p.ProtocolVersion {
				v = known
			}
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": s.name, "version": s.version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools}, nil
	case "tools/call":
		return s.call(ctx, req.Params)
	}
	return nil, &rpcError{codeMethod, "method not found: " + req.Method}
}

func (s *Server) call(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{codeParams, "tools/call needs a name"}
	}
	i, ok := s.byName[p.Name]
	if !ok {
		return nil, &rpcError{codeParams, "unknown tool: " + p.Name}
	}
	if len(p.Arguments) == 0 {
		p.Arguments = json.RawMessage("{}")
	}
	res, err := s.tools[i].Call(ctx, p.Arguments)
	if err != nil {
		var bad *ArgError
		if errors.As(err, &bad) {
			return nil, &rpcError{codeParams, bad.Error()}
		}
		// A failure of the tool itself is the model's to read, not a protocol error.
		return Result{Content: []Content{Text(err.Error())}, IsError: true}, nil
	}
	if res.Content == nil {
		res.Content = []Content{}
	}
	return res, nil
}

// ArgError is a call whose arguments are wrong; the client gets a protocol error.
type ArgError struct{ Msg string }

func (e *ArgError) Error() string { return e.Msg }
