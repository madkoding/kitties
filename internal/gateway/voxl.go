package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
// WebCodecs, 60 pictures a second and a few milliseconds behind. The container publishes no
// port, so the gateway is its reverse proxy: /v1/kitties/{ref}/voxl/ is voxl's own page, behind
// the gateway's sign-in, and every connection to it is a `docker exec ... socat` into the
// container's loopback. voxl's own password is the container's secret: it is added here, and the
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
		Transport: &http.Transport{
			DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
			IdleConnTimeout:     10 * time.Second,
			MaxIdleConnsPerHost: 2,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.voxl.mu.Lock()
			delete(s.voxl.m, r.PathValue("ref"))
			s.voxl.mu.Unlock()
			fail(w, http.StatusBadGateway, errors.New("the live view did not answer"))
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
	// The interface frames it: only itself (and the Android app's page) may. Every response
	// carries X-Frame-Options: DENY; frame-ancestors says who may instead, and replaces it.
	h := w.Header()
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", "frame-ancestors 'self' "+AppOrigin)
	h.Set("Cache-Control", "no-store")
	proxy.ServeHTTP(w, r)
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
