package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestArgsMountHomeWorkAndShared(t *testing.T) {
	s := Spec{Name: "kitty-a", KittyName: "a", Image: "img", Home: "/d/homes/a", Workspace: "/d/work/a", Shared: "/d/shared/a"}
	got := strings.Join(s.Args(), " ")
	home := strings.Index(got, "-v /d/homes/a:/home/kitty ")
	work := strings.Index(got, "-v /d/work/a:/home/kitty/work ")
	shared := strings.Index(got, "-v /d/shared/a:/home/kitty/Shared ")
	if home < 0 || work < home || shared < work {
		t.Errorf("the home first, then the folders inside it: %s", got)
	}
	if strings.Contains(strings.Join(Spec{Name: "kitty-a", Image: "img"}.Args(), " "), "/home/kitty ") {
		t.Error("no home: the image's own")
	}
}

func TestImageHasHomeAndMounts(t *testing.T) {
	f := &fake{answer: func(args []string) ([]byte, error) {
		switch {
		case args[0] == "image" && args[len(args)-1] == "new":
			return []byte("1\n"), nil
		case args[0] == "image" && args[len(args)-1] == "gone":
			return []byte("no such image"), errors.New("x")
		case args[0] == "image":
			return []byte("\n"), nil
		case args[len(args)-1] == "kitty-new":
			return []byte("/home/kitty\n/home/kitty/work\n\n"), nil
		case args[len(args)-1] == "kitty-old":
			return []byte("/home/kitty/work\n"), nil
		}
		return []byte("no such container"), errors.New("x")
	}}
	f.install(t, "docker")
	m := New(Runtime{Bin: "docker"})
	ctx := context.Background()
	if yes, err := m.ImageHasHome(ctx, "new"); !yes || err != nil {
		t.Errorf("labelled image: %v %v", yes, err)
	}
	if yes, _ := m.ImageHasHome(ctx, "old"); yes {
		t.Error("an image without the label")
	}
	if yes, err := m.ImageHasHome(ctx, "gone"); yes || err != nil {
		t.Error("an image not here yet has no home")
	}
	if _, err := m.ImageHasHome(ctx, "BAD REF"); err == nil {
		t.Error("a bad reference")
	}
	if has, err := m.HasHome(ctx, "kitty-new"); !has || err != nil {
		t.Errorf("mounted home: %v %v", has, err)
	}
	if has, err := m.HasHome(ctx, "kitty-old"); has || err != nil {
		t.Errorf("home inside: %v %v", has, err)
	}
	if _, err := m.HasHome(ctx, "kitty-x"); err == nil || !strings.Contains(err.Error(), "no such container") {
		t.Errorf("inspect fails: %v", err)
	}
	if _, err := m.Mounts(ctx, "Bad Name"); err == nil {
		t.Error("a bad name")
	}
}

func TestExecStreamsAndRestart(t *testing.T) {
	f := &fake{answer: ok}
	f.install(t, "docker")
	old := runStream
	t.Cleanup(func() { runStream = old })
	var got []string
	var input string
	runStream = func(_ context.Context, r io.Reader, w io.Writer, _ string, args ...string) ([]byte, error) {
		got = args
		if r != nil {
			b, _ := io.ReadAll(r)
			input = string(b)
		}
		if args[len(args)-1] == "fail" {
			return []byte("tar: boom\n"), errors.New("exit 2")
		}
		if args[len(args)-1] == "quiet" {
			return nil, errors.New("exit 3")
		}
		_, _ = w.Write([]byte("out"))
		return nil, nil
	}
	m := New(Runtime{Bin: "docker"})
	ctx := context.Background()
	var buf bytes.Buffer
	if err := m.ExecOut(ctx, "kitty-a", &buf, "tar", "-cf", "-"); err != nil || buf.String() != "out" ||
		strings.Join(got, " ") != "exec -w /home/kitty kitty-a tar -cf -" {
		t.Errorf("ExecOut: %v %q %v", err, buf.String(), got)
	}
	if err := m.ExecOut(ctx, "kitty-a", &buf, "fail"); err == nil || !strings.Contains(err.Error(), "tar: boom") {
		t.Errorf("the reason on stderr: %v", err)
	}
	if out, err := m.ExecIn(ctx, "kitty-a", strings.NewReader("tarball"), "kitty-import"); err != nil || out != "out" || input != "tarball" ||
		got[1] != "-i" {
		t.Errorf("ExecIn: %v %q %q %v", err, out, input, got)
	}
	if _, err := m.ExecIn(ctx, "kitty-a", strings.NewReader(""), "quiet"); err == nil || !strings.Contains(err.Error(), "exit 3") {
		t.Errorf("no stderr: the error: %v", err)
	}
	for _, err := range []error{
		m.ExecOut(ctx, "Bad", &buf, "x"), m.ExecOut(ctx, "kitty-a", &buf),
		func() error { _, e := m.ExecIn(ctx, "Bad", nil, "x"); return e }(),
		func() error { _, e := m.ExecIn(ctx, "kitty-a", nil); return e }(),
	} {
		if err == nil {
			t.Error("a bad name or no command is refused")
		}
	}
	if err := m.Restart(ctx, "kitty-a"); err != nil || strings.Join(f.calls[len(f.calls)-1], " ") != "restart -t 10 kitty-a" {
		t.Errorf("restart: %v %v", err, f.calls)
	}
}

func TestRunStreamRunsTheCommand(t *testing.T) {
	var out bytes.Buffer
	if _, err := runStream(context.Background(), strings.NewReader("hi"), &out, "cat"); err != nil || out.String() != "hi" {
		t.Errorf("cat: %v %q", err, out.String())
	}
	if stderr, err := runStream(context.Background(), nil, &out, "sh", "-c", "echo bad >&2; exit 1"); err == nil || string(stderr) != "bad\n" {
		t.Errorf("stderr: %q %v", stderr, err)
	}
}

func TestArgsMountTheBrowserVolume(t *testing.T) {
	s := Spec{Name: "kitty-a", Image: "img", Home: "/d/homes/a", BrowserVolume: "kitties-browser-kitty-a"}
	if got := strings.Join(s.Args(), " "); !strings.Contains(got, "-v kitties-browser-kitty-a:/var/lib/kitty-browser ") {
		t.Errorf("the browser's volume: %s", got)
	}
	if strings.Contains(strings.Join(Spec{Name: "kitty-a", Image: "img"}.Args(), " "), BrowserDir) {
		t.Error("no volume: none mounted")
	}
	if err := (Spec{Name: "kitty-a", Image: "img", BrowserVolume: "/etc:/x"}).Validate(); err == nil {
		t.Error("a volume name that is a path")
	}
}

func TestImageHasBrowserVolume(t *testing.T) {
	f := &fake{answer: func(args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), BrowserLabel) && args[len(args)-1] == "new" {
			return []byte("1\n"), nil
		}
		return []byte("\n"), nil
	}}
	f.install(t, "docker")
	m := New(Runtime{Bin: "docker"})
	if yes, err := m.ImageHasBrowserVolume(context.Background(), "new"); !yes || err != nil {
		t.Errorf("labelled image: %v %v", yes, err)
	}
	if yes, _ := m.ImageHasBrowserVolume(context.Background(), "old"); yes {
		t.Error("an image without the label")
	}
}
