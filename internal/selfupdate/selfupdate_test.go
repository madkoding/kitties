package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/config"
)

// release is a fake GitHub: the newest release, its binary for linux/amd64 and its SHA256SUMS.
type release_ struct {
	tag        string
	prerelease bool
	binary     string // what kitties-linux-amd64 holds
	sums       string // "" computes it from the binary
	noAsset    bool
	status     int
}

func fakeGitHub(t *testing.T, r *release_) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/repos/madkoding/kitties/releases/latest":
			if r.status != 0 {
				w.WriteHeader(r.status)
				return
			}
			assets := fmt.Sprintf(`[{"name":"kitties-linux-amd64","browser_download_url":"%[1]s/dl/bin"},{"name":"SHA256SUMS","browser_download_url":"%[1]s/dl/sums"}]`, srv.URL)
			if r.noAsset {
				assets = "[]"
			}
			fmt.Fprintf(w, `{"tag_name":%q,"prerelease":%v,"assets":%s}`, r.tag, r.prerelease, assets)
		case "/dl/bin":
			_, _ = w.Write([]byte(r.binary))
		case "/dl/sums":
			sums := r.sums
			if sums == "" {
				h := sha256.Sum256([]byte(r.binary))
				sums = hex.EncodeToString(h[:]) + "  kitties-linux-amd64\n0000  other-file\n"
			}
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func script(version string) string { return "#!/bin/sh\necho kitties " + version + "\n" }

// updater makes an updater of a fake installed binary at version v1.0.0.
func updater(t *testing.T, srv *httptest.Server, mode string) (*Updater, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a running program is not replaced on Windows")
	}
	exe := filepath.Join(t.TempDir(), "kitties")
	if err := os.WriteFile(exe, []byte(script("v1.0.0")), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults().Updates
	cfg.Gateway, cfg.API = mode, srv.URL
	u := New(cfg, "v1.0.0")
	u.Exe, u.OS, u.Arch, u.InDocker = exe, "linux", "amd64", false
	return u, exe
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, tag string
		want         bool
	}{
		{"v1.0.0", "v1.0.1", true}, {"v1.0.0", "v1.1.0", true}, {"v1.9.9", "v2.0.0", true}, {"1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.0.0", false}, {"v1.2.0", "v1.1.9", false}, {"v1.10.0", "v1.9.0", false},
		{"dev", "v9.9.9", false}, {"v1.0.0-4-gabc123", "v9.9.9", false}, {"v1.0.0-dirty", "v9.9.9", false},
		{"v1.0.0", "v2.0.0-rc1", false}, {"v1.0.0", "nightly", false}, {"", "v1.0.0", false},
	} {
		if got := Newer(c.current, c.tag); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.tag, got, c.want)
		}
	}
}

func TestCheckFindsANewerRelease(t *testing.T) {
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	u, _ := updater(t, fakeGitHub(t, r), config.UpdateNotify)
	st, err := u.Check(context.Background())
	if err != nil || !st.Available || st.Latest != "v1.1.0" || !st.CanApply || st.Mode != "notify" || st.CheckedAt.IsZero() {
		t.Fatalf("Check = %+v, %v", st, err)
	}
	if u.Status().Latest != "v1.1.0" {
		t.Error("the status is not kept")
	}
	// The same version, a pre-release, and a failing GitHub are not news.
	r.tag = "v1.0.0"
	if st, _ := u.Check(context.Background()); st.Available {
		t.Errorf("the same version is available: %+v", st)
	}
	r.tag, r.prerelease = "v2.0.0", true
	if st, _ := u.Check(context.Background()); st.Available {
		t.Errorf("a pre-release is available: %+v", st)
	}
	r.status = 500
	if st, err := u.Check(context.Background()); err == nil || st.Error == "" || st.Available {
		t.Errorf("a failing GitHub: %+v, %v", st, err)
	}
}

func TestADevelopmentBuildNeverAsks(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	defer srv.Close()
	u, _ := updater(t, srv, config.UpdateAuto)
	u.Current = "dev"
	st, err := u.Check(context.Background())
	if err != nil || st.Available || st.CanApply || !strings.Contains(st.Why, "development build") || asked {
		t.Errorf("dev = %+v %v asked %v", st, err, asked)
	}
}

func TestApplyReplacesTheBinaryAndKeepsTheOld(t *testing.T) {
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	u, exe := updater(t, fakeGitHub(t, r), config.UpdateNotify)
	ctx := context.Background()
	// In use: not now.
	u.Idle = func() bool { return false }
	if _, err := u.Apply(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy = %v", err)
	}
	u.Idle = func() bool { return true }
	to, err := u.Apply(ctx)
	if err != nil || to != "v1.1.0" {
		t.Fatalf("Apply = %q, %v", to, err)
	}
	if got, _ := os.ReadFile(exe); string(got) != script("v1.1.0") {
		t.Errorf("the binary is %q", got)
	}
	if prev, _ := os.ReadFile(exe + ".prev"); string(prev) != script("v1.0.0") {
		t.Errorf("the old binary was not kept: %q", prev)
	}
	if st, _ := os.Stat(exe); st.Mode().Perm()&0o100 == 0 {
		t.Error("the new binary is not executable")
	}
	// Nothing is left behind in the folder but the two.
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 2 {
		t.Errorf("the folder holds %d files", len(entries))
	}
	// Already the newest.
	u.Current = "v1.1.0"
	if _, err := u.Apply(ctx); err == nil || !strings.Contains(err.Error(), "already the newest") {
		t.Errorf("up to date = %v", err)
	}
}

func TestApplyRefusesWhatItCannotTrust(t *testing.T) {
	good := script("v1.1.0")
	h := sha256.Sum256([]byte(good))
	sum := hex.EncodeToString(h[:])
	for name, c := range map[string]struct {
		r    release_
		want string
	}{
		"a changed binary":       {release_{tag: "v1.1.0", binary: good, sums: strings.Repeat("a", 64) + "  kitties-linux-amd64\n"}, "does not match"},
		"a binary not listed":    {release_{tag: "v1.1.0", binary: good, sums: sum + "  another-file\n"}, "does not list"},
		"a binary that lies":     {release_{tag: "v1.1.0", binary: script("v0.0.1")}, "did not run as v1.1.0"},
		"a binary that is junk":  {release_{tag: "v1.1.0", binary: "not a program"}, "did not run as v1.1.0"},
		"a release with no file": {release_{tag: "v1.1.0", noAsset: true}, "has no kitties-linux-amd64"},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.r
			u, exe := updater(t, fakeGitHub(t, &r), config.UpdateAuto)
			if _, err := u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Apply = %v, want %q", err, c.want)
			}
			if got, _ := os.ReadFile(exe); string(got) != script("v1.0.0") {
				t.Errorf("the running binary was touched: %q", got)
			}
			if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
				t.Errorf("leftovers: %d files", len(entries))
			}
		})
	}
}

func TestOnlyHttpsAndLoopback(t *testing.T) {
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	srv := fakeGitHub(t, r)
	u, _ := updater(t, srv, config.UpdateAuto)
	if _, err := u.get(context.Background(), "http://example.com/x", 10, ""); err == nil || !strings.Contains(err.Error(), "not an https address") {
		t.Errorf("a plain http address = %v", err)
	}
	if _, err := u.get(context.Background(), "ftp://x/y", 10, ""); err == nil {
		t.Error("ftp")
	}
	if _, err := u.get(context.Background(), srv.URL+"/dl/bin", 3, ""); err == nil || !strings.Contains(err.Error(), "larger than expected") {
		t.Errorf("a download over the limit = %v", err)
	}
	if _, err := u.get(context.Background(), srv.URL+"/nothing", 10, ""); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("a 404 = %v", err)
	}
	// A redirect to a plain http address is refused.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "http://example.com/evil", http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := u.get(context.Background(), redirect.URL, 10, ""); err == nil {
		t.Error("a redirect to http was followed")
	}
}

func TestWhereItDoesNotUpdateItself(t *testing.T) {
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	u, exe := updater(t, fakeGitHub(t, r), config.UpdateAuto)
	for name, set := range map[string]func(){
		"a container":            func() { u.InDocker = true },
		"the app":                func() { u.Exe = "/Applications/Kitties.app/Contents/MacOS/kitties-gateway" },
		"the gateway of the app": func() { u.Exe = filepath.Join(filepath.Dir(exe), "kitties-gateway") },
		"windows":                func() { u.OS = "windows" },
		"a read-only place":      func() { u.Exe = filepath.Join(t.TempDir(), "missing", "kitties") },
	} {
		u.InDocker, u.OS, u.Exe = false, "linux", exe
		set()
		if ok, why := u.canApply(); ok || why == "" {
			t.Errorf("%s: canApply = %v %q", name, ok, why)
		}
	}
	u.InDocker, u.OS, u.Exe = false, "linux", exe
	if ok, _ := u.canApply(); !ok {
		t.Error("a plain binary in a writable folder cannot be updated")
	}
	if u.assetName() != "kitties-linux-amd64" {
		t.Errorf("asset = %s", u.assetName())
	}
	u.OS = "windows"
	if u.assetName() != "kitties-windows-amd64.exe" {
		t.Errorf("asset = %s", u.assetName())
	}
}

func TestChecksum(t *testing.T) {
	h := strings.Repeat("ab", 32)
	sums := h + "  kitties-linux-amd64\n" + strings.ToUpper(h) + " *kitties-darwin-arm64\nshort  x\n"
	if checksum(sums, "kitties-linux-amd64") != h || checksum(sums, "kitties-darwin-arm64") != h {
		t.Error("a listed file")
	}
	if checksum(sums, "x") != "" || checksum(sums, "missing") != "" {
		t.Error("a file that is not listed, or with a short sum")
	}
}

type told struct {
	mu        sync.Mutex
	available []string
	updated   []string
	logs      []string
	restarts  []string
}

func (t *told) hooks(window bool) Hooks {
	return Hooks{
		Window:    func() bool { return window },
		Available: func(s Status) { t.mu.Lock(); t.available = append(t.available, s.Latest+"|"+s.Why); t.mu.Unlock() },
		Updated:   func(from, to string) { t.mu.Lock(); t.updated = append(t.updated, from+">"+to); t.mu.Unlock() },
		Log:       func(f string, a ...any) { t.mu.Lock(); t.logs = append(t.logs, fmt.Sprintf(f, a...)); t.mu.Unlock() },
	}
}

func TestRoundsTellAndUpdate(t *testing.T) {
	ctx := context.Background()
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	srv := fakeGitHub(t, r)
	// notify: the person is told once per version, nothing is installed.
	u, exe := updater(t, srv, config.UpdateNotify)
	var got told
	u.Restart = func(exe string, args, env []string) error { got.restarts = append(got.restarts, exe); return nil }
	u.round(ctx, got.hooks(true))
	u.round(ctx, got.hooks(true))
	if len(got.available) != 1 || got.available[0] != "v1.1.0|" || len(got.restarts) != 0 {
		t.Errorf("notify: %+v", &got)
	}
	if b, _ := os.ReadFile(exe); string(b) != script("v1.0.0") {
		t.Error("notify mode installed something")
	}
	r.tag, r.binary = "v1.2.0", script("v1.2.0")
	u.round(ctx, got.hooks(true))
	if len(got.available) != 2 {
		t.Errorf("a newer version is news again: %v", got.available)
	}
	// auto: outside the window, or while in use, nothing; then it installs, tells and restarts.
	u, exe = updater(t, srv, config.UpdateAuto)
	got = told{}
	u.Restart = func(exe string, args, env []string) error { got.restarts = append(got.restarts, exe); return nil }
	u.round(ctx, got.hooks(false))
	u.Idle = func() bool { return false }
	u.round(ctx, got.hooks(true))
	if len(got.updated) != 0 || len(got.restarts) != 0 || len(got.available) != 0 {
		t.Errorf("waiting: %+v", &got)
	}
	u.Idle = func() bool { return true }
	u.round(ctx, got.hooks(true))
	if len(got.updated) != 1 || got.updated[0] != "v1.0.0>v1.2.0" || len(got.restarts) != 1 || got.restarts[0] != exe {
		t.Errorf("update: %+v", &got)
	}
	// A restart that fails is said, with the way out.
	u, _ = updater(t, srv, config.UpdateAuto)
	got = told{}
	u.Restart = func(string, []string, []string) error { return errors.New("no exec here") }
	u.round(ctx, got.hooks(true))
	if len(got.logs) < 2 || !strings.Contains(strings.Join(got.logs, "|"), "could not restart") {
		t.Errorf("a failed restart: %v", got.logs)
	}
	// An install that fails (a bad sum) is told once, so the person can do it by hand.
	r.sums = strings.Repeat("0", 64) + "  kitties-linux-amd64\n"
	u, _ = updater(t, srv, config.UpdateAuto)
	got = told{}
	u.round(ctx, got.hooks(true))
	u.round(ctx, got.hooks(true))
	if len(got.available) != 1 || !strings.Contains(strings.Join(got.logs, "|"), "does not match") {
		t.Errorf("a bad release: %+v", &got)
	}
	// Where it cannot replace itself, even auto only tells, with why.
	r.sums = ""
	u, _ = updater(t, srv, config.UpdateAuto)
	u.InDocker = true
	got = told{}
	u.round(ctx, got.hooks(true))
	if len(got.available) != 1 || !strings.Contains(got.available[0], "docker compose pull") {
		t.Errorf("in a container: %+v", &got)
	}
	// A GitHub that does not answer is logged, not told.
	r.status = 500
	u, _ = updater(t, srv, config.UpdateNotify)
	got = told{}
	u.round(ctx, got.hooks(true))
	if len(got.available) != 0 || len(got.logs) != 1 {
		t.Errorf("a failing GitHub: %+v", &got)
	}
}

func TestWatchLooksUntilStopped(t *testing.T) {
	r := &release_{tag: "v1.1.0", binary: script("v1.1.0")}
	u, _ := updater(t, fakeGitHub(t, r), config.UpdateNotify)
	var got told
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { u.Watch(ctx, time.Millisecond, time.Millisecond, got.hooks(true)); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got.mu.Lock()
		n := len(got.available)
		got.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watch did not stop")
	}
	if len(got.available) != 1 {
		t.Errorf("available = %v", got.available)
	}
	// Off does not look at all.
	off, _ := updater(t, fakeGitHub(t, r), config.UpdateOff)
	finished := make(chan struct{})
	go func() { off.Watch(context.Background(), time.Hour, time.Hour, Hooks{}); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("an off watch kept waiting")
	}
}

func TestNewAndRelaunch(t *testing.T) {
	u := New(config.Defaults().Updates, "v1.0.0")
	if u.Exe == "" || u.OS != runtime.GOOS || u.Arch != runtime.GOARCH || u.Restart == nil || u.Status().Current != "v1.0.0" || u.Status().Mode != "notify" {
		t.Errorf("New = %+v", u)
	}
	var got string
	u.Restart = func(exe string, args, env []string) error { got = exe; return nil }
	if err := u.Relaunch(); err != nil || got != u.Exe {
		t.Errorf("Relaunch = %v %q", err, got)
	}
	u.Restart = func(string, []string, []string) error { return errors.New("no") }
	if err := u.Relaunch(); err == nil || !strings.Contains(u.Status().Error, "restart it by hand") {
		t.Errorf("a failed relaunch: %v %+v", err, u.Status())
	}
	// The redirect rule: https and this machine only.
	redirect := u.HTTP.CheckRedirect
	for address, ok := range map[string]bool{"https://github.com/x": true, "http://127.0.0.1:1/x": true, "http://localhost/x": true, "http://example.com/x": false} {
		req, _ := http.NewRequest("GET", address, nil)
		if err := redirect(req, nil); (err == nil) != ok {
			t.Errorf("redirect to %s = %v", address, err)
		}
	}
}
