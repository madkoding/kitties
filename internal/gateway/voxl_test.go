package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// The H.264 live view: voxl's page behind the gateway's sign-in, reached through the container's
// own connection, with voxl's password added by the gateway.
func TestVoxlProxy(t *testing.T) {
	var seen []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, fmt.Sprintf("%s %s host=%s auth=%s cookie=%q", r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("Authorization"), r.Header.Get("Cookie")))
		if r.URL.Path == "/ws" {
			hj := w.(http.Hijacker)
			c, rw, _ := hj.Hijack()
			defer c.Close()
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = rw.Flush()
			line, _ := bufio.NewReader(c).ReadString('\n')
			_, _ = c.Write([]byte("echo " + line))
			return
		}
		_, _ = io.WriteString(w, "<html>voxl</html>")
	}))
	defer backend.Close()
	srv, f, _ := setup(t)
	f.voxl = func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", backend.Listener.Addr().String())
	}

	// No ticket without signing in, and none for a desktop without voxl.
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/voxl/ticket", "", nil); code != http.StatusUnauthorized {
		t.Errorf("ticket without token = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/zz/voxl/ticket", "tok", nil); code != http.StatusNotFound {
		t.Errorf("ticket of nobody = %d", code)
	}
	// Signed in with the gateway's token: a ticket, and with it the page, with the headers that let
	// the interface frame it.
	code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/voxl/ticket", "tok", nil)
	root, _ := got["path"].(string)
	if code != 200 || !strings.HasPrefix(root, "/v1/kitties/a1/voxl/t/") || !strings.HasSuffix(root, "/") {
		t.Fatalf("ticket = %d %v", code, got)
	}
	res, err := http.Get(srv.URL + root + "?stats")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "<html>voxl</html>" {
		t.Fatalf("page = %d %q", res.StatusCode, body)
	}
	if res.Header.Get("X-Frame-Options") != "" || res.Header.Get("Content-Security-Policy") != "frame-ancestors 'self' "+AppOrigin+" "+WailsOrigins {
		t.Errorf("framing headers = %v", res.Header)
	}
	// voxl gets its own password and address, never the gateway's token, and no cookie.
	want := `GET /?stats host=127.0.0.1:6901 auth=Bearer vpw cookie=""`
	if len(seen) != 1 || seen[0] != want {
		t.Errorf("voxl saw %q, want %q", seen, want)
	}
	// A wrong ticket, or a ticket of another kitty, opens nothing.
	f.kitties["bo"] = &kitty.Kitty{ID: "bo", Name: "bo"}
	for _, p := range []string{"/v1/kitties/ana/voxl/t/nope/", strings.Replace(root, "/a1/", "/bo/", 1), "/v1/kitties/ana/voxl/"} {
		if res, err := http.Get(srv.URL + p); err != nil || res.StatusCode == 200 {
			t.Errorf("%s opened: %v %v", p, res, err)
		}
	}
	// Only the viewer's own pages go through, not voxl's files, recordings or screenshots.
	for _, p := range []string{"files/list", "screenshot", "record", "stats"} {
		if res, err := http.Get(srv.URL + root + p); err != nil || res.StatusCode != http.StatusNotFound {
			t.Errorf("/%s = %v %v", p, res, err)
		}
	}
	if len(seen) != 1 {
		t.Errorf("voxl was reached for a refused path: %q", seen)
	}
	// The ticket's bare path goes to the one with a slash, so the page's relative links resolve.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := noFollow.Get(srv.URL + strings.TrimSuffix(root, "/")); err != nil || res.StatusCode != http.StatusMovedPermanently || !strings.HasSuffix(res.Header.Get("Location"), root) {
		t.Errorf("redirect = %v %v", res, err)
	}

	// The WebSocket upgrade goes through both ways.
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %sws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", root, strings.TrimPrefix(srv.URL, "http://"))
	r := bufio.NewReader(conn)
	if status, _ := r.ReadString('\n'); !strings.Contains(status, "101") {
		t.Fatalf("upgrade = %q", status)
	}
	for {
		if l, err := r.ReadString('\n'); err != nil || l == "\r\n" {
			break
		}
	}
	fmt.Fprint(conn, "ping\n")
	if got, _ := r.ReadString('\n'); got != "echo ping\n" {
		t.Errorf("through the upgrade: %q", got)
	}

	// A desktop without voxl gets no ticket (the page then follows /rects).
	f.voxl = nil
	srv.Config.Handler.(*Server).voxl = voxlCache{}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/voxl/ticket", "tok", nil); code != http.StatusNotFound {
		t.Errorf("no voxl = %d", code)
	}
}

// A desktop just turned on is running a few seconds before voxl listens: the page, its files and
// the WebSocket wait for it instead of failing at once; one that never answers gets a 503 that
// says so (a page that loads itself again for the frame, the JSON error for the rest).
func TestVoxlWaitsUntilUp(t *testing.T) {
	defer func(w, p time.Duration) { voxlUpWait, voxlUpPoll = w, p }(voxlUpWait, voxlUpPoll)
	voxlUpWait, voxlUpPoll = 200*time.Millisecond, time.Millisecond
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html>voxl</html>")
	}))
	defer backend.Close()
	srv, f, _ := setup(t)
	var mu sync.Mutex
	dials, upAfter := 0, 3
	f.voxl = func(ctx context.Context) (net.Conn, error) {
		mu.Lock()
		dials++
		up := dials > upAfter
		mu.Unlock()
		if !up {
			return nil, errors.New("connection refused")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", backend.Listener.Addr().String())
	}
	_, got, _ := call(t, srv, "POST", "/v1/kitties/ana/voxl/ticket", "tok", nil)
	root, _ := got["path"].(string)

	get := func(accept, lang string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", srv.URL+root, nil)
		req.Header.Set("Accept", accept)
		req.Header.Set("Accept-Language", lang)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, string(body)
	}
	// Not up for the first three tries, then the page.
	if res, body := get("text/html", ""); res.StatusCode != 200 || body != "<html>voxl</html>" || dials != 4 {
		t.Fatalf("after the start = %d %q, %d dials", res.StatusCode, body, dials)
	}

	// Never up: 503 and Retry-After.
	mu.Lock()
	dials, upAfter = 0, 1<<30
	mu.Unlock()
	srv.Config.Handler.(*Server).voxl.m = nil
	res, body := get("text/html,application/xhtml+xml", "es-CL,es;q=0.9,en;q=0.8")
	if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") != "3" ||
		!strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, notUpText["es"]) || !strings.Contains(body, `lang="es"`) {
		t.Errorf("frame = %d %v %q", res.StatusCode, res.Header, body)
	}
	if dials < 2 {
		t.Errorf("only %d dials before giving up", dials)
	}
	if _, body := get("text/html", "x,de-DE"); !strings.Contains(body, notUpText["en"]) {
		t.Errorf("unknown language = %q", body)
	}
	if _, body := get("text/html", "pt-BR"); !strings.Contains(body, notUpText["pt"]) {
		t.Errorf("portuguese = %q", body)
	}
	res, body = get("*/*", "")
	if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(res.Header.Get("Content-Type"), "json") || !strings.Contains(body, "still be starting") {
		t.Errorf("json = %d %q", res.StatusCode, body)
	}
}

// Only requests without a body are sent again, and a request that is gone stops the wait.
func TestRetryUntilUp(t *testing.T) {
	defer func(w, p time.Duration) { voxlUpWait, voxlUpPoll = w, p }(voxlUpWait, voxlUpPoll)
	voxlUpWait, voxlUpPoll = time.Second, time.Millisecond
	tries := 0
	rt := retryUntilUp{next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		tries++
		return nil, errors.New("refused")
	})}
	req, _ := http.NewRequest("POST", "http://x/", strings.NewReader("body"))
	if _, err := rt.RoundTrip(req); err == nil || tries != 1 {
		t.Errorf("with a body: %v after %d tries", err, tries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", "http://x/", nil)
	if _, err := rt.RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
