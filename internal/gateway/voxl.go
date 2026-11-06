package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/orchestrator"
)

// THE LIVE VIEW, H.264.
//
// Each kitty's desktop runs voxl (https://github.com/tembo/voxl, MIT; the image builds it with
// x264, so the image is GPL): the display as H.264 over a WebSocket, decoded in the page with
// WebCodecs, 60 pictures a second and a few milliseconds behind. The container publishes it only
// on the host's loopback, so the gateway is its reverse proxy: /v1/kitties/{ref}/voxl/ is voxl's
// own page, behind the gateway's sign-in, and every connection to it is dialled by Tunnel (the
// published port, else the container's address, else `docker exec ... socat`). voxl's own password is the container's secret: it is added here, and the
// person never sees it.
//
// Only what the viewer needs goes through (its page, scripts, font and the WebSocket): not voxl's
// file transfer, recordings or screenshots, which the gateway has its own, authorised ways for.
// A desktop whose image has no voxl answers 404 and the page falls back to /rects.

func (s *Server) voxlRoutes() {
	// The person signs in once for a ticket; the frame then loads voxl's page, scripts and
	// WebSocket from a path that carries it (they cannot carry the gateway's token or cookie:
	// the Android app's page is another origin, and the page's own links are relative).
	s.mux.Handle("POST /v1/kitties/{ref}/voxl/ticket", s.auth(s.handleVoxlTicket))
	s.mux.HandleFunc("GET /v1/kitties/{ref}/voxl/t/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
	})
	s.mux.HandleFunc("GET /v1/kitties/{ref}/voxl/t/{ticket}/{path...}", s.handleVoxl)
}

// voxlTicketIdle is how long a ticket lives without being used, and voxlTicketMax how long at most.
const (
	voxlTicketIdle = 15 * time.Minute
	voxlTicketMax  = 12 * time.Hour
)

type voxlTicket struct {
	ref           string
	made, lastUse time.Time
}

func (s *Server) handleVoxlTicket(w http.ResponseWriter, r *http.Request) {
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	// No voxl here (an older image, the demo, a machine that is off): the page follows /rects.
	if _, err := s.voxlFor(r.Context(), k.ID); err != nil {
		fail(w, http.StatusNotFound, errors.New("this desktop has no H.264 live view"))
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	ticket := hex.EncodeToString(raw)
	now := time.Now()
	s.voxl.mu.Lock()
	if s.voxl.tickets == nil {
		s.voxl.tickets = map[string]*voxlTicket{}
	}
	for t, v := range s.voxl.tickets {
		if now.Sub(v.lastUse) > voxlTicketIdle || now.Sub(v.made) > voxlTicketMax {
			delete(s.voxl.tickets, t)
		}
	}
	s.voxl.tickets[ticket] = &voxlTicket{ref: k.ID, made: now, lastUse: now}
	s.voxl.mu.Unlock()
	reply(w, http.StatusOK, map[string]string{"path": "/v1/kitties/" + k.ID + "/voxl/t/" + ticket + "/"})
}

// ticketRef is the kitty a ticket opens, or "" when it is unknown or has run out.
func (s *Server) ticketRef(ticket string) string {
	now := time.Now()
	s.voxl.mu.Lock()
	defer s.voxl.mu.Unlock()
	v := s.voxl.tickets[ticket]
	if v == nil {
		return ""
	}
	if now.Sub(v.lastUse) > voxlTicketIdle || now.Sub(v.made) > voxlTicketMax {
		delete(s.voxl.tickets, ticket)
		return ""
	}
	v.lastUse = now
	return v.ref
}

// voxlPaths are the only paths of voxl that go through.
var voxlPaths = map[string]bool{
	"": true, "index.html": true, "client.js": true, "worker.js": true, "audio.js": true, "mic.js": true,
	"favicon.ico": true, "inter.woff2": true, "healthz": true, "ws": true,
}

// voxlHost is the address voxl believes it has: it only answers for an IP address or localhost.
const voxlHost = "127.0.0.1:6901"

// voxlTTL is how long a kitty's voxl password is remembered.
const voxlTTL = 30 * time.Second

type voxlEntry struct {
	link    orchestrator.Voxl
	proxy   *httputil.ReverseProxy
	expires time.Time
}

// voxlCache holds the proxies by kitty, so a page's dozen requests cost one look at the password.
type voxlCache struct {
	mu      sync.Mutex
	m       map[string]*voxlEntry
	tickets map[string]*voxlTicket
}

func (s *Server) voxlFor(ctx context.Context, ref string) (*httputil.ReverseProxy, error) {
	s.voxl.mu.Lock()
	defer s.voxl.mu.Unlock()
	if e := s.voxl.m[ref]; e != nil && time.Now().Before(e.expires) {
		return e.proxy, nil
	}
	link, err := s.fleet.Voxl(ctx, ref)
	if err != nil {
		delete(s.voxl.m, ref)
		return nil, err
	}
	dial := link.Dial
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme = "http"
			out.URL.Host = voxlHost
			out.URL.Path = "/" + pr.In.PathValue("path")
			out.URL.RawPath = ""
			// The gateway's token (or voxl's own, in the page's links) is not voxl's business.
			out.URL.RawQuery = withoutToken(pr.In.URL.RawQuery)
			out.Host = voxlHost
			out.Header.Del("Cookie")
			out.Header.Del("Authorization")
			out.Header.Set("Authorization", "Bearer "+link.Password)
			if out.Header.Get("Origin") != "" {
				out.Header.Set("Origin", "http://"+voxlHost)
			}
		},
		Transport: retryUntilUp{next: &http.Transport{
			DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
			IdleConnTimeout:     10 * time.Second,
			MaxIdleConnsPerHost: 2,
		}},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.voxl.mu.Lock()
			delete(s.voxl.m, r.PathValue("ref"))
			s.voxl.mu.Unlock()
			notUpYet(w, r)
		},
	}
	if s.voxl.m == nil {
		s.voxl.m = map[string]*voxlEntry{}
	}
	s.voxl.m[ref] = &voxlEntry{link: link, proxy: proxy, expires: time.Now().Add(voxlTTL)}
	return proxy, nil
}

func (s *Server) handleVoxl(w http.ResponseWriter, r *http.Request) {
	ref := s.ticketRef(r.PathValue("ticket"))
	if ref == "" || ref != r.PathValue("ref") {
		fail(w, http.StatusUnauthorized, errors.New("this live view link has run out; open the desktop again"))
		return
	}
	if !voxlPaths[r.PathValue("path")] {
		fail(w, http.StatusNotFound, errors.New("not a page of the live view"))
		return
	}
	proxy, err := s.voxlFor(r.Context(), ref)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("this desktop has no H.264 live view"))
		return
	}
	// The interface frames it: only itself (and the Android and desktop apps' pages) may. Every response
	// carries X-Frame-Options: DENY; frame-ancestors says who may instead, and replaces it.
	h := w.Header()
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", "frame-ancestors 'self' "+AppOrigin+" "+WailsOrigins)
	h.Set("Cache-Control", "no-store")
	proxy.ServeHTTP(w, r)
}

// A desktop just turned on is marked running as soon as its container is, a few seconds before
// voxl listens: a request in between would find nobody. It is tried again until voxl answers or
// voxlUpWait passes, every voxlUpPoll.
var (
	voxlUpWait = 20 * time.Second
	voxlUpPoll = 500 * time.Millisecond
)

// retryUntilUp sends a request again while voxl cannot be reached. Only requests without a body
// (the page, its files, the WebSocket's upgrade) are sent again: nothing of them was taken yet.
type retryUntilUp struct{ next http.RoundTripper }

func (t retryUntilUp) RoundTrip(r *http.Request) (*http.Response, error) {
	deadline := time.Now().Add(voxlUpWait)
	for {
		res, err := t.next.RoundTrip(r)
		if err == nil || (r.Body != nil && r.Body != http.NoBody) || time.Now().Add(voxlUpPoll).After(deadline) {
			return res, err
		}
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(voxlUpPoll):
		}
	}
}

// notUpYet answers for a voxl that did not answer in time: 503 with Retry-After, so the page
// and the apps know to try again. The frame (a page asked for as HTML) gets a page that says so
// in the person's language and loads itself again; everything else, the JSON error.
func notUpYet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "3")
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		fail(w, http.StatusServiceUnavailable, errors.New("the live view is not answering yet: the desktop may still be starting, try again in a few seconds"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, `<!doctype html><html lang="`+notUpLang(r)+`"><head><meta charset="utf-8"><meta http-equiv="refresh" content="3">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1"><title>Kitty desktop</title>`+
		`<style>body{margin:0;height:100vh;display:grid;place-items:center;background:#111;color:#ccc;font:14px system-ui,sans-serif;text-align:center}</style>`+
		`</head><body><p>`+notUpText[notUpLang(r)]+`</p></body></html>`)
}

// notUpText is what the waiting page says, by language.
var notUpText = map[string]string{
	"en": "The desktop is still starting. Trying again…",
	"es": "El escritorio aún está arrancando. Reintentando…",
	"pt": "A área de trabalho ainda está iniciando. Tentando de novo…",
}

// notUpLang is the first of the person's languages the waiting page has, else English.
func notUpLang(r *http.Request) string {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if _, ok := notUpText[tag[:min(2, len(tag))]]; ok && len(tag) >= 2 {
			return tag[:2]
		}
	}
	return "en"
}

// withoutToken is a query string without its token parameter, the rest untouched.
func withoutToken(raw string) string {
	var keep []string
	for _, kv := range strings.Split(raw, "&") {
		if kv != "" && kv != "token" && !strings.HasPrefix(kv, "token=") {
			keep = append(keep, kv)
		}
	}
	return strings.Join(keep, "&")
}
