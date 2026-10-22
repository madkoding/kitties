package provider

// --- claude-code ---------------------------------------------------------------------------
//
// The claude-code kind thinks on the person's own Claude subscription by running the official,
// unmodified `claude` program they signed in to with `claude auth login`. Anthropic only allows a
// subscription through its own client, so kitties never sees nor keeps those credentials: it
// starts claude with no tools of its own, hands it the conversation and reads back the answer.
// The kitty's tools are described in the prompt (text mode), as for any server without native
// tool calls.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ClaudeClient answers through the claude program.
type ClaudeClient struct {
	opts Options
	// exec runs claude; tests answer without the program.
	exec func(ctx context.Context, bin string, args, env []string, dir string, stdin []byte) (stdout, stderr []byte, err error)
	find func() (string, error)
}

// ErrNoClaude is returned when the claude program is not installed.
var ErrNoClaude = errors.New("the claude program (Claude Code) is not installed: install it from claude.com/code, then run `claude auth login` in a terminal")

// NewClaude builds the client for a claude-code profile.
func NewClaude(opts Options) (*ClaudeClient, error) {
	if opts.Model == "" {
		opts.Model = kinds["claude-code"].DefaultModel
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4096
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	return &ClaudeClient{opts: opts, exec: runClaude, find: FindClaude}, nil
}

// Describe names the provider and model.
func (c *ClaudeClient) Describe() string { return "claude-code/" + c.opts.Model }

// Model is the configured model.
func (c *ClaudeClient) Model() string { return c.opts.Model }

// claudeHomes are where the installers put claude, for a desktop app whose PATH is the system's
// bare one (a macOS app started from the Dock sees /usr/bin:/bin:/usr/sbin:/sbin).
func claudeHomes() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".claude", "local"), filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin",
	}
	if runtime.GOOS == "windows" {
		dirs = append(dirs, filepath.Join(os.Getenv("APPDATA"), "npm"))
	}
	return dirs
}

// FindClaude returns the path of the claude program: KITTIES_CLAUDE_BIN, the PATH, or where its
// installers put it.
func FindClaude() (string, error) {
	if bin := os.Getenv("KITTIES_CLAUDE_BIN"); bin != "" {
		return exec.LookPath(bin)
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return filepath.Abs(p)
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.cmd"
	}
	for _, dir := range claudeHomes() {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", ErrNoClaude
}

// claudeEnv is the program's environment: this one without the variables that would make claude
// bill an API key instead of the subscription, with the installers' folders on the PATH (an npm
// claude is a node script), and kitties' limits.
func claudeEnv(bin string, maxTokens int) []string {
	env := []string{}
	path := ""
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "ANTHROPIC_"), strings.HasPrefix(name, "CLAUDE_CODE_USE_"):
			continue
		case name == "PATH":
			path = value
			continue
		}
		env = append(env, kv)
	}
	dirs := append([]string{filepath.Dir(bin)}, claudeHomes()...)
	if path != "" {
		dirs = append(dirs, path)
	}
	return append(env, "PATH="+strings.Join(dirs, string(os.PathListSeparator)),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "MAX_THINKING_TOKENS=0",
		fmt.Sprintf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=%d", maxTokens))
}

// claudeArgs are the flags of one turn: the model, the system prompt from a file, no tools, no
// settings, no slash commands and nothing kept once it ends.
func claudeArgs(model, systemFile string) []string {
	args := []string{"-p", "--model", model,
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--tools", "", "--setting-sources", "", "--strict-mcp-config", "--disable-slash-commands",
		"--max-turns", "1", "--permission-mode", "dontAsk", "--no-session-persistence"}
	if systemFile != "" {
		args = append(args, "--system-prompt-file", systemFile)
	}
	return args
}

// claudeFrame is the one stream-json user message a turn sends: the conversation as text, and the
// pictures of its last turn.
func claudeFrame(rest []Message) ([]byte, error) {
	if len(rest) == 0 {
		return nil, errors.New("claude-code needs a conversation")
	}
	blocks := []map[string]any{{"type": "text", "text": claudeTranscript(rest)}}
	last := rest[len(rest)-1]
	for _, img := range last.Images {
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": img.MIME, "data": base64.StdEncoding.EncodeToString(img.Data)}})
	}
	return json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": blocks}})
}

// claudeTranscript is the conversation as one message. A lone question goes as it is; a longer
// conversation is written out, oldest first, and the model answers its last turn. A leading "/"
// would run as a claude command, so it never starts the text.
func claudeTranscript(rest []Message) string {
	if len(rest) == 1 && rest[0].Role == "user" {
		return " " + rest[0].Content
	}
	var sb strings.Builder
	sb.WriteString(" The conversation so far, oldest first. Write only the assistant's next reply.\n")
	for _, m := range rest {
		switch m.Role {
		case "assistant":
			sb.WriteString("\n### Assistant (you)\n")
		case "tool":
			sb.WriteString("\n### Tool result\n")
		default:
			sb.WriteString("\n### User\n")
		}
		sb.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&sb, "\n@tool %s %s", tc.Name, argsJSON(tc.Args))
		}
		if len(m.Images) > 0 {
			fmt.Fprintf(&sb, "\n(%d picture(s) attached)", len(m.Images))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// claudeResult is the part of claude's last stream-json line that kitties reads.
type claudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   *struct {
		Input      int `json:"input_tokens"`
		CacheRead  int `json:"cache_read_input_tokens"`
		CacheWrite int `json:"cache_creation_input_tokens"`
		Output     int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete runs one turn of claude.
func (c *ClaudeClient) Complete(ctx context.Context, messages []Message) (Reply, error) {
	bin, err := c.find()
	if err != nil {
		return Reply{}, err
	}
	system, rest := splitSystem(messages)
	frame, err := claudeFrame(rest)
	if err != nil {
		return Reply{}, err
	}
	// A folder of its own: claude reports where it runs, and nothing of kitties' is there.
	dir, err := os.MkdirTemp("", "kitties-claude-")
	if err != nil {
		return Reply{}, err
	}
	defer os.RemoveAll(dir)
	systemFile := ""
	if system != "" {
		systemFile = filepath.Join(dir, "system.md")
		if err := os.WriteFile(systemFile, []byte(system), 0o600); err != nil {
			return Reply{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	stdout, stderr, runErr := c.exec(ctx, bin, claudeArgs(c.opts.Model, systemFile), claudeEnv(bin, c.opts.MaxTokens), dir, append(frame, '\n'))
	res, found := lastResult(stdout)
	switch {
	case found && res.IsError:
		return Reply{}, claudeError(res.Result + " " + res.Subtype)
	case !found && runErr != nil:
		return Reply{}, claudeError(fmt.Sprintf("%v: %s", runErr, clip(string(stderr), 300)))
	case !found:
		return Reply{}, errors.New("claude ended without an answer")
	}
	reply := Reply{Content: strings.TrimSpace(res.Result)}
	if u := res.Usage; u != nil {
		reply.Usage = Usage{Input: u.Input + u.CacheRead + u.CacheWrite, Output: u.Output}
	}
	if reply.Content == "" {
		return Reply{}, errors.New("claude returned an empty answer")
	}
	return reply, nil
}

// lastResult finds the result line of claude's output.
func lastResult(stdout []byte) (claudeResult, bool) {
	var res claudeResult
	found := false
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64<<10), 64<<20)
	for sc.Scan() {
		var ev claudeResult
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == "result" {
			res, found = ev, true
		}
	}
	return res, found
}

// claudeError explains a failed turn; a missing login says how to sign in.
func claudeError(msg string) error {
	msg = strings.TrimSpace(msg)
	low := strings.ToLower(msg)
	if strings.Contains(low, "login") || strings.Contains(low, "log in") || strings.Contains(low, "api key") || strings.Contains(low, "auth") {
		return &HTTPError{Code: 401, Body: "claude is not signed in to a Claude subscription: run `claude auth login` in a terminal, then try again (" + msg + ")"}
	}
	return &HTTPError{Code: 502, Body: "claude: " + msg}
}

// runClaude runs the program with stdin and returns what it printed.
func runClaude(ctx context.Context, bin string, args, env []string, dir string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
