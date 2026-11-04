package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
