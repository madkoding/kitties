package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The display's pictures reach the page as data: URLs; a refused stream says why.
func TestWatchDesktop(t *testing.T) {
	var mu sync.Mutex
	var events []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/kitties/gone/stream" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=kf")
		_, _ = w.Write([]byte("--kf\r\nContent-Type: image/jpeg\r\n\r\nabc\r\n--kf\r\nContent-Type: image/jpeg\r\n\r\ndef\r\n--kf--\r\n"))
	}))
	defer srv.Close()
	a := NewApp()
	a.emit = func(name string, data any) {
		mu.Lock()
		defer mu.Unlock()
		m := data.(map[string]string)
		m["name"] = name
		events = append(events, m)
	}
	a.startup(t.Context())
	a.conn.URL, a.conn.Token = srv.URL, "tok"
	if err := a.WatchDesktop("a/b"); err == nil {
		t.Error("a bad ref is refused")
	}
	for _, ref := range []string{"tom", "gone"} {
		if err := a.WatchDesktop(ref); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(ok func() bool) {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			done := ok()
			mu.Unlock()
			if done {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	has := func(name, key, prefix string) bool {
		for _, e := range events {
			if e["name"] == name && strings.HasPrefix(e[key], prefix) {
				return true
			}
		}
		return false
	}
	wait(func() bool {
		return has("kitties:desktop:tom", "frame", "data:image/jpeg;base64,ZGVm") && has("kitties:desktop:gone", "error", "")
	})
	a.UnwatchDesktop("tom")
	a.UnwatchDesktop("gone")
	mu.Lock()
	defer mu.Unlock()
	if !has("kitties:desktop:tom", "frame", "data:image/jpeg;base64,YWJj") || !has("kitties:desktop:tom", "frame", "data:image/jpeg;base64,ZGVm") {
		t.Errorf("frames = %v", events)
	}
	if !has("kitties:desktop:gone", "error", "the gateway answered HTTP 404") {
		t.Errorf("errors = %v", events)
	}
}

// The display as what changed: the size, then rectangles as data: URLs; a gateway without the
// stream, or one that talks nonsense, is said on the event.
func TestWatchRects(t *testing.T) {
	var mu sync.Mutex
	var events []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kitties/old/rects":
			w.WriteHeader(404)
		case "/v1/kitties/odd/rects":
			_, _ = w.Write([]byte("HELLO\n"))
		case "/v1/kitties/bad/rects":
			_, _ = w.Write([]byte("UPDATE 1\nBOX 1 2 3 4 5\n"))
		case "/v1/kitties/huge/rects":
			_, _ = w.Write([]byte("UPDATE 5000\n"))
		default:
			if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("fps") != "20" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte("SCREEN 1280 800\nUPDATE 2\nRECT 0 0 1280 800 3\nabcRECT 64 64 64 64 3\ndefUPDATE 0\n"))
			// Then the connection ends and the watcher starts again.
		}
	}))
	defer srv.Close()
	a := NewApp()
	a.emit = func(name string, data any) {
		mu.Lock()
		defer mu.Unlock()
		m, ok := data.(map[string]any)
		if !ok {
			m = map[string]any{}
			for k, v := range data.(map[string]string) {
				m[k] = v
			}
		}
		m["name"] = name
		events = append(events, m)
	}
	a.startup(t.Context())
	a.conn.URL, a.conn.Token = srv.URL, "tok"
	if err := a.WatchRects("a/b"); err == nil {
		t.Error("a bad ref is refused")
	}
	for _, ref := range []string{"tom", "old", "odd", "bad", "huge"} {
		if err := a.WatchRects(ref); err != nil {
			t.Fatal(err)
		}
	}
	has := func(ok func(e map[string]any) bool) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range events {
			if ok(e) {
				return true
			}
		}
		return false
	}
	errorOf := func(ref, prefix string) func(map[string]any) bool {
		return func(e map[string]any) bool {
			s, _ := e["error"].(string)
			return e["name"] == "kitties:rects:"+ref && strings.HasPrefix(s, prefix)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !(has(errorOf("huge", "an update of a size")) && has(errorOf("bad", "not a rectangle")) &&
		has(errorOf("odd", "not a stream")) && has(errorOf("old", "the gateway answered HTTP 404"))) {
		time.Sleep(10 * time.Millisecond)
	}
	for ref, prefix := range map[string]string{"old": "the gateway answered HTTP 404", "odd": "not a stream of rectangles", "bad": "not a rectangle", "huge": "an update of a size"} {
		if !has(errorOf(ref, prefix)) {
			t.Errorf("%s: no error %q in %v", ref, prefix, events)
		}
	}
	for _, ref := range []string{"tom", "old", "odd", "bad", "huge"} {
		a.UnwatchRects(ref)
	}
	if !has(func(e map[string]any) bool {
		size, _ := e["size"].([]int)
		return e["name"] == "kitties:rects:tom" && len(size) == 2 && size[0] == 1280 && size[1] == 800
	}) {
		t.Errorf("no size in %v", events)
	}
	if !has(func(e map[string]any) bool {
		list, _ := e["rects"].([]map[string]any)
		return e["name"] == "kitties:rects:tom" && len(list) == 2 && list[1]["x"] == 64 && list[1]["w"] == 64 &&
			strings.HasPrefix(list[0]["data"].(string), "data:text/plain; charset=utf-8;base64,YWJj")
	}) {
		t.Errorf("no rectangles in %v", events)
	}
}
