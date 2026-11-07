package llmauth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// --- Installing Claude Code ----------------------------------------------------------------
//
// A Claude plan needs the claude program where the gateway runs. From a browser on another
// machine there is no terminal at hand, so the page can ask the gateway to run Anthropic's own
// installer (the one claude.com/product/claude-code gives): it puts claude in ~/.local/bin, one
// of the folders FindClaude looks in.

// claudeInstallTTL is how long the installer may take.
const claudeInstallTTL = 10 * time.Minute

// claudeInstaller is the command that installs claude on this system.
func claudeInstaller() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://claude.ai/install.ps1 | iex"}
	}
	return "sh", []string{"-c", "curl -fsSL https://claude.ai/install.sh | bash"}
}

// runClaudeInstaller runs Anthropic's installer and says why it failed, in its last words.
func runClaudeInstaller(ctx context.Context) error {
	name, args := claudeInstaller()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return fmt.Errorf("%w: %s", err, last)
	}
	return err
}

// Install installs the program kind signs in with (only claude-code has one). It is a flow the
// page follows: connected once the program is found (at once when it already is).
func (m *Manager) Install(kind string) (Flow, error) {
	if kind != "claude-code" {
		return Flow{}, fmt.Errorf("%s has no program to install", kind)
	}
	if p, err := m.findClaude(); err == nil {
		return Flow{ID: randomText(12), Kind: kind, Method: "install", Status: Connected, Who: p}, nil
	}
	m.prune()
	m.mu.Lock()
	for _, f := range m.flows {
		if f.Kind == kind && f.Method == "install" && f.Status == Pending {
			m.mu.Unlock()
			return f.Flow, nil
		}
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), claudeInstallTTL)
	return m.trackWith(ctx, cancel, Flow{Kind: kind, Method: "install"}, nil, func(ctx context.Context) (Credential, error) {
		defer m.forgetClaudeStatus()
		if err := m.installClaude(ctx); err != nil {
			return Credential{}, fmt.Errorf("the Claude Code installer failed (install it by hand from claude.com/product/claude-code): %w", err)
		}
		p, err := m.findClaude()
		if err != nil {
			return Credential{}, errors.New("the installer ended but claude is not where Kitties looks (~/.local/bin): restart Kitties or set KITTIES_CLAUDE_BIN")
		}
		return Credential{Kind: "claude-code", Who: p}, nil
	}), nil
}
