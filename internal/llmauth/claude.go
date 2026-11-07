package llmauth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/provider"
)

// --- Claude (claude-code) ------------------------------------------------------------------
//
// A Claude plan is used only through Anthropic's own program, so the sign-in is that program's
// too: kitties runs `claude auth login`, shows the link it prints, and types into it the code
// claude.com shows once the person signs in. The redirect is Anthropic's page, not this
// machine, so it works from a browser anywhere (the LAN, a public address, a phone). Kitties
// never sees nor keeps the credentials: the program keeps them where it always does.

// claudeStatusTTL is how long the answer of `claude auth status` is trusted: it is a program
// start, and the settings page asks for the accounts often.
const claudeStatusTTL = 30 * time.Second

// claudeLinkWait is how long the program may take to print its link.
const claudeLinkWait = 30 * time.Second

// claudeLogin is a running `claude auth login`: the link to open, where the code is typed, and
// its end.
type claudeLogin struct {
	link string
	code io.WriteCloser
	// wait returns once the program ended, with the reason it failed.
	wait func() error
}

// claudeCache keeps the last answer of `claude auth status`.
type claudeCache struct {
	mu      sync.Mutex
	checked time.Time
	bin     string
	in      bool
	known   bool
}

// claudeStatus says whether the program at bin holds a Claude login. known is false when the
// program cannot tell (an old claude without `auth status`): then it is taken as signed in, as
// before this sign-in existed.
func (m *Manager) claudeStatus(bin string) (in, known bool) {
	r := &m.claude
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bin == bin && !r.checked.IsZero() && m.now().Sub(r.checked) < claudeStatusTTL {
		return r.in, r.known
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "auth", "status", "--json")
	cmd.Env = provider.ClaudeEnv(bin)
	out, _ := cmd.Output()
	var st struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	in, known = true, false
	if json.Unmarshal(out, &st) == nil && st.LoggedIn != nil {
		in, known = *st.LoggedIn, true
	}
	r.bin, r.in, r.known, r.checked = bin, in, known, m.now()
	return in, known
}

// forgetClaudeStatus makes the next question run the program again (after a sign-in).
func (m *Manager) forgetClaudeStatus() {
	m.claude.mu.Lock()
	m.claude.checked = time.Time{}
	m.claude.mu.Unlock()
}

// claudeURL finds the sign-in link in what the program prints.
var claudeURL = regexp.MustCompile(`https://\S+/oauth/authorize\?\S+`)

// startClaudeLogin runs `claude auth login --claudeai` and waits for the link it prints.
func startClaudeLogin(ctx context.Context, bin string) (claudeLogin, error) {
	cmd := exec.CommandContext(ctx, bin, "auth", "login", "--claudeai")
	env := provider.ClaudeEnv(bin)
	if runtime.GOOS != "windows" {
		// The page opens the link itself: no second tab on the gateway's machine (or none at all
		// on a server).
		env = append(env, "BROWSER=true")
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return claudeLogin{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return claudeLogin{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return claudeLogin{}, fmt.Errorf("claude could not start: %w", err)
	}
	links := make(chan string, 1)
	var mu sync.Mutex
	var last string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !sent {
				if u := claudeURL.FindString(line); u != "" {
					links <- u
					sent = true
					continue
				}
			}
			// The prompt shares its line with the answer: keep what follows it.
			if _, after, ok := strings.Cut(line, "Paste code here if prompted >"); ok {
				line = strings.TrimSpace(after)
			}
			if line != "" {
				mu.Lock()
				last = line
				mu.Unlock()
			}
		}
	}()
	wait := func() error {
		<-done
		err := cmd.Wait()
		if err == nil {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if last != "" {
			return errors.New(strings.TrimPrefix(last, "Login failed: "))
		}
		return fmt.Errorf("claude auth login failed: %w", err)
	}
	timer := time.NewTimer(claudeLinkWait)
	defer timer.Stop()
	select {
	case link := <-links:
		return claudeLogin{link: link, code: stdin, wait: wait}, nil
	case <-done:
		err := wait()
		if err == nil {
			// It signed in without asking (a login it found by itself).
			return claudeLogin{wait: func() error { return nil }}, nil
		}
		return claudeLogin{}, fmt.Errorf("claude did not give a sign-in link: %w", err)
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		_ = cmd.Wait()
		return claudeLogin{}, errors.New("claude did not give a sign-in link in time: run `claude auth login` in a terminal")
	}
}

// startClaude signs the claude program in: done at once when it already is, else the program's
// own link and the code pasted back.
func (m *Manager) startClaude() (Flow, error) {
	bin, err := m.findClaude()
	if err != nil {
		return Flow{}, err
	}
	if in, _ := m.claudeStatus(bin); in {
		return Flow{ID: randomText(12), Kind: "claude-code", Method: "instant", Status: Connected, Who: bin}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), flowTTL)
	login, err := m.claudeLogin(ctx, bin)
	if err != nil {
		cancel()
		return Flow{}, err
	}
	if login.link == "" {
		cancel()
		m.forgetClaudeStatus()
		return Flow{ID: randomText(12), Kind: "claude-code", Method: "instant", Status: Connected, Who: bin}, nil
	}
	paste := make(chan string, 1)
	return m.trackWith(ctx, cancel, Flow{Kind: "claude-code", Method: "browser", URL: login.link, Paste: true}, paste,
		func(ctx context.Context) (Credential, error) {
			defer m.forgetClaudeStatus()
			ended := make(chan error, 1)
			go func() { ended <- login.wait() }()
			for {
				select {
				case <-ctx.Done():
					_ = login.code.Close()
					<-ended
					return Credential{}, ctx.Err()
				case err := <-ended:
					if err != nil {
						return Credential{}, fmt.Errorf("Claude did not sign in: %w", err)
					}
					// Nothing is kept here: the program holds its own login.
					return Credential{Kind: "claude-code", Who: bin}, nil
				case text := <-paste:
					code := strings.TrimSpace(text)
					if code == "" {
						continue
					}
					if _, err := io.WriteString(login.code, code+"\n"); err != nil {
						return Credential{}, fmt.Errorf("claude stopped waiting for the code: %w", err)
					}
				}
			}
		}), nil
}
