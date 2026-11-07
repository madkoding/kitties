package llmauth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
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

// runClaudeInstaller runs Anthropic's installer, its output written to out as it goes, and says
// why it failed in its last words.
func runClaudeInstaller(ctx context.Context, out *logTail) error {
	name, args := claudeInstaller()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if last := out.Last(); last != "" {
		return fmt.Errorf("%w: %s", err, last)
	}
	return err
}

// logTail keeps the last lines a program printed, readable while it runs: progress bars
// (carriage returns) keep their last state and terminal colours are dropped.
type logTail struct {
	mu    sync.Mutex
	lines []string
	cur   []byte
}

// logLines is how many lines the page is shown.
const logLines = 8

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func (l *logTail) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range p {
		switch b {
		case '\n':
			l.lines = append(l.lines, l.clean())
			if len(l.lines) > logLines {
				l.lines = l.lines[len(l.lines)-logLines:]
			}
			l.cur = l.cur[:0]
		case '\r':
			l.cur = l.cur[:0]
		default:
			if len(l.cur) < 1024 {
				l.cur = append(l.cur, b)
			}
		}
	}
	return len(p), nil
}

func (l *logTail) clean() string {
	return strings.TrimRight(ansi.ReplaceAllString(string(l.cur), ""), " \t")
}

// String is the last lines, the one still being written included.
func (l *logTail) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]string{}, l.lines...)
	if c := l.clean(); c != "" {
		out = append(out, c)
	}
	if len(out) > logLines {
		out = out[len(out)-logLines:]
	}
	return strings.Join(out, "\n")
}

// Last is the last line with words in it.
func (l *logTail) Last() string {
	lines := strings.Split(l.String(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
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
	log := &logTail{}
	return m.trackLog(ctx, cancel, Flow{Kind: kind, Method: "install"}, nil, log, func(ctx context.Context) (Credential, error) {
		defer m.forgetClaudeStatus()
		if err := m.installClaude(ctx, log); err != nil {
			return Credential{}, fmt.Errorf("the Claude Code installer failed (install it by hand from claude.com/product/claude-code): %w", err)
		}
		p, err := m.findClaude()
		if err != nil {
			return Credential{}, errors.New("the installer ended but claude is not where Kitties looks (~/.local/bin): restart Kitties or set KITTIES_CLAUDE_BIN")
		}
		return Credential{Kind: "claude-code", Who: p}, nil
	}), nil
}
