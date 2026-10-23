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
