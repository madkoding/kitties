package reply

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

func TestListenRoutesThePersonsMessages(t *testing.T) {
	defer func(d time.Duration) { recheck = d }(recheck)
	recheck = 20 * time.Millisecond
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/topic-of-twenty-chars-x/json" {
			http.NotFound(w, r)
			return
		}
		fl := w.(http.Flusher)
		fmt.Fprintln(w, `{"id":"o","event":"open"}`)
		fmt.Fprintln(w, `{"id":"m1","event":"message","message":"luna: from me","tags":["`+connector.NtfyMark+`"]}`)
		fmt.Fprintln(w, `{"id":"m2","event":"message","message":"luna: hola"}`)
		fl.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	s, _, o := setup(luna, bo)
	ctx, cancel := context.WithCancel(context.Background())
	on := true
	var onMu sync.Mutex
	done := make(chan struct{})
	go func() {
		s.Listen(ctx, func() (connector.Settings, bool) {
			onMu.Lock()
			defer onMu.Unlock()
			return connector.Settings{"url": srv.URL, "topic": "topic-of-twenty-chars-x", "token": "tk"}, on
		}, srv.Client())
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for len(o.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := o.bodies(); got != "luna|luna heard: hola" {
		t.Errorf("sent %q (the gateway's own message must be skipped)", got)
	}
	mu.Lock()
	if len(seen) == 0 || seen[0] != "/topic-of-twenty-chars-x/json Bearer tk" {
		t.Errorf("requests %v", seen)
	}
	mu.Unlock()
	// Turning replies off closes the stream.
	onMu.Lock()
	on = false
	onMu.Unlock()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not stop")
	}
	s.Wait()
}

func TestStreamGoesOnFromTheLastMessage(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		fmt.Fprintln(w, `{"id":"m9","event":"message","message":"x","tags":["`+connector.NtfyMark+`"]}`)
	}))
	defer srv.Close()
	s, _, _ := setup(luna)
	last := "m3"
	read, err := s.stream(context.Background(), context.Background(), srv.Client(), map[string]string{"url": srv.URL, "topic": "t"}, &last)
	if !read || err != nil || got != "since=m3" || last != "m9" {
		t.Errorf("read %v err %v query %q last %q", read, err, got, last)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) }))
	defer srv2.Close()
	if _, err := s.stream(context.Background(), context.Background(), srv2.Client(), map[string]string{"url": srv2.URL, "topic": "t"}, &last); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("a refused topic: %v", err)
	}
}
