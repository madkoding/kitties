package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// boatServer is a small in-memory Boat: enough of the sandbox API to run the contract against.
type boatServer struct {
	mu        sync.Mutex
	t         *testing.T
	next      int
	boxes     map[string]*boatSandbox
	keys      map[string]string
	snaps     map[string]string // name -> status
	pageSize  int
	snapFails bool
	stopFails bool
	hits      map[string]int
	secret    string
}

func newBoatServer(t *testing.T) (*boatServer, *Boat) {
	s := &boatServer{t: t, boxes: map[string]*boatSandbox{}, keys: map[string]string{}, snaps: map[string]string{}, hits: map[string]int{}, secret: "sk-test"}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, &Boat{Key: s.secret, Deployment: "dep-1", BaseURL: srv.URL, Poll: time.Millisecond, Timeout: 2 * time.Second}
}

func (s *boatServer) reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (s *boatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits[r.Method+" "+strings.SplitN(r.URL.Path, "?", 2)[0]]++
	if r.Header.Get("Authorization") != "Bearer "+s.secret {
		s.reply(w, 401, map[string]string{"code": "unauthorized"})
		return
	}
	p := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(p, "/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch {
	case r.Method == "POST" && p == "sandboxes":
		if id, ok := s.keys[r.Header.Get("Idempotency-Key")]; ok {
			s.reply(w, 200, map[string]any{"sandbox": s.boxes[id]})
			return
		}
		s.next++
		labels := map[string]string{}
		for k, v := range body["labels"].(map[string]any) {
			labels[k] = v.(string)
		}
		b := &boatSandbox{ID: "bx_" + strings.Repeat("a", 7) + string(rune('0'+s.next)), Name: body["name"].(string), State: "provisioned", Labels: labels, SnapshotAvailable: false}
		s.boxes[b.ID] = b
		s.keys[r.Header.Get("Idempotency-Key")] = b.ID
		s.reply(w, 200, map[string]any{"sandbox": b})
	case r.Method == "GET" && p == "sandboxes":
		ids := []string{}
		for id := range s.boxes {
			ids = append(ids, id)
		}
		sortStrings(ids)
		start := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			for i, id := range ids {
				if id == c {
					start = i + 1
				}
			}
		}
		end := len(ids)
		page := map[string]any{"hasMore": false}
		if s.pageSize > 0 && start+s.pageSize < end {
			end = start + s.pageSize
			page = map[string]any{"hasMore": true, "nextCursor": ids[end-1]}
		}
		out := []*boatSandbox{}
		for _, id := range ids[start:end] {
			out = append(out, s.boxes[id])
		}
		s.reply(w, 200, map[string]any{"sandboxes": out, "pageInfo": page})
	case len(parts) == 2 && parts[0] == "sandboxes" && r.Method == "GET":
		b, ok := s.boxes[parts[1]]
		if !ok {
			s.reply(w, 404, map[string]string{"code": "not_found"})
			return
		}
		s.reply(w, 200, map[string]any{"sandbox": b})
	case len(parts) == 3 && parts[2] == "stop":
		b := s.boxes[parts[1]]
		if s.stopFails {
			s.reply(w, 500, map[string]string{"code": "stop_failed"})
			return
		}
		b.State, b.SnapshotAvailable = "archived", true
		if s.snapFails {
			b.LastSnapshotStatus = "failed"
		}
		s.reply(w, 200, nil)
	case len(parts) == 3 && parts[2] == "resume":
		b := s.boxes[parts[1]]
		b.State = "idle"
		s.reply(w, 200, nil)
	case len(parts) == 2 && parts[0] == "sandboxes" && r.Method == "DELETE":
		if r.Header.Get("X-Ascii-Confirm-Delete") != parts[1] {
			s.reply(w, 400, map[string]string{"code": "confirm_required"})
			return
		}
		delete(s.boxes, parts[1])
		s.reply(w, 204, nil)
	case r.Method == "POST" && p == "saved-snapshots":
		s.snaps[body["name"].(string)] = "ready"
		s.reply(w, 200, nil)
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "saved-snapshots":
		st, ok := s.snaps[parts[1]]
		if !ok {
			s.reply(w, 404, map[string]string{"code": "not_found"})
			return
		}
		s.reply(w, 200, map[string]any{"snapshot": map[string]string{"name": parts[1], "status": st}})
	default:
		s.reply(w, 404, map[string]string{"code": "unknown_route"})
	}
}

func sortStrings(a []string) {
	for i := range a {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func TestBoatKeepsTheContract(t *testing.T) {
	Contract(t, func(t *testing.T) Provider { _, b := newBoatServer(t); return b })
}

func TestBoatRequiresKeyAndDeployment(t *testing.T) {
	b := &Boat{BaseURL: "http://127.0.0.1:1"}
	if _, err := b.List(context.Background()); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("list = %v", err)
	}
}

func TestBoatSendsKeyAndNeverLeaksItInErrors(t *testing.T) {
	s, b := newBoatServer(t)
	b.Key = "sk-wrong"
	_, err := b.List(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 || strings.Contains(err.Error(), "sk-wrong") {
		t.Fatalf("err = %v", err)
	}
	_ = s
}

func TestBoatIgnoresMachinesItDidNotMake(t *testing.T) {
	ctx := context.Background()
	s, b := newBoatServer(t)
	s.boxes["bx_foreign1"] = &boatSandbox{ID: "bx_foreign1", State: "idle", Labels: map[string]string{labelDeployment: "other"}}
	s.boxes["bx_shared01"] = &boatSandbox{ID: "bx_shared01", State: "idle", Access: "shared", Labels: map[string]string{labelDeployment: "dep-1"}}
	if all, err := b.List(ctx); err != nil || len(all) != 0 {
		t.Fatalf("list = %v %v", all, err)
	}
	for _, f := range []func() error{
		func() error { _, err := b.Get(ctx, "bx_foreign1"); return err },
		func() error { _, err := b.Sleep(ctx, "bx_foreign1"); return err },
		func() error { _, err := b.Wake(ctx, "bx_foreign1"); return err },
		func() error { return b.Delete(ctx, "bx_foreign1") },
		func() error { return b.Delete(ctx, "bx_shared01") },
	} {
		if err := f(); !errors.Is(err, ErrNotFound) {
			t.Errorf("a foreign machine must look absent: %v", err)
		}
	}
	if len(s.boxes) != 2 {
		t.Error("a foreign machine was touched")
	}
}

func TestBoatListFollowsPagesAndRefusesAPartialInventory(t *testing.T) {
	ctx := context.Background()
	s, b := newBoatServer(t)
	s.pageSize = 2
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		if _, err := b.Create(ctx, CreateRequest{Key: k, Name: k}); err != nil {
			t.Fatal(err)
		}
	}
	if all, err := b.List(ctx); err != nil || len(all) != 5 {
		t.Fatalf("list = %d %v", len(all), err)
	}
	// A cursor that never advances is an incomplete inventory, not a short list.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sandboxes":[],"pageInfo":{"hasMore":true,"nextCursor":"x"}}`))
	}))
	defer srv.Close()
	bad := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL}
	if _, err := bad.List(ctx); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("stuck cursor = %v", err)
	}
	// Ensure then refuses to create behind it.
	if _, err := Ensure(ctx, &failCreateBoat{bad}, CreateRequest{Key: "k", Name: "n"}); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("ensure = %v", err)
	}
}

type failCreateBoat struct{ *Boat }

func (failCreateBoat) Create(context.Context, CreateRequest) (Machine, error) {
	return Machine{}, errors.New("connection reset")
}

func TestBoatCreateLimitAndRetryWithTheSameKey(t *testing.T) {
	ctx := context.Background()
	s, b := newBoatServer(t)
	b.MaxRunning = 1
	a, err := b.Create(ctx, CreateRequest{Key: "k1", Name: "mimi"})
	if err != nil || a.Name != "mimi" || a.Key != "k1" || a.State != Running {
		t.Fatalf("create = %+v %v", a, err)
	}
	if again, err := b.Create(ctx, CreateRequest{Key: "k1", Name: "mimi"}); err != nil || again.ID != a.ID {
		t.Errorf("a retry at the limit must return the same machine: %+v %v", again, err)
	}
	if _, err := b.Create(ctx, CreateRequest{Key: "k2", Name: "tom"}); !errors.Is(err, ErrLimit) {
		t.Errorf("second = %v, want ErrLimit", err)
	}
	if _, err := b.Create(ctx, CreateRequest{}); err == nil {
		t.Error("an empty request must fail")
	}
	if s.hits["POST /sandboxes"] != 1 {
		t.Errorf("%d creates reached Boat, want 1", s.hits["POST /sandboxes"])
	}
}

func TestBoatSleepFailuresAreNotSaved(t *testing.T) {
	ctx := context.Background()
	s, b := newBoatServer(t)
	m, _ := b.Create(ctx, CreateRequest{Key: "k", Name: "n"})
	s.snapFails = true
	if _, err := b.Sleep(ctx, m.ID); err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Errorf("failed snapshot = %v", err)
	}
	s2, b2 := newBoatServer(t)
	m2, _ := b2.Create(ctx, CreateRequest{Key: "k", Name: "n"})
	s2.stopFails = true
	if _, err := b2.Sleep(ctx, m2.ID); err == nil {
		t.Error("a refused stop must fail")
	}
}

func TestBoatSleepWaitIsBounded(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sandbox":{"id":"bx_00000001","state":"busy","labels":{"kitties.deployment":"d"}}}`))
	}))
	defer srv.Close()
	b := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL, Poll: time.Millisecond, Timeout: 30 * time.Millisecond}
	if _, err := b.Sleep(ctx, "bx_00000001"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("sleep = %v", err)
	}
}

func TestBoatCheckpointFailureAndMissingMachineID(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/sandboxes":
			_, _ = w.Write([]byte(`{"sandbox":{}}`))
		case strings.HasPrefix(r.URL.Path, "/saved-snapshots/"):
			_, _ = w.Write([]byte(`{"snapshot":{"status":"failed"}}`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`{"sandbox":{"id":"bx_00000001","state":"archived","snapshotAvailable":true,"labels":{"kitties.deployment":"d"}}}`))
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	b := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL, Poll: time.Millisecond, Timeout: time.Second}
	if _, err := b.Create(ctx, CreateRequest{Key: "k", Name: "n"}); err == nil {
		t.Error("an answer with no machine id must fail")
	}
	if _, err := b.Sleep(ctx, "bx_00000001"); err == nil || !strings.Contains(err.Error(), "checkpoint failed") {
		t.Errorf("sleep = %v", err)
	}
}

func TestBoatDoesNotFollowRedirects(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	b := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL}
	if _, err := b.List(context.Background()); err == nil || leaked {
		t.Errorf("redirect: err=%v leaked=%v", err, leaked)
	}
}

func TestBoatDefaults(t *testing.T) {
	b := &Boat{}
	if b.base() != DefaultBoatURL || b.ttl() != 3600 || b.poll() != 2*time.Second || b.timeout() != 10*time.Minute {
		t.Error("defaults")
	}
	if (&Boat{BaseURL: "http://x/"}).base() != "http://x" {
		t.Error("a trailing slash is dropped")
	}
	if e := (&APIError{Status: 429, Code: "RATE"}); !strings.Contains(e.Error(), "RATE") {
		t.Error("APIError text")
	}
}

func TestBoatWakeReportsAnUnhydratedMachineAsNotReady(t *testing.T) {
	ctx := context.Background()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(200)
			return
		}
		n++
		h := "true"
		if n < 3 {
			h = "false"
		}
		_, _ = w.Write([]byte(`{"sandbox":{"id":"bx_00000001","state":"idle","hydrated":` + h + `,"labels":{"kitties.deployment":"d"}}}`))
	}))
	defer srv.Close()
	b := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL, Poll: time.Millisecond, Timeout: time.Second}
	m, err := b.Wake(ctx, "bx_00000001")
	if err != nil || m.State != Running || n < 3 {
		t.Errorf("wake = %+v %v after %d reads", m, err, n)
	}
}

func TestBoatRefusalCarriesCodeAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"code":"rate_limited"}`))
	}))
	defer srv.Close()
	b := &Boat{Key: "k", Deployment: "d", BaseURL: srv.URL, Client: srv.Client(), TTL: 2 * time.Hour}
	_, err := b.Get(context.Background(), "bx_00000001")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "rate_limited" || ae.RetryAfter != 7*time.Second || b.ttl() != 7200 {
		t.Errorf("err = %v (%+v), ttl %d", err, ae, b.ttl())
	}
	if shortKey("0123456789abcdef") != "0123456789ab" || shortKey("ab") != "ab" {
		t.Error("shortKey")
	}
}
