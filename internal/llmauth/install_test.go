package llmauth

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/provider"
)

func TestInstallClaude(t *testing.T) {
	r := newRig(t)
	if _, err := r.m.Install("codex"); err == nil {
		t.Error("codex has no program")
	}
	release := make(chan struct{})
	installed := false
	r.m.installClaude = func(ctx context.Context) error {
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
	r.m.installClaude = func(context.Context) error { return nil }
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
	if err := runClaudeInstaller(ctx); err == nil {
		t.Error("a cancelled install succeeded")
	}
}
