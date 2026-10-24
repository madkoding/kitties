package container

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fake struct {
	calls  [][]string
	answer func(args []string) ([]byte, error)
}

func (f *fake) install(t *testing.T, bins ...string) {
	t.Helper()
	oldLook, oldRun, oldPoll := lookPath, runCmd, pollEvery
	lookPath = func(bin string) (string, error) {
		for _, b := range bins {
			if b == bin {
				return "/usr/bin/" + bin, nil
			}
		}
		return "", errors.New("not found")
	}
	runCmd = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, args)
		return f.answer(args)
	}
	pollEvery = time.Millisecond
	t.Cleanup(func() { lookPath, runCmd, pollEvery = oldLook, oldRun, oldPoll })
}

func ok(args []string) ([]byte, error) { return []byte("abc\n"), nil }

func TestDetect(t *testing.T) {
	f := &fake{answer: ok}
	f.install(t, "podman")
	rt, err := Detect(context.Background(), "auto")
	if err != nil || rt.Bin != "/usr/bin/podman" {
		t.Errorf("Detect = %+v, %v", rt, err)
	}
	if _, err := Detect(context.Background(), "docker"); !errors.Is(err, ErrNoRuntime) {
		t.Errorf("missing docker: %v", err)
	}
	f = &fake{answer: func([]string) ([]byte, error) { return []byte("daemon down"), errors.New("x") }}
	f.install(t, "docker")
	if _, err := Detect(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "daemon down") {
		t.Errorf("a runtime that does not answer: %v", err)
	}
	f.install(t)
	if _, err := Detect(context.Background(), "auto"); !errors.Is(err, ErrNoRuntime) {
		t.Error("no binary at all")
	}
}

func TestSpecValidateAndArgs(t *testing.T) {
	good := Spec{Name: "kitty-mimi", KittyName: "mimi", Image: "ghcr.io/x/y:latest", MemoryMB: 1024, CPUs: 1.5,
		Network: "cats", Workspace: "/data/mimi", Env: map[string]string{"B": "2", "A": "1"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(good.Args(), " ")
	for _, want := range []string{"run -d --name kitty-mimi", "--label kitties.bot=1", "--label kitties.name=mimi",
		"--memory 1024m", "--cpus 1.5", "--network cats",
		"-v /data/mimi:/home/kitty/work", "-e A=1 -e B=2", "-e KITTY_NAME=mimi -e DISPLAY=:99 ghcr.io/x/y:latest"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	minimal := Spec{Name: "a", Image: "i"}
	if a := strings.Join(minimal.Args(), " "); strings.Contains(a, "--memory") || strings.Contains(a, "-p ") {
		t.Errorf("minimal args = %s", a)
	}
	bad := []Spec{
		{Name: "Bad", Image: "i"},
		{Name: "a", Image: "Bad Image"},
		{Name: "a", Image: "i", MemoryMB: -1},
		{Name: "a", Image: "i", Network: "bad net"},
		{Name: "a", Image: "i", Env: map[string]string{"1x": "y"}},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v must be refused", s)
		}
	}
}

func TestManagerLifecycle(t *testing.T) {
	f := &fake{answer: ok}
	f.install(t, "docker")
	m := New(Runtime{Bin: "docker"})
	if m.Runtime() != "docker" {
		t.Error("Runtime")
	}
	id, err := m.Up(context.Background(), Spec{Name: "kitty-a", Image: "img"})
	if err != nil || id != "abc" {
		t.Errorf("Up = %q, %v", id, err)
	}
	if _, err := m.Up(context.Background(), Spec{Name: "Bad"}); err == nil {
		t.Error("Up validates")
	}
	for _, fn := range []func(context.Context, string) error{m.Stop, m.Start, m.Remove} {
		if err := fn(context.Background(), "kitty-a"); err != nil {
			t.Error(err)
		}
		if err := fn(context.Background(), "Bad Name"); err == nil {
			t.Error("names are validated")
		}
	}
	if f.calls[1][0] != "stop" || f.calls[2][0] != "start" || f.calls[3][0] != "rm" {
		t.Errorf("calls = %v", f.calls)
	}
	out, err := m.Exec(context.Background(), "kitty-a", "ls", "-la")
	if err != nil || out != "abc\n" || f.calls[4][0] != "exec" || f.calls[4][2] != Workdir {
		t.Errorf("Exec = %q, %v, %v", out, err, f.calls[4])
	}
	if _, err := m.Exec(context.Background(), "kitty-a"); err == nil {
		t.Error("Exec needs a command")
	}
	if _, err := m.Exec(context.Background(), "Bad", "ls"); err == nil {
		t.Error("Exec validates the name")
	}
	if png, err := m.Screenshot(context.Background(), "kitty-a"); err != nil || string(png) != "abc\n" {
		t.Errorf("Screenshot = %q, %v", png, err)
	}
	if _, err := m.Screenshot(context.Background(), "Bad"); err == nil {
		t.Error("Screenshot validates the name")
	}
	f.answer = func(args []string) ([]byte, error) { return []byte("line1\nboom"), errors.New("exit 1") }
	if _, err := m.Up(context.Background(), Spec{Name: "kitty-a", Image: "img"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Up failure: %v", err)
	}
	if err := m.Stop(context.Background(), "kitty-a"); err == nil {
		t.Error("Stop failure")
	}
	if out, err := m.Exec(context.Background(), "kitty-a", "false"); err == nil || out == "" {
		t.Error("Exec failure keeps the output")
	}
	if _, err := m.Screenshot(context.Background(), "kitty-a"); err == nil {
		t.Error("Screenshot failure")
	}
}

func TestListStatusWait(t *testing.T) {
	ps := "id1|kitty-a|img|Up 2 minutes|a\nid2|kitty-b|img|Exited (0)|b\ngarbage\n"
	f := &fake{answer: func(args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte(ps), nil
		}
		return nil, nil
	}}
	f.install(t, "docker")
	m := New(Runtime{Bin: "docker"})
	infos, err := m.List(context.Background())
	if err != nil || len(infos) != 2 || !infos[0].Running || infos[1].Running || infos[1].Kitty != "b" {
		t.Errorf("List = %+v, %v", infos, err)
	}
	exists, running, err := m.Status(context.Background(), "kitty-b")
	if err != nil || !exists || running {
		t.Errorf("Status = %v %v %v", exists, running, err)
	}
	if exists, _, _ := m.Status(context.Background(), "kitty-z"); exists {
		t.Error("unknown container")
	}
	if err := m.WaitReady(context.Background(), "kitty-a", time.Second); err != nil {
		t.Error(err)
	}
	if err := m.WaitReady(context.Background(), "kitty-b", 3*time.Millisecond); err == nil {
		t.Error("a stopped container never becomes ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.WaitReady(ctx, "kitty-b", time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait: %v", err)
	}
	f.answer = func([]string) ([]byte, error) { return []byte("no daemon"), errors.New("x") }
	if _, err := m.List(context.Background()); err == nil {
		t.Error("List failure")
	}
	if err := m.WaitReady(context.Background(), "kitty-a", time.Second); err == nil {
		t.Error("WaitReady reports the list failure")
	}
	if _, _, err := m.Status(context.Background(), "kitty-a"); err == nil {
		t.Error("Status reports the list failure")
	}
}

// A Mac app from the Finder has a bare PATH: the folders of Docker Desktop and the others are
// added when they exist, once each.
func TestWidenPath(t *testing.T) {
	oldOS, oldHome, oldSet, oldGet, oldDir := goos, homeDir, setPath, getPath, isDir
	t.Cleanup(func() { goos, homeDir, setPath, getPath, isDir = oldOS, oldHome, oldSet, oldGet, oldDir })
	path := "/usr/bin:/bin:/usr/local/bin"
	getPath = func() string { return path }
	setPath = func(v string) { path = v }
	homeDir = func() (string, error) { return "/Users/me", nil }
	isDir = func(p string) bool {
		return p == "/usr/local/bin" || p == "/Users/me/.docker/bin" || p == "/opt/homebrew/bin"
	}
	goos = "linux"
	widenPath()
	if path != "/usr/bin:/bin:/usr/local/bin" {
		t.Errorf("linux = %s", path)
	}
	goos = "darwin"
	widenPath()
	if path != "/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:/Users/me/.docker/bin" {
		t.Errorf("mac = %s", path)
	}
	path = ""
	homeDir = func() (string, error) { return "", errors.New("no home") }
	widenPath()
	if path != "/usr/local/bin:/opt/homebrew/bin" {
		t.Errorf("empty = %s", path)
	}
}

// A command can be given text on its input, and the text is never in the arguments or an error.
func TestExecInput(t *testing.T) {
	f := &fake{answer: ok}
	f.install(t, "docker")
	var gave string
	oldIn := runCmdIn
	runCmdIn = func(_ context.Context, stdin, _ string, args ...string) ([]byte, error) {
		gave = stdin
		f.calls = append(f.calls, args)
		return f.answer(args)
	}
	t.Cleanup(func() { runCmdIn = oldIn })
	m := &Manager{rt: Runtime{Bin: "docker"}}
	out, err := m.ExecInput(context.Background(), "kitty-a", "hunter2", "xdotool", "type", "--file", "-")
	if err != nil || out != "abc\n" || gave != "hunter2" {
		t.Fatalf("ExecInput = %q, %v (input %q)", out, err, gave)
	}
	if call := strings.Join(f.calls[len(f.calls)-1], " "); !strings.Contains(call, "exec -i") || strings.Contains(call, "hunter2") {
		t.Errorf("call = %q", call)
	}
	if _, err := m.ExecInput(context.Background(), "Bad", "x", "ls"); err == nil {
		t.Error("the name is validated")
	}
	if _, err := m.ExecInput(context.Background(), "kitty-a", "x"); err == nil {
		t.Error("it needs a command")
	}
	f.answer = func([]string) ([]byte, error) { return []byte("no display"), errors.New("exit 1") }
	if out, err := m.ExecInput(context.Background(), "kitty-a", "hunter2", "xdotool"); err == nil || out != "no display" || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("a failure = %q, %v", out, err)
	}
}

// An image is pulled again, and a container is stale when the image it was made from is not the
// one the reference names now.
func TestPullAndImageStale(t *testing.T) {
	f := &fake{answer: func(args []string) ([]byte, error) {
		switch {
		case args[0] == "inspect":
			return []byte("sha256:old\n"), nil
		case args[0] == "image" && args[1] == "inspect":
			return []byte("sha256:new\n"), nil
		}
		return []byte("ok"), nil
	}}
	f.install(t, "docker")
	m := &Manager{rt: Runtime{Bin: "docker"}}
	ctx := context.Background()
	if err := m.Pull(ctx, "ghcr.io/x/y:latest"); err != nil || strings.Join(f.calls[len(f.calls)-1], " ") != "pull -q ghcr.io/x/y:latest" {
		t.Errorf("Pull = %v %v", err, f.calls)
	}
	if err := m.Pull(ctx, "bad image"); err == nil {
		t.Error("a bad reference was pulled")
	}
	if stale, err := m.ImageStale(ctx, "kitty-a", "ghcr.io/x/y:latest"); err != nil || !stale {
		t.Errorf("old vs new = %v, %v", stale, err)
	}
	f.answer = func(args []string) ([]byte, error) { return []byte("sha256:same\n"), nil }
	if stale, _ := m.ImageStale(ctx, "kitty-a", "ghcr.io/x/y:latest"); stale {
		t.Error("the same image is stale")
	}
	// An image that is not here is not stale; a container that is not there is an error.
	f.answer = func(args []string) ([]byte, error) {
		if args[0] == "image" {
			return []byte("no such image"), errors.New("exit 1")
		}
		return []byte("sha256:old"), nil
	}
	if stale, err := m.ImageStale(ctx, "kitty-a", "ghcr.io/x/y:latest"); stale || err != nil {
		t.Errorf("no local image = %v, %v", stale, err)
	}
	f.answer = func(args []string) ([]byte, error) { return []byte("no such container"), errors.New("exit 1") }
	if _, err := m.ImageStale(ctx, "kitty-a", "ghcr.io/x/y:latest"); err == nil {
		t.Error("a container that is not there")
	}
	f.answer = func([]string) ([]byte, error) { return []byte("denied"), errors.New("exit 1") }
	if err := m.Pull(ctx, "ghcr.io/x/y:latest"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("a refused pull = %v", err)
	}
	for _, c := range [][2]string{{"Bad", "ghcr.io/x/y"}, {"kitty-a", "bad image"}} {
		if _, err := m.ImageStale(ctx, c[0], c[1]); err == nil {
			t.Errorf("ImageStale(%q, %q) was accepted", c[0], c[1])
		}
	}
}
