package llmauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes a claude program that answers `auth status` from a file and runs a
// `auth login` that prints its link and takes "good#x" as the code.
func fakeClaude(t *testing.T, body string) (bin, state string) {
	t.Helper()
	dir := t.TempDir()
	state = filepath.Join(dir, "signed-in")
	bin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\nSTATE=" + state + "\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, state
}

const claudeScript = `
case "$2" in
status)
  if [ -f "$STATE" ]; then echo '{"loggedIn": true}'; else echo '{"loggedIn": false}'; fi ;;
login)
  echo "Opening browser to sign in…"
  echo "If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?code=true&state=x"
  printf "Paste code here if prompted > "
  read code
  if [ "$code" = "good#x" ]; then touch "$STATE"; echo "Login successful."; exit 0; fi
  echo "Login failed: That code wasn't accepted."
  exit 1 ;;
esac
`

func waitFlow(t *testing.T, m *Manager, id string) Flow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, ok := m.Get(id)
		if !ok {
			t.Fatal("the flow is gone")
		}
		if f.Status != Pending {
			return f
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the flow never ended")
	return Flow{}
}

func TestClaudeSignsInThroughItsOwnProgram(t *testing.T) {
	r := newRig(t)
	bin, state := fakeClaude(t, claudeScript)
	r.m.findClaude = func() (string, error) { return bin, nil }
	r.m.claudeLogin = startClaudeLogin

	accounts := r.m.Accounts()
	if a := accounts[0]; a.Kind != "claude-code" || a.SignedIn || a.Missing || a.Who != bin {
		t.Fatalf("not signed in yet = %+v", a)
	}
	f, err := r.m.Start("claude-code", StartOptions{})
	if err != nil || f.Status != Pending || f.Method != "browser" || !f.Paste ||
		f.URL != "https://claude.com/cai/oauth/authorize?code=true&state=x" {
		t.Fatalf("start = %+v, %v", f, err)
	}
	if err := r.m.Paste(f.ID, "  good#x \n"); err != nil {
		t.Fatal(err)
	}
	if end := waitFlow(t, r.m, f.ID); end.Status != Connected || end.Who != bin {
		t.Fatalf("end = %+v", end)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatal("the program did not get the code")
	}
	// Nothing is kept by kitties: the program holds the login.
	if _, ok, _ := r.m.store.Load("claude-code"); ok {
		t.Error("a claude credential was saved")
	}
	if !r.m.SignedIn("claude-code") {
		t.Error("signed in after the code")
	}
	// Signed in already: done at once.
	f, err = r.m.Start("claude-code", StartOptions{})
	if err != nil || f.Status != Connected || f.Method != "instant" {
		t.Fatalf("again = %+v, %v", f, err)
	}
}

func TestClaudeRefusesAWrongCode(t *testing.T) {
	r := newRig(t)
	bin, _ := fakeClaude(t, claudeScript)
	r.m.findClaude = func() (string, error) { return bin, nil }
	r.m.claudeLogin = startClaudeLogin
	f, err := r.m.Start("claude-code", StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// An empty paste is waited past; a wrong code ends it with the program's words.
	if err := r.m.Paste(f.ID, " "); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := r.m.Paste(f.ID, "bad"); err != nil {
		t.Fatal(err)
	}
	end := waitFlow(t, r.m, f.ID)
	if end.Status != Failed || !strings.Contains(end.Error, "That code wasn't accepted") || strings.Contains(end.Error, "Login failed") {
		t.Fatalf("end = %+v", end)
	}
	if r.m.SignedIn("claude-code") {
		t.Error("a wrong code signed in")
	}
}

func TestClaudeSignInIsCancelled(t *testing.T) {
	r := newRig(t)
	bin, _ := fakeClaude(t, claudeScript)
	r.m.findClaude = func() (string, error) { return bin, nil }
	r.m.claudeLogin = startClaudeLogin
	f, err := r.m.Start("claude-code", StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A second start replaces the first, which stops its program.
	g, err := r.m.Start("claude-code", StartOptions{})
	if err != nil || g.ID == f.ID {
		t.Fatalf("second = %+v, %v", g, err)
	}
	if _, ok := r.m.Get(f.ID); ok {
		t.Error("the first sign-in is still there")
	}
	r.m.Cancel(g.ID)
	if _, ok := r.m.Get(g.ID); ok {
		t.Error("cancelled sign-in is still there")
	}
}

func TestClaudeLoginOddPrograms(t *testing.T) {
	ctx := context.Background()
	// A program that signs in by itself, without a link.
	bin, _ := fakeClaude(t, `echo "Login successful."`)
	l, err := startClaudeLogin(ctx, bin)
	if err != nil || l.link != "" || l.wait() != nil {
		t.Fatalf("no link, ok = %+v, %v", l, err)
	}
	// One that fails before any link.
	bin, _ = fakeClaude(t, `echo "Login failed: no network"; exit 1`)
	if _, err := startClaudeLogin(ctx, bin); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Errorf("failure = %v", err)
	}
	// One that fails without a word.
	bin, _ = fakeClaude(t, `exit 3`)
	if _, err := startClaudeLogin(ctx, bin); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("silent failure = %v", err)
	}
	// One that cannot start.
	if _, err := startClaudeLogin(ctx, filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing program started")
	}

	// A program that signs in by itself makes the start instant.
	r := newRig(t)
	bin, state := fakeClaude(t, `echo '{"loggedIn": false}'`)
	_ = state
	r.m.findClaude = func() (string, error) { return bin, nil }
	r.m.claudeLogin = func(context.Context, string) (claudeLogin, error) {
		return claudeLogin{wait: func() error { return nil }}, nil
	}
	if f, err := r.m.Start("claude-code", StartOptions{}); err != nil || f.Status != Connected {
		t.Errorf("instant = %+v, %v", f, err)
	}
	r.m.claudeLogin = func(context.Context, string) (claudeLogin, error) {
		return claudeLogin{}, errors.New("boom")
	}
	r.m.forgetClaudeStatus()
	if _, err := r.m.Start("claude-code", StartOptions{}); err == nil || err.Error() != "boom" {
		t.Errorf("failed start = %v", err)
	}
}

func TestClaudeStatusIsCachedAndOldProgramsAreTrusted(t *testing.T) {
	r := newRig(t)
	// An old claude without `auth status`: taken as signed in, as before.
	bin, _ := fakeClaude(t, `echo "unknown command"`)
	if in, known := r.m.claudeStatus(bin); !in || known {
		t.Errorf("old = %v %v", in, known)
	}
	bin2, state := fakeClaude(t, claudeScript)
	if in, known := r.m.claudeStatus(bin2); in || !known {
		t.Errorf("signed out = %v %v", in, known)
	}
	_ = os.WriteFile(state, nil, 0o600)
	// Cached until forgotten.
	if in, _ := r.m.claudeStatus(bin2); in {
		t.Error("not cached")
	}
	r.m.forgetClaudeStatus()
	if in, _ := r.m.claudeStatus(bin2); !in {
		t.Error("not refreshed")
	}
	// A program that does not run at all is not known either.
	if in, known := r.m.claudeStatus(filepath.Join(t.TempDir(), "nope")); !in || known {
		t.Errorf("missing = %v %v", in, known)
	}
}
