// Package gateway is the HTTP API of the fleet: the CLI, the browser page and other programs
// create kitties, open chats and send messages through it. Every request carries the bearer
// token; the page itself is the only thing served without one.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/provider"
)

// Fleet is what the gateway needs from the orchestrator.
type Fleet interface {
	List() []*kitty.Kitty
	Get(ref string) (*kitty.Kitty, error)
	Create(ctx context.Context, s orchestrator.Spec) (*kitty.Kitty, error)
	Start(ctx context.Context, ref string) error
	Stop(ctx context.Context, ref string) error
	Remove(ctx context.Context, ref string) error
	Scale(ctx context.Context, name string, n int) error
	Exec(ctx context.Context, ref string, argv ...string) (string, error)
	Screenshot(ctx context.Context, ref string) ([]byte, error)
	Providers() *provider.Registry
	Connectors() *connector.Registry
	HasContainers() (bool, error)
}

// Chats is what the gateway needs from the chat service.
type Chats interface {
	Create(title string, members []string) (*chat.Session, error)
	Get(id string) (*chat.Session, error)
	List() []*chat.Session
	Delete(id string) error
	AddMembers(id string, members []string) (*chat.Session, error)
	RemoveMember(id, ref string) (*chat.Session, error)
	Send(ctx context.Context, id, text string) ([]chat.Turn, error)
}

// Server is the HTTP API.
type Server struct {
	fleet   Fleet
	chats   Chats
	token   string
	version string
	started time.Time
	mux     *http.ServeMux
}

// NewToken makes a random bearer token.
func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// New builds the server. An empty token refuses every API call.
func New(fleet Fleet, chats Chats, token, version string) *Server {
	s := &Server{fleet: fleet, chats: chats, token: token, version: version, started: time.Now(), mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	api := s.auth
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.Handle("GET /v1/providers", api(s.handleProviders))
	s.mux.Handle("GET /v1/connectors", api(s.handleConnectors))
	s.mux.Handle("GET /v1/kitties", api(s.handleListKitties))
	s.mux.Handle("POST /v1/kitties", api(s.handleCreateKitty))
	s.mux.Handle("GET /v1/kitties/{ref}", api(s.handleGetKitty))
	s.mux.Handle("DELETE /v1/kitties/{ref}", api(s.handleRemoveKitty))
	s.mux.Handle("POST /v1/kitties/{ref}/start", api(s.handleStartKitty))
	s.mux.Handle("POST /v1/kitties/{ref}/stop", api(s.handleStopKitty))
	s.mux.Handle("POST /v1/kitties/{ref}/exec", api(s.handleExec))
	s.mux.Handle("GET /v1/kitties/{ref}/screenshot", api(s.handleScreenshot))
	s.mux.Handle("POST /v1/scale/{name}", api(s.handleScale))
	s.mux.Handle("GET /v1/chats", api(s.handleListChats))
	s.mux.Handle("POST /v1/chats", api(s.handleCreateChat))
	s.mux.Handle("GET /v1/chats/{id}", api(s.handleGetChat))
	s.mux.Handle("DELETE /v1/chats/{id}", api(s.handleDeleteChat))
	s.mux.Handle("POST /v1/chats/{id}/members", api(s.handleAddMembers))
	s.mux.Handle("DELETE /v1/chats/{id}/members/{ref}", api(s.handleRemoveMember))
	s.mux.Handle("POST /v1/chats/{id}/messages", api(s.handleSend))
}

// auth checks the bearer token (or the token query, for the page's screenshot links).
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == r.Header.Get("Authorization") {
			got = r.URL.Query().Get("token")
		}
		if s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			fail(w, http.StatusUnauthorized, errors.New("a valid bearer token is required"))
			return
		}
		next(w, r)
	})
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	reply(w, code, map[string]string{"error": err.Error()})
}

// codeFor maps an error to a status.
func codeFor(err error) int {
	msg := err.Error()
	switch {
	case errors.Is(err, orchestrator.ErrNotFound), strings.HasPrefix(msg, "no such "):
		return http.StatusNotFound
	case strings.Contains(msg, "already exists"), strings.Contains(msg, "not valid"), strings.Contains(msg, "unknown "),
		strings.Contains(msg, "cannot be"), strings.Contains(msg, "is empty"), strings.Contains(msg, "needs at least"),
		strings.Contains(msg, "not between"), strings.Contains(msg, "not a kitty"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func decode(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("the body is not valid JSON: %w", err)
	}
	return nil
}

// --- handlers --------------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	has, err := s.fleet.HasContainers()
	out := map[string]any{"ok": true, "version": s.version, "uptime": time.Since(s.started).Round(time.Second).String(),
		"kitties": len(s.fleet.List()), "containers": has}
	if err != nil {
		out["containers_error"] = err.Error()
	}
	reply(w, http.StatusOK, out)
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"providers": s.fleet.Providers().List(), "kinds": provider.Kinds()})
}

func (s *Server) handleConnectors(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"connectors": s.fleet.Connectors().Descriptions(nil)})
}

func (s *Server) handleListKitties(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"kitties": s.fleet.List()})
}

func (s *Server) handleCreateKitty(w http.ResponseWriter, r *http.Request) {
	var spec struct {
		orchestrator.Spec
		Start *bool `json:"start"`
	}
	if err := decode(r, &spec); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	k, err := s.fleet.Create(r.Context(), spec.Spec)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if spec.Start == nil || *spec.Start {
		if err := s.fleet.Start(r.Context(), k.ID); err != nil {
			k, _ = s.fleet.Get(k.ID)
			reply(w, http.StatusCreated, map[string]any{"kitty": k, "warning": "created but not started: " + err.Error()})
			return
		}
		k, _ = s.fleet.Get(k.ID)
	}
	reply(w, http.StatusCreated, map[string]any{"kitty": k})
}

func (s *Server) handleGetKitty(w http.ResponseWriter, r *http.Request) {
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"kitty": k})
}

func (s *Server) lifecycle(w http.ResponseWriter, r *http.Request, op func(context.Context, string) error) {
	ref := r.PathValue("ref")
	if err := op(r.Context(), ref); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	k, err := s.fleet.Get(ref)
	if err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	reply(w, http.StatusOK, map[string]any{"kitty": k})
}

func (s *Server) handleStartKitty(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, s.fleet.Start)
}
func (s *Server) handleStopKitty(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, s.fleet.Stop)
}
func (s *Server) handleRemoveKitty(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, s.fleet.Remove)
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.fleet.Exec(r.Context(), r.PathValue("ref"), body.Argv...)
	if err != nil {
		reply(w, codeFor(err), map[string]any{"error": err.Error(), "output": out})
		return
	}
	reply(w, http.StatusOK, map[string]any{"output": out})
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	png, err := s.fleet.Screenshot(r.Context(), r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) handleScale(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Replicas int `json:"replicas"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.Scale(r.Context(), r.PathValue("name"), body.Replicas); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"kitties": s.fleet.List()})
}

func (s *Server) handleListChats(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"chats": s.chats.List()})
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title   string   `json:"title"`
		Members []string `json:"members"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	sess, err := s.chats.Create(body.Title, body.Members)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"chat": sess})
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	sess, err := s.chats.Get(r.PathValue("id"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"chat": sess})
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	if err := s.chats.Delete(r.PathValue("id")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAddMembers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Members []string `json:"members"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	sess, err := s.chats.AddMembers(r.PathValue("id"), body.Members)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"chat": sess})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	sess, err := s.chats.RemoveMember(r.PathValue("id"), r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"chat": sess})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	turns, err := s.chats.Send(r.Context(), r.PathValue("id"), body.Text)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"turns": turns})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

// Listen serves on addr until ctx ends, warning when the address is not loopback.
func (s *Server) Listen(ctx context.Context, addr string, logf func(string, ...any)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	if host, _, _ := net.SplitHostPort(addr); host != "127.0.0.1" && host != "localhost" && host != "::1" {
		logf("warning: the gateway listens on %s, reachable from the network; the token is its only protection", addr)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logf("gateway listening on http://%s", ln.Addr())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
