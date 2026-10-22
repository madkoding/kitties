package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
)

// Agenda is what the gateway needs from the scheduler and its stores: the goals, running one
// now, firing events at them, and reading a kitty's journal.
type Agenda interface {
	Goals(kitty string) []*agenda.Goal
	Goal(id string) (*agenda.Goal, error)
	AddGoal(g agenda.Goal) (*agenda.Goal, error)
	UpdateGoal(id string, g agenda.Goal) (*agenda.Goal, error)
	RemoveGoal(id string) error
	RunNow(ctx context.Context, id string) (string, error)
	Fire(ctx context.Context, ev agenda.Event) []string
	Recent(kitty string, n int) []agenda.Entry
}

// Forgetter is an agenda that drops a removed kitty's goals and journal.
type Forgetter interface {
	Forget(kitty string) error
}

// WithAgenda serves the goals, the journals and the incoming hooks.
func (s *Server) WithAgenda(a Agenda) *Server {
	s.agenda = a
	return s
}

// agendaRoutes are registered when the server has an agenda; without one they answer 404.
func (s *Server) agendaRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/goals", api(s.handleGoals))
	s.mux.Handle("POST /v1/goals", api(s.handleAddGoal))
	s.mux.Handle("GET /v1/goals/{id}", api(s.handleGoal))
	s.mux.Handle("PUT /v1/goals/{id}", api(s.handleUpdateGoal))
	s.mux.Handle("DELETE /v1/goals/{id}", api(s.handleRemoveGoal))
	s.mux.Handle("POST /v1/goals/{id}/run", api(s.handleRunGoal))
	s.mux.Handle("GET /v1/kitties/{ref}/journal", api(s.handleJournal))
	s.mux.HandleFunc("POST /v1/hooks/{connector}", s.handleHook)
}

var errNoAgenda = errors.New("this gateway has no agenda (goals and journals are off)")

var errNoApprovals = errors.New("this gateway holds no actions for approval")

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	kitty := ""
	if ref := r.URL.Query().Get("kitty"); ref != "" {
		k, err := s.fleet.Get(ref)
		if err != nil {
			fail(w, codeFor(err), err)
			return
		}
		kitty = k.ID
	}
	goals := s.agenda.Goals(kitty)
	if goals == nil {
		goals = []*agenda.Goal{}
	}
	reply(w, http.StatusOK, map[string]any{"goals": goals})
}

// goalBody is what the page sends for a goal; the kitty may be a name or an id.
type goalBody struct {
	Kitty   string   `json:"kitty"`
	Title   string   `json:"title"`
	Text    string   `json:"text"`
	Every   string   `json:"every"`
	On      []string `json:"on"`
	Enabled *bool    `json:"enabled"`
}

func (b goalBody) goal() agenda.Goal {
	g := agenda.Goal{Kitty: b.Kitty, Title: b.Title, Text: b.Text, Every: b.Every, On: b.On, Enabled: true}
	if b.Enabled != nil {
		g.Enabled = *b.Enabled
	}
	return g
}

func (s *Server) handleAddGoal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	var body goalBody
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	k, err := s.fleet.Get(body.Kitty)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	g := body.goal()
	g.Kitty = k.ID
	if err := s.checkTriggers(g); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.agenda.AddGoal(g)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"goal": saved})
}

// checkTriggers makes sure a goal listens to connectors that exist.
func (s *Server) checkTriggers(g agenda.Goal) error {
	for _, name := range g.On {
		if _, ok := s.fleet.Connectors().Get(name); !ok {
			return errors.New("on: unknown connector " + strconv.Quote(name))
		}
	}
	return nil
}

func (s *Server) handleGoal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	g, err := s.agenda.Goal(r.PathValue("id"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"goal": g})
}

func (s *Server) handleUpdateGoal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	var body goalBody
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	g := body.goal()
	if err := s.checkTriggers(g); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.agenda.UpdateGoal(r.PathValue("id"), g)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"goal": saved})
}

func (s *Server) handleRemoveGoal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	if err := s.agenda.RemoveGoal(r.PathValue("id")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRunGoal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	chatID, err := s.agenda.RunNow(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "chat": chatID})
}

func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	n := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && v > 0 && v <= 500 {
		n = v
	}
	entries := s.agenda.Recent(k.ID, n)
	if entries == nil {
		entries = []agenda.Entry{}
	}
	reply(w, http.StatusOK, map[string]any{"journal": entries})
}

// maxHookBody bounds what a service may post.
const maxHookBody = 1 << 20

// handleHook receives an event a service posts for a connector (GitHub's webhooks, a Slack
// app, anything that can POST). The connector's inbound_secret must be presented: as GitHub's
// HMAC signature of the body, as the X-Kitties-Secret header, or as ?secret=. The goals that
// listen to the connector run with the event.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	if s.agenda == nil {
		fail(w, http.StatusNotFound, errNoAgenda)
		return
	}
	ip := s.clientIP(r)
	if wait := s.limit.wait(ip); wait > 0 {
		tooMany(w, wait)
		return
	}
	name := r.PathValue("connector")
	secret, err := s.fleet.InboundSecret(name)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if secret == "" {
		fail(w, http.StatusForbidden, errors.New("this connector has no incoming events secret; set inbound_secret in its settings"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody+1))
	if err != nil || len(body) > maxHookBody {
		fail(w, http.StatusRequestEntityTooLarge, errors.New("the event is too big (1 MB at most)"))
		return
	}
	if !hookAllowed(r, body, secret) {
		s.limit.fail(ip)
		fail(w, http.StatusForbidden, errors.New("the secret does not match"))
		return
	}
	kind := r.Header.Get("X-GitHub-Event")
	if kind == "" {
		kind = r.Header.Get("X-Kitties-Event")
	}
	ev := agenda.Summarise(name, kind, body)
	goals := s.agenda.Fire(r.Context(), ev)
	if goals == nil {
		goals = []string{}
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "goals": goals, "event": ev})
}

// hookAllowed checks the secret a service presented, in whichever of the three ways.
func hookAllowed(r *http.Request, body []byte, secret string) bool {
	if sig := r.Header.Get("Stripe-Signature"); sig != "" {
		return stripeSigned(sig, body, secret, time.Now())
	}
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(strings.ToLower(sig)), []byte(want))
	}
	got := r.Header.Get("X-Kitties-Secret")
	if got == "" {
		got = r.URL.Query().Get("secret")
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

// stripeSigned checks Stripe's signature: t=<unix>,v1=<hmac of "t.body">, no older than five minutes.
func stripeSigned(header string, body []byte, secret string, now time.Time) bool {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return false
	}
	if d := now.Sub(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, sig := range sigs {
		if hmac.Equal([]byte(sig), []byte(want)) {
			return true
		}
	}
	return false
}
