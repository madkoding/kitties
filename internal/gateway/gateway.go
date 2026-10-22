// Package gateway is the HTTP API of the fleet: the CLI, the browser page and other programs
// create kitties, open chats and send messages through it. Every request carries the bearer
// token; the page itself is the only thing served without one.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/oauth"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/skills"
)

// Fleet is what the gateway needs from the orchestrator.
type Fleet interface {
	List() []*kitty.Kitty
	Get(ref string) (*kitty.Kitty, error)
	Create(ctx context.Context, s orchestrator.Spec) (*kitty.Kitty, error)
	Edit(ref string, u orchestrator.Update) (*kitty.Kitty, error)
	Start(ctx context.Context, ref string) error
	Stop(ctx context.Context, ref string) error
	Remove(ctx context.Context, ref string) error
	Scale(ctx context.Context, name string, n int) error
	Exec(ctx context.Context, ref string, argv ...string) (string, error)
	Screenshot(ctx context.Context, ref string) ([]byte, error)
	Providers() *provider.Registry
	Connectors() *connector.Registry
	HasContainers() (bool, error)
	Settings() orchestrator.SettingsView
	SetProvider(name string, p config.Provider) error
	RemoveProvider(name string) error
	SetDefaultProvider(name string) error
	TestProvider(ctx context.Context, name string) (string, error)
	SetConnector(name string, c config.Connector) error
	RemoveConnector(name string) error
	OAuth() *oauth.Manager
	// InboundSecret is the secret a service presents to post events to a connector.
	InboundSecret(name string) (string, error)
	// Notify is where the person is told outside the interface; SetNotify changes it.
	Notify() notify.Config
	SetNotify(c notify.Config) error
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
	Cancel(id string) bool
	Shot(id, name string) ([]byte, error)
	Subscribe(id string) (<-chan chat.Event, func())
}

// Server is the HTTP API.
type Server struct {
	fleet   Fleet
	chats   Chats
	token   string
	version string
	started time.Time
	mux     *http.ServeMux
	skills  *skills.Library
	arts    *artifacts.Store

	// The web server side (serve.go).
	web          fs.FS
	publicURL    string
	trustProxy   bool
	noLocalToken bool
	tlsCert      string
	tlsKey       string
	limit        *limiter
	// agenda is the goals and journals, nil when the deployment has none.
	agenda Agenda
	// approver holds actions for the person's yes, nil when none are held.
	approver Approver
	// memories is what the kitties know about the person, nil when they keep none.
	memories Memories
	// notifier tests notifications, nil when the gateway sends none.
	notifier NotifyTester
}

// WithLibrary serves the skills and the artifacts.
func (s *Server) WithLibrary(sk *skills.Library, arts *artifacts.Store) *Server {
	s.skills, s.arts = sk, arts
	return s
}

// NewToken makes a random bearer token.
func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// New builds the server. An empty token refuses every API call.
func New(fleet Fleet, chats Chats, token, version string) *Server {
	s := &Server{fleet: fleet, chats: chats, token: token, version: version, started: time.Now(), mux: http.NewServeMux(), limit: newLimiter(10, time.Minute)}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.secure(w, r)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	api := s.auth
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/session", s.handleSession)
	s.mux.HandleFunc("POST /v1/login", s.handleLogin)
	s.mux.HandleFunc("POST /v1/logout", s.handleLogout)
	s.mux.Handle("GET /v1/providers", api(s.handleProviders))
	s.mux.Handle("GET /v1/connectors", api(s.handleConnectors))
	s.mux.Handle("GET /v1/kitties", api(s.handleListKitties))
	s.mux.Handle("POST /v1/kitties", api(s.handleCreateKitty))
	s.mux.Handle("GET /v1/kitties/{ref}", api(s.handleGetKitty))
	s.mux.Handle("DELETE /v1/kitties/{ref}", api(s.handleRemoveKitty))
	s.mux.Handle("PATCH /v1/kitties/{ref}", api(s.handleEditKitty))
	s.mux.Handle("GET /v1/avatars", api(s.handleAvatars))
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
	s.mux.Handle("POST /v1/chats/{id}/cancel", api(s.handleCancel))
	s.mux.Handle("GET /v1/chats/{id}/shots/{name}", api(s.handleShot))
	s.mux.Handle("GET /v1/settings", api(s.handleSettings))
	s.mux.Handle("PUT /v1/settings/providers/{name}", api(s.handleSetProvider))
	s.mux.Handle("DELETE /v1/settings/providers/{name}", api(s.handleRemoveProvider))
	s.mux.Handle("POST /v1/settings/providers/{name}/test", api(s.handleTestProvider))
	s.mux.Handle("PUT /v1/settings/default-provider", api(s.handleDefaultProvider))
	s.mux.Handle("PUT /v1/settings/connectors/{name}", api(s.handleSetConnector))
	s.mux.Handle("DELETE /v1/settings/connectors/{name}", api(s.handleRemoveConnector))
	s.mux.HandleFunc("GET /v1/local-token", s.handleLocalToken)
	s.agendaRoutes()
	s.approvalRoutes()
	s.memoryRoutes()
	s.mux.Handle("GET /v1/chats/{id}/events", api(s.handleEvents))
	s.mux.Handle("POST /v1/oauth/{name}/start", api(s.handleOAuthStart))
	s.mux.HandleFunc("GET /v1/oauth/callback", s.handleOAuthCallback)
	s.mux.Handle("GET /v1/skills", api(s.handleSkills))
	s.mux.Handle("GET /v1/skills/{name}", api(s.handleSkill))
	s.mux.Handle("PUT /v1/skills/{name}", api(s.handleSaveSkill))
	s.mux.Handle("DELETE /v1/skills/{name}", api(s.handleDeleteSkill))
	s.mux.Handle("POST /v1/skills/{name}/accept", api(s.handleAcceptSkill))
	s.mux.Handle("GET /v1/artifacts", api(s.handleArtifacts))
	s.mux.Handle("GET /v1/artifacts/{name}", api(s.handleArtifact))
	s.mux.Handle("PUT /v1/artifacts/{name}", api(s.handlePutArtifact))
	s.mux.Handle("DELETE /v1/artifacts/{name}", api(s.handleDeleteArtifact))
}

// handleEvents streams a chat's turns as they come (server-sent events): one event per new
// turn, per step of a desktop task and when a turn finishes. A heartbeat keeps proxies awake.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.chats.Get(id); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, errors.New("streaming is not supported here"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()
	events, cancel := s.chats.Subscribe(id)
	defer cancel()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case ev := <-events:
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
			if ev.Type == "deleted" {
				return
			}
		}
	}
}

var heartbeat = 20 * time.Second

// handleOAuthStart begins a connector's sign-in and returns the consent URL for the page to open.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	redirect := s.publicURL + "/v1/oauth/callback"
	if s.publicURL == "" {
		scheme := "http"
		if s.isHTTPS(r) {
			scheme = "https"
		}
		redirect = scheme + "://" + s.requestHost(r) + "/v1/oauth/callback"
	}
	link, err := s.fleet.OAuth().Start(r.PathValue("name"), redirect)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"url": link, "redirect": redirect})
}

// handleOAuthCallback is where the identity service sends the browser back; it is open, since
// the browser carries no token, and the state ties it to a flow this gateway started.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := q.Get("error"); e != "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, oauth.CallbackHTML, "Sign-in refused", html.EscapeString(e+" "+q.Get("error_description")))
		return
	}
	name, err := s.fleet.OAuth().Exchange(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, oauth.CallbackHTML, "Sign-in failed", html.EscapeString(err.Error()))
		return
	}
	_, _ = fmt.Fprintf(w, oauth.CallbackHTML, "Connected", "The connector "+html.EscapeString(name)+" is signed in. You can close this window.")
}

func (s *Server) noLibrary(w http.ResponseWriter) bool {
	if s.skills == nil || s.arts == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway has no library"))
		return true
	}
	return false
}

func (s *Server) handleSkills(w http.ResponseWriter, _ *http.Request) {
	if s.noLibrary(w) {
		return
	}
	list, err := s.skills.List()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"skills": list})
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	sk, err := s.skills.Get(r.PathValue("name"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"skill": sk})
}

func (s *Server) handleSaveSkill(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	sk, err := s.skills.Save(r.PathValue("name"), body.Body)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"skill": sk})
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	if err := s.skills.Delete(r.PathValue("name")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAcceptSkill(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	sk, err := s.skills.Accept(r.PathValue("name"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"skill": sk})
}

func (s *Server) handleArtifacts(w http.ResponseWriter, _ *http.Request) {
	if s.noLibrary(w) {
		return
	}
	list, err := s.arts.List()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"artifacts": list})
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	name := r.PathValue("name")
	data, err := s.arts.Get(name)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	w.Header().Set("Content-Type", artifacts.Type(name))
	w.Header().Set("Content-Disposition", "inline; filename=\""+name+"\"")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handlePutArtifact(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 9<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	a, err := s.arts.Put(r.PathValue("name"), data, "")
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"artifact": a})
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	if s.noLibrary(w) {
		return
	}
	if err := s.arts.Delete(r.PathValue("name")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLocalToken hands the token to a client on this machine, so a local executable or the
// gateway's own page needs no typing (see localTokenOK for who counts as local). A server
// never does: it turns this off and people sign in with the token.
func (s *Server) handleLocalToken(w http.ResponseWriter, r *http.Request) {
	if err := s.localTokenOK(r); err != nil {
		code := http.StatusForbidden
		if err == errNoToken {
			code = http.StatusNotFound
		}
		fail(w, code, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, http.StatusOK, map[string]string{"token": s.token})
}

// sameOrigin reports whether origin names the host this request arrived on.
func sameOrigin(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return strings.EqualFold(o, host)
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
	case errors.Is(err, chat.ErrBusy), strings.Contains(msg, "already running"), strings.Contains(msg, "is already approved"), strings.Contains(msg, "is already rejected"):
		return http.StatusConflict
	case errors.Is(err, chat.ErrNoApprovals), errors.Is(err, chat.ErrNoMemory):
		return http.StatusNotFound
	case strings.Contains(msg, "configuration file"):
		return http.StatusForbidden
	case strings.Contains(msg, "already exists"), strings.Contains(msg, "not valid"), strings.Contains(msg, "unknown "),
		strings.Contains(msg, "cannot be"), strings.Contains(msg, "is empty"), strings.Contains(msg, "needs at least"),
		strings.Contains(msg, "not between"), strings.Contains(msg, "not a kitty"), strings.Contains(msg, "is not one of"),
		strings.Contains(msg, "needs a"), strings.Contains(msg, "profile name"), strings.Contains(msg, "connector name"),
		strings.Contains(msg, "clear text"), strings.Contains(msg, ".kind:"), strings.HasPrefix(msg, "avatar."),
		strings.Contains(msg, "too big"), strings.Contains(msg, "too long"), strings.Contains(msg, "is empty"), strings.Contains(msg, "needs a name"),
		strings.Contains(msg, "built in"), strings.Contains(msg, "holds"), strings.Contains(msg, " needs "), strings.Contains(msg, "not a valid"),
		strings.Contains(msg, "does not sign in"), strings.Contains(msg, "no OAuth flow"), strings.Contains(msg, "not signed in"),
		strings.HasPrefix(msg, "schedule:"), strings.HasPrefix(msg, "on:"), strings.Contains(msg, "a goal "), strings.Contains(msg, "back itself up"),
		strings.Contains(msg, "limits"), strings.Contains(msg, "not ask or auto"), strings.Contains(msg, "can be for good"), strings.Contains(msg, "always asked"),
		strings.HasPrefix(msg, "notify."), strings.Contains(msg, "rating is"), strings.Contains(msg, "feedback is for"), strings.Contains(msg, "demand is a level"),
		strings.Contains(msg, "a note needs"), strings.HasPrefix(msg, "scope "), strings.Contains(msg, "the turn is"):
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

func (s *Server) handleAvatars(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"breeds": kitty.Breeds, "colors": kitty.Colors})
}

func (s *Server) handleEditKitty(w http.ResponseWriter, r *http.Request) {
	var u orchestrator.Update
	if err := decode(r, &u); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	k, err := s.fleet.Edit(r.PathValue("ref"), u)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"kitty": k})
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
	id := ""
	if k, err := s.fleet.Get(r.PathValue("ref")); err == nil {
		id = k.ID
	}
	s.lifecycle(w, r, func(ctx context.Context, ref string) error {
		if err := s.fleet.Remove(ctx, ref); err != nil {
			return err
		}
		// Its goals and journal go with it.
		if f, ok := s.agenda.(Forgetter); ok && id != "" {
			_ = f.Forget(id)
		}
		return nil
	})
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

func (s *Server) handleSettings(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleSetProvider(w http.ResponseWriter, r *http.Request) {
	var p config.Provider
	if err := decode(r, &p); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.SetProvider(r.PathValue("name"), p); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleRemoveProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.fleet.RemoveProvider(r.PathValue("name")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	answer, err := s.fleet.TestProvider(ctx, r.PathValue("name"))
	if err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "reply": answer})
}

func (s *Server) handleDefaultProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.SetDefaultProvider(body.Name); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleSetConnector(w http.ResponseWriter, r *http.Request) {
	var c config.Connector
	if err := decode(r, &c); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.SetConnector(r.PathValue("name"), c); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleRemoveConnector(w http.ResponseWriter, r *http.Request) {
	if err := s.fleet.RemoveConnector(r.PathValue("name")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, s.fleet.Settings())
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{"cancelled": s.chats.Cancel(r.PathValue("id"))})
}

func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
	png, err := s.chats.Shot(r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(png)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if s.web != nil {
		s.handleStatic(w, r)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

// Listen serves on addr until ctx ends (https when a certificate was given). It says where the
// page is, and warns when the token would cross a network in the clear.
func (s *Server) Listen(ctx context.Context, addr string, logf func(string, ...any)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	tls := s.tlsCert != ""
	if !config.IsLoopbackAddr(addr) && !tls && !s.trustProxy && !strings.HasPrefix(s.publicURL, "https://") {
		logf("warning: the gateway listens on %s without https; the token and the chats cross the network in the clear. Set gateway.tls_cert and tls_key, or put it behind a TLS proxy (trust_proxy)", addr)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	scheme := "http"
	if tls {
		scheme = "https"
	}
	where := s.publicURL
	if where == "" {
		where = scheme + "://" + ln.Addr().String()
	}
	what := "gateway"
	if s.web != nil {
		what = "gateway and web interface"
	}
	logf("%s listening on %s", what, where)
	if tls {
		err = srv.ServeTLS(ln, s.tlsCert, s.tlsKey)
	} else {
		err = srv.Serve(ln)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
