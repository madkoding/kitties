package llmauth

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/provider"
)

func TestInstallClaude(t *testing.T) {
	r := newRig(t)
	if _, err := r.m.Install("codex"); err == nil {
		t.Error("codex has no program")
	}
	release := make(chan struct{})
	installed := false
	r.m.installClaude = func(ctx context.Context, out *logTail) error {
		_, _ = out.Write([]byte("Downloading Claude Code...\n\x1b[32m 10%\r 55%"))
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		installed = true
		return nil
	}
	r.m.findClaude = func() (string, error) {
		if installed {
			return "/home/cat/.local/bin/claude", nil
		}
		return "", provider.ErrNoClaude
	}
	f, err := r.m.Install("claude-code")
	if err != nil || f.Status != Pending || f.Method != "install" {
		t.Fatalf("install = %+v, %v", f, err)
	}
	// Asking again follows the same install; a sign-in started meanwhile does not stop it.
	if g, _ := r.m.Install("claude-code"); g.ID != f.ID {
		t.Errorf("second install = %+v", g)
	}
	if _, err := r.m.Start("claude-code", StartOptions{}); !errors.Is(err, provider.ErrNoClaude) {
		t.Errorf("sign-in before claude = %v", err)
	}
	// What the installer prints is followed as it goes.
	for i := 0; i < 200; i++ {
		if g, _ := r.m.Get(f.ID); g.Log != "" {
			if g.Log != "Downloading Claude Code...\n 55%" {
				t.Errorf("log = %q", g.Log)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if done := r.wait(t, f.ID); done.Status != Connected || done.Who != "/home/cat/.local/bin/claude" {
		t.Fatalf("done = %+v", done)
	}
	// Installed already: done at once.
	if g, err := r.m.Install("claude-code"); err != nil || g.Status != Connected {
		t.Errorf("again = %+v, %v", g, err)
	}
}

func TestInstallClaudeFails(t *testing.T) {
	r := newRig(t)
	f, err := r.m.Install("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if done := r.wait(t, f.ID); done.Status != Failed || !strings.Contains(done.Error, "no installs in tests") {
		t.Errorf("failed = %+v", done)
	}
	// The installer ran, but claude is not where Kitties looks.
	r.m.installClaude = func(context.Context, *logTail) error { return nil }
	f, _ = r.m.Install("claude-code")
	if done := r.wait(t, f.ID); done.Status != Failed || !strings.Contains(done.Error, "KITTIES_CLAUDE_BIN") {
		t.Errorf("not found = %+v", done)
	}
}

func TestClaudeInstallerCommand(t *testing.T) {
	name, args := claudeInstaller()
	if runtime.GOOS != "windows" && (name != "sh" || !strings.Contains(args[1], "https://claude.ai/install.sh")) {
		t.Errorf("installer = %s %v", name, args)
	}
	// A cancelled run fails without installing anything.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runClaudeInstaller(ctx, &logTail{}); err == nil {
		t.Error("a cancelled install succeeded")
	}
}

func TestLogTail(t *testing.T) {
	l := &logTail{}
	if l.String() != "" || l.Last() != "" {
		t.Error("empty")
	}
	for i := 0; i < 12; i++ {
		_, _ = l.Write([]byte("line " + string(rune('a'+i)) + "\n"))
	}
	_, _ = l.Write([]byte("\n   \n"))
	got := strings.Split(l.String(), "\n")
	if len(got) != logLines || got[0] != "line g" || l.Last() != "line l" {
		t.Errorf("tail = %q, last %q", got, l.Last())
	}
	_, _ = l.Write([]byte(strings.Repeat("x", 2000)))
	if n := len(strings.Split(l.String(), "\n")); n != logLines || !strings.HasSuffix(l.String(), strings.Repeat("x", 1024)) {
		t.Errorf("long line kept %d lines", n)
	}
}

func TestClaudeInstallerSaysWhyItFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	t.Setenv("PATH", t.TempDir())
	out := &logTail{}
	// With no sh on the PATH it cannot even start; the error says so.
	if err := runClaudeInstaller(context.Background(), out); err == nil {
		t.Error("ran without sh")
	}
}
