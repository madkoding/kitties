package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/orchestrator"
)

// fakeContainers is a runtime that always works.
type fakeContainers struct{ names map[string]bool }

func (f *fakeContainers) Up(_ context.Context, s container.Spec) (string, error) {
	f.names[s.Name] = true
	return "id", nil
}
func (f *fakeContainers) Start(context.Context, string) error { return nil }
func (f *fakeContainers) Stop(context.Context, string) error  { return nil }
func (f *fakeContainers) Remove(_ context.Context, n string) error {
	delete(f.names, n)
	return nil
}
func (f *fakeContainers) Exec(_ context.Context, n string, argv ...string) (string, error) {
	return "ran " + strings.Join(argv, " ") + "\n", nil
}
func (f *fakeContainers) ExecInput(_ context.Context, n, stdin string, argv ...string) (string, error) {
	return "ran " + strings.Join(argv, " ") + "\n", nil
}
func (f *fakeContainers) Stream(context.Context, string, int, int) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("FRAME 3\npng")), nil
}
func (f *fakeContainers) Screenshot(context.Context, string) ([]byte, error) {
	return []byte("PNG"), nil
}
func (f *fakeContainers) List(context.Context) ([]container.Info, error) {
	var out []container.Info
	for n := range f.names {
		out = append(out, container.Info{Name: n, Running: true})
	}
	return out, nil
}

func withRuntime(t *testing.T, available bool) {
	t.Helper()
	oldDetect, oldNew, oldNotify := detectRuntime, newContainers, notifyContext
	detectRuntime = func(context.Context, string) (container.Runtime, error) {
		if !available {
			return container.Runtime{}, container.ErrNoRuntime
		}
		return container.Runtime{Bin: "fake"}, nil
	}
	fc := &fakeContainers{names: map[string]bool{}}
	newContainers = func(container.Runtime) orchestrator.Containers { return fc }
	notifyContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	t.Cleanup(func() { detectRuntime, newContainers, notifyContext = oldDetect, oldNew, oldNotify })
}

// writeConfig writes a configuration whose only provider is a local chat server at url.
func writeConfig(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kitties.yaml")
	text := "data_dir: " + filepath.Join(dir, "data") + "\nproviders:\n  p:\n    kind: custom\n    model: m\n    base_url: " + url + "\n" +
		"kitties:\n  - name: mimi\n  - name: team\n    replicas: 2\n    desktop: false\ngateway:\n  listen: 127.0.0.1:0\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exec(t *testing.T, in string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(in), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsageVersionInit(t *testing.T) {
	if code, _, errOut := exec(t, ""); code != 2 || !strings.Contains(errOut, "Usage:") {
		t.Errorf("no command: %d %s", code, errOut)
	}
	if code, _, _ := exec(t, "", "-bogus"); code != 2 {
		t.Error("bad flag")
	}
	if code, out, _ := exec(t, "", "version"); code != 0 || !strings.Contains(out, "kitties dev") {
		t.Error("version")
	}
	if code, _, _ := exec(t, "", "help"); code != 0 {
		t.Error("help")
	}
	path := filepath.Join(t.TempDir(), "sub", "kitties.yaml")
	if code, out, _ := exec(t, "", "-config", path, "init"); code != 0 || !strings.Contains(out, "Wrote") {
		t.Errorf("init: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "souls", "tom.yaml")); err != nil {
		t.Error("init writes the sample soul")
	}
	if code, _, errOut := exec(t, "", "-config", path, "init"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Error("init refuses to overwrite")
	}
	bad := filepath.Join(path, "x.yaml")
	if code, _, _ := exec(t, "", "-config", bad, "init"); code != 1 {
		t.Error("init under a file fails")
	}
	wd, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer os.Chdir(wd)
	if code, _, _ := exec(t, "", "init"); code != 0 {
		t.Error("init in the working directory")
	}
	if _, err := os.ReadFile("kitties.yaml"); err != nil {
		t.Error("kitties.yaml written")
	}
	// The written example must load, keys aside.
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("OPENAI_API_KEY", "k")
	withRuntime(t, false)
	t.Setenv("HOME", dir)
	if code, out, errOut := exec(t, "", "ls"); code != 0 || !strings.Contains(out, "no kitties yet") {
		t.Errorf("ls on the example: %d %s %s", code, out, errOut)
	}
	if code, _, errOut := exec(t, "", "-config", filepath.Join(dir, "missing.yaml"), "ls"); code != 1 || !strings.Contains(errOut, "kitties init") {
		t.Error("a missing configuration points at init")
	}
}

// chatServer answers the chat completions with the given texts in order, repeating the last.
func chatServer(t *testing.T, texts ...string) string {
	t.Helper()
	mux := http.NewServeMux()
	var mu sync.Mutex
	n := 0
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		text := texts[n]
		if n < len(texts)-1 {
			n++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconv.Quote(text) + `}}]}`))
	})
	srv := &http.Server{Handler: mux}
	ln, err := newListener()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

func TestFleetCommands(t *testing.T) {
	withRuntime(t, true)
	cfg := writeConfig(t, chatServer(t, "hello there"))
	if code, out, errOut := exec(t, "", "-config", cfg, "providers"); code != 0 || !strings.Contains(out, "custom/m") {
		t.Errorf("providers: %d %s %s", code, out, errOut)
	}
	if code, out, errOut := exec(t, "", "-config", cfg, "create", "ana", "-persona", "a tester"); code != 0 || !strings.Contains(out, "ana is up") {
		t.Errorf("create: %d %s %s", code, out, errOut)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create", "ana"); code != 1 {
		t.Error("duplicate create fails")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create"); code != 1 {
		t.Error("create needs a name")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create", "x", "-bogus"); code != 1 {
		t.Error("create bad flag")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "create", "brain", "-no-desktop", "-connectors", "a"); code != 1 || out != "" {
		t.Error("unknown connector")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create", "brain", "-no-desktop", "-avatar", "tuxedo/mint"); code != 0 {
		t.Error("brain-only create")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "ls"); code != 0 || !strings.Contains(out, "tuxedo/mint") {
		t.Errorf("ls shows the avatar: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create", "lion", "-avatar", "lion"); code != 1 {
		t.Error("a bad avatar is refused")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "ls"); code != 0 || !strings.Contains(out, "ana") || !strings.Contains(out, "brain") {
		t.Errorf("ls: %s", out)
	}
	for _, cmd := range []string{"stop", "start", "rm"} {
		if code, out, _ := exec(t, "", "-config", cfg, cmd, "ana"); code != 0 || !strings.Contains(out, "ana: "+cmd) {
			t.Errorf("%s: %d %s", cmd, code, out)
		}
		if code, _, _ := exec(t, "", "-config", cfg, cmd); code != 1 {
			t.Errorf("%s needs a name", cmd)
		}
	}
	if code, _, _ := exec(t, "", "-config", cfg, "start", "nobody"); code != 1 {
		t.Error("unknown kitty")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "scale", "team", "3"); code != 0 || !strings.Contains(out, "team-3") {
		t.Errorf("scale: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "scale", "team"); code != 1 {
		t.Error("scale usage")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "scale", "team", "x"); code != 1 {
		t.Error("scale number")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "scale", "nobody", "1"); code != 1 {
		t.Error("scale unknown")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "exec", "team-1", "ls"); code != 1 || out != "" {
		t.Error("exec without a container")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "create", "desk"); code != 0 {
		t.Error("create desk")
	}
	if code, out, _ := exec(t, "", "-config", cfg, "exec", "desk", "ls", "-la"); code != 0 || out != "ran ls -la\n" {
		t.Errorf("exec: %d %q", code, out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "exec", "desk"); code != 1 {
		t.Error("exec usage")
	}
	shot := filepath.Join(t.TempDir(), "s.png")
	if code, out, _ := exec(t, "", "-config", cfg, "shot", "desk", shot); code != 0 || !strings.Contains(out, "saved") {
		t.Errorf("shot: %s", out)
	}
	if data, _ := os.ReadFile(shot); string(data) != "PNG" {
		t.Error("shot file")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "shot", "desk"); code != 1 {
		t.Error("shot usage")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "shot", "nobody", shot); code != 1 {
		t.Error("shot unknown")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "shot", "desk", filepath.Join(shot, "x")); code != 1 {
		t.Error("shot unwritable")
	}
	if code, _, errOut := exec(t, "", "-config", cfg, "bogus"); code != 1 || !strings.Contains(errOut, "unknown command") {
		t.Error("unknown command")
	}
	t.Setenv("USER", "pat")
	in := "\nhi\n@desk and you?\n/quit\n"
	code, out, errOut := exec(t, in, "-config", cfg, "chat", "desk,brain")
	if code != 0 || strings.Count(out, "hello there") != 3 || !strings.Contains(out, "@name sends") {
		t.Errorf("group chat: %d %s %s", code, out, errOut)
	}
	if code, out, _ := exec(t, "hi\n", "-config", cfg, "chat", "brain"); code != 0 || !strings.Contains(out, "brain: hello there") {
		t.Errorf("single chat to the end of input: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "chat"); code != 1 {
		t.Error("chat usage")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "chat", "nobody"); code != 1 {
		t.Error("chat unknown kitty")
	}
	// A brain that fails is reported and the chat goes on.
	bad := writeConfig(t, "http://127.0.0.1:1")
	if code, _, _ := exec(t, "", "-config", bad, "create", "x", "-no-desktop"); code != 0 {
		t.Error("create x")
	}
	if code, out, _ := exec(t, "hi\n/exit\n", "-config", bad, "chat", "x"); code != 0 || !strings.Contains(out, "could not answer") {
		t.Errorf("failing brain: %s", out)
	}
	// Chat state that cannot be saved is reported.
	_ = os.WriteFile(filepath.Join(filepath.Dir(bad), "data", "chats", "zz.json"), []byte("{"), 0o600)
	if code, _, errOut := exec(t, "", "-config", bad, "ls"); code != 1 || !strings.Contains(errOut, "damaged") {
		t.Error("damaged chats are reported")
	}
}

func TestChatFollowsDesktopTask(t *testing.T) {
	withRuntime(t, true)
	pollEvery = 5 * time.Millisecond
	defer func() { pollEvery = 500 * time.Millisecond }()
	cfg := writeConfig(t, chatServer(t,
		"On it.\n@tool click {\"x\": 5, \"y\": 5}",
		"@tool done {\"summary\": \"Clicked the thing.\"}",
		"anything else?"))
	if code, _, _ := exec(t, "", "-config", cfg, "create", "desk"); code != 0 {
		t.Fatal("create desk")
	}
	code, out, errOut := exec(t, "click it\n/quit\n", "-config", cfg, "chat", "desk")
	if code != 0 || !strings.Contains(out, "desk: On it.") || !strings.Contains(out, "working on the desktop") ||
		!strings.Contains(out, "· click: ran xdotool") || !strings.Contains(out, "[shot 1-1.png]") || !strings.Contains(out, "desk: Clicked the thing.") {
		t.Errorf("follow: %d %s %s", code, out, errOut)
	}
}

func TestGatewayCommand(t *testing.T) {
	withRuntime(t, true)
	cfg := writeConfig(t, chatServer(t, "ok"))
	ctx, cancel := context.WithCancel(context.Background())
	notifyContext = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	out, errOut := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- run([]string{"-config", cfg, "gateway"}, strings.NewReader(""), out, errOut) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "listening") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("gateway: %d %s %s", code, out.String(), errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "gateway token") || !strings.Contains(text, "mimi") || !strings.Contains(text, "team-2") {
		t.Errorf("gateway output: %s", text)
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg), "data", "gateway.token")); err != nil || len(strings.TrimSpace(string(data))) != 32 {
		t.Errorf("the token is kept in the data folder: %q %v", data, err)
	}
	// A kitty that cannot start is reported, not fatal.
	withRuntime(t, false)
	badCfg := writeConfig(t, "http://127.0.0.1:1")
	_ = os.WriteFile(badCfg, []byte(strings.Replace(mustRead(t, badCfg), "listen: 127.0.0.1:0", "listen: 127.0.0.1:99999", 1)), 0o600)
	if code, _, errOut := exec(t, "", "-config", badCfg, "gateway"); code != 1 || !strings.Contains(errOut, "brain-only") || !strings.Contains(errOut, "listen on") {
		t.Errorf("gateway on a busy port: %d %s", code, errOut)
	}
}

// syncBuffer is a bytes.Buffer safe to read while the gateway goroutine writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOpenFailures(t *testing.T) {
	withRuntime(t, false)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "k.yaml")
	_ = os.WriteFile(cfg, []byte("data_dir: "+filepath.Join(dir, "data")+"\nproviders:\n  p:\n    kind: openai\n"), 0o600)
	t.Setenv("OPENAI_API_KEY", "")
	if code, _, errOut := exec(t, "", "-config", cfg, "ls"); code != 0 || !strings.Contains(errOut, "API key") {
		t.Errorf("a provider without a key is reported, the fleet still runs: %d %s", code, errOut)
	}
	if code, _, errOut := exec(t, "", "-config", cfg, "create", "x", "-no-desktop"); code != 1 || !strings.Contains(errOut, "cannot be used yet") {
		t.Errorf("a kitty cannot use the keyless profile: %d %s", code, errOut)
	}
	if code, _, errOut := exec(t, "", "-config", cfg, "providers"); code != 0 || strings.Contains(errOut, "brain-only") {
		t.Error("providers never probes the runtime")
	}
	if err := errors.Join(nil); err != nil {
		t.Error(err)
	}
}

func TestDemoMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withRuntime(t, false)
	if code, out, errOut := exec(t, "", "-demo", "providers"); code != 0 || !strings.Contains(out, "demo/scripted") || !strings.Contains(errOut, "demo mode") {
		t.Errorf("demo providers: %d %s %s", code, out, errOut)
	}
	if code, _, _ := exec(t, "", "-demo", "-config", filepath.Join(t.TempDir(), "none.yaml"), "providers"); code != 1 {
		t.Error("a named file that is missing fails even in demo mode")
	}
	pollEvery = 5 * time.Millisecond
	defer func() { pollEvery = 500 * time.Millisecond }()
	if code, out, _ := exec(t, "", "-demo", "create", "zoe"); code != 0 || !strings.Contains(out, "zoe is up (demo") {
		t.Errorf("demo create: %d %s", code, out)
	}
	code, out, errOut := exec(t, "hello\nopen https://example.com\n/quit\n", "-demo", "chat", "zoe")
	if code != 0 || !strings.Contains(out, "I am zoe") || !strings.Contains(out, "· open: opened https://example.com") || !strings.Contains(out, "placeholder domain") {
		t.Errorf("demo chat: %d %s %s", code, out, errOut)
	}
	// A configuration file still wins in demo mode: its providers become scripted brains.
	cfg := writeConfig(t, "http://127.0.0.1:1")
	if code, out, _ := exec(t, "", "-demo", "-config", cfg, "providers"); code != 0 || !strings.Contains(out, "p            demo/scripted") {
		t.Errorf("demo with a file: %d %s", code, out)
	}
}

func TestFollowTaskHelpers(t *testing.T) {
	if firstLine("  a\nb ") != "a…" || firstLine(strings.Repeat("x", 120)) != strings.Repeat("x", 100)+"…" || firstLine("ok") != "ok" {
		t.Error("firstLine")
	}
	withRuntime(t, true)
	cfg := writeConfig(t, chatServer(t, "@tool wait {\"seconds\": 3}", "@tool done {\"summary\": \"late\"}"))
	if code, _, _ := exec(t, "", "-config", cfg, "create", "desk"); code != 0 {
		t.Fatal("create desk")
	}
	// A cancelled context (ctrl+c) stops following and cancels the task.
	ctx, cancel := context.WithCancel(context.Background())
	notifyContext = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	pollEvery = 5 * time.Millisecond
	defer func() { pollEvery = 500 * time.Millisecond }()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	code, out, _ := exec(t, "wait for it\n", "-config", cfg, "chat", "desk")
	if code != 0 || !strings.Contains(out, "working on the desktop") || strings.Contains(out, "late") {
		t.Errorf("cancel: %d %s", code, out)
	}
	// A chat whose session vanished stops following.
	a := &app{out: &bytes.Buffer{}}
	a.chats = nil
	_ = a
}
