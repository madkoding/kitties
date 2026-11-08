// Command kitties orchestrates a fleet of autonomous agents, each with its own soul, its own AI
// provider and its own isolated container.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/attach"
	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/cloud"
	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/demo"
	"github.com/madkoding/kitties/internal/digest"
	"github.com/madkoding/kitties/internal/gateway"
	"github.com/madkoding/kitties/internal/guard"
	"github.com/madkoding/kitties/internal/keep"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/mcp"
	"github.com/madkoding/kitties/internal/memory"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/reply"
	"github.com/madkoding/kitties/internal/selfupdate"
	"github.com/madkoding/kitties/internal/shared"
	"github.com/madkoding/kitties/internal/skills"
	"github.com/madkoding/kitties/internal/soul"
	"github.com/madkoding/kitties/internal/tidy"
	"github.com/madkoding/kitties/internal/vault"
	"github.com/madkoding/kitties/internal/web"
)

// version is set by the linker (-X main.version=...).
var version = "dev"

const usage = `kitties %s: a fleet of autonomous agents, each with its own soul and desktop.

Usage: kitties [-config file] <command> [arguments]

Commands:
  init [-force]             write an example configuration (and a sample soul) to start from
  gateway [-listen addr]    bootstrap the fleet and serve the HTTP API and the web interface
  gateway start|stop|restart|status|logs
                            manage the systemd service installed by scripts/install.sh
  expose [local|lan|wan]    where the gateway can be reached from: this machine, the local network or
                            every network (-yes skips the question of wan); alone, it shows it
  config [set <key> <value> | unset <key>]
                            show or change the gateway: port, public_url, trust_proxy
  token [rotate]            print the gateway's token (to sign in to the web interface), or replace it
  mcp [-url u] [-token t]   MCP over stdio for Claude, bridged to a gateway's /mcp (docs/MCP.md)
  ls                        list the kitties and their state
  create <name> [flags]     create a kitty (-provider, -persona, -soul file, -avatar breed/color, -no-desktop)
  start|stop|rm <name>      manage one kitty
  lead [<name> | none]      show the lead kitty (the one that orchestrates the others and reports for
                            all of them), make another the lead, or leave the fleet without one
  scale <name> <n>          run n replicas of a configured kitty (name-1 ... name-n)
  chat [<name>[,<name>...]] talk to one kitty, or to a group of them, from the terminal (no name: the lead)
  exec <name> <cmd...>      run a command inside the kitty's container
  shot <name> <file.png>    save a screenshot of the kitty's display
  providers                 list the configured AI profiles and the supported kinds
  version                   print the version
  release-key | release-sign <file> | release-verify <file> <sig> <key>
                            the signing of the releases (for whoever makes them)

Flags: -config file picks the configuration; -demo needs no key and no Docker (a scripted brain
and a drawn desktop) and is the quickest way to see the interface: kitties -demo gateway.

Environment: KITTIES_CONFIG names the configuration file (default ./kitties.yaml or ~/.kitties/kitties.yaml).
A gateway without a configuration file starts empty: add models and kitties from the web page.
For a server, KITTIES_LISTEN, KITTIES_TOKEN, KITTIES_PUBLIC_URL, KITTIES_DATA_DIR and KITTIES_TRUST_PROXY
override the gateway block, which is how a container is configured (see docs/DEPLOY.md).
With the service of scripts/install.sh installed, expose, config and token change the service (through
sudo); -config file changes a gateway of your own. They need no AI key.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// app is the state every command shares.
type app struct {
	demo   bool
	cfg    config.Config
	fleet  *orchestrator.Manager
	chats  *chat.Service
	agenda *agenda.Agenda
	// approvals hold the actions that wait for a yes; memory is what the kitties know of the
	// person; notifier tells the person outside the interface.
	approvals *approvals.Store
	vault     *vault.Store
	keep      *keep.Store
	updater   *selfupdate.Updater
	// booted is what the start found out about the last update (rolled back, confirmed).
	booted selfupdate.BootResult
	budget *budget.Store
	digest *digest.Reporter
	// summaries asks the lead to sum its reports up every day (autonomy.lead_summary).
	summaries *chat.LeadSummaries
	memory    *memory.Store
	notifier  *notify.Notifier
	skills    *skills.Library
	arts      *artifacts.Store
	// guard keeps the exposed kitties from giving the person away; tokens are the gateway's own
	// secrets it takes out too.
	guard    *guard.Guard
	tokensMu sync.Mutex
	tokens   []string
	in       io.Reader
	out      io.Writer
	err      io.Writer
}

// Seams for the tests.
var (
	detectRuntime = container.Detect
	newContainers = func(rt container.Runtime) orchestrator.Containers { return container.New(rt) }
	getenv        = os.Getenv
	webFS         = web.FS
	notifyContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
)

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("kitties", flag.ContinueOnError)
	fs.SetOutput(errOut)
	configPath := fs.String("config", "", "configuration file (default: KITTIES_CONFIG, ./kitties.yaml or ~/.kitties/kitties.yaml)")
	demoMode := fs.Bool("demo", false, "no AI key and no Docker: a scripted brain and a drawn desktop, to try the interface")
	fs.Usage = func() { fmt.Fprintf(errOut, usage, version) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return 2
	}
	cmd, rest := rest[0], rest[1:]
	switch cmd {
	case "version":
		fmt.Fprintln(out, "kitties", version)
		return 0
	case "init":
		return cmdInit(*configPath, rest, out, errOut)
	case "expose", "config", "token":
		if code := cmdSetup(cmd, rest, *configPath, args, in, out, errOut); code >= 0 {
			return code
		}
	case "release-key", "release-sign", "release-verify":
		return cmdRelease(cmd, rest, out, errOut)
	case "help", "-h", "--help":
		fs.Usage()
		return 0
	case "gateway":
		if len(rest) > 0 && isServiceVerb(rest[0]) {
			return cmdGatewayService(rest[0], rest[1:], in, out, errOut)
		}
	}
	if !fleetCommands[cmd] {
		fmt.Fprintf(errOut, "kitties: unknown command %q (run `kitties help`)\n", cmd)
		return 1
	}
	// A gateway that was just updated is counted before anything else can go wrong (even reading
	// the configuration): one that does not hold goes back to the version before.
	var booted selfupdate.BootResult
	if cmd == "gateway" {
		var berr error
		if booted, berr = selfupdate.New(config.Defaults().Updates, version).Boot(); berr != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", berr)
		}
	}
	path := *configPath
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	switch {
	case err != nil && *demoMode && *configPath == "":
		// No file of their own: the demo brings its kitties. A file that was named must load.
		cfg, err = demoConfig()
	case err != nil && errors.Is(err, os.ErrNotExist) && *configPath == "" && os.Getenv("KITTIES_CONFIG") == "" && (cmd == "gateway" || cmd == "token" || cmd == "mcp"):
		// A server with no file yet: it starts empty and is set up from the web page.
		cfg, err = config.Defaults(), nil
		if cmd == "gateway" {
			fmt.Fprintf(errOut, "kitties: no configuration file (%s): starting empty; add models and kitties from the web page\n", path)
		}
	}
	if err == nil {
		err = cfg.ApplyEnv(os.Getenv)
	}
	if err == nil && cmd == "gateway" {
		// What the person chose in Settings or an installer (local, lan, wan) wins over the file and the environment.
		cfg.ApplyExposure()
		cfg.ApplyMCPSettings()
	}
	if err != nil {
		fmt.Fprintf(errOut, "kitties: %v\nRun `kitties init` to write an example configuration.\n", err)
		return 1
	}
	if cmd == "token" {
		return cmdToken(cfg, out, errOut)
	}
	if cmd == "mcp" {
		return cmdMCP(rest, cfg, in, out, errOut)
	}
	a := &app{cfg: cfg, in: in, out: out, err: errOut, demo: *demoMode, booted: booted}
	if err := a.open(cmd); err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	if err := a.dispatch(cmd, rest); err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	return 0
}

// cloudRouter sends each kitty's desktop to the local engine or to the cloud provider, as the
// configuration says. A side that cannot work (no engine, no API key) fails only its own kitties,
// with the reason; the note says what is missing. Nothing here runs unless a desktop is set to the
// cloud.
func cloudRouter(cfg config.Config, env func(string) string, local orchestrator.Containers, localErr error) (*cloud.Router, string) {
	r := &cloud.Router{IsCloud: cfg.IsCloudContainer, LocalErr: localErr}
	if local != nil {
		r.Local = local
	}
	var notes []string
	if localErr != nil {
		notes = append(notes, localErr.Error()+"; local desktops will not start")
	}
	key := env(cfg.Cloud.APIKeyEnv)
	if key == "" {
		r.CloudErr = fmt.Errorf("cloud: set %s to the %s API key", cfg.Cloud.APIKeyEnv, cfg.Cloud.Provider)
		notes = append(notes, r.CloudErr.Error()+"; cloud desktops will not start")
		return r, strings.Join(notes, "; ")
	}
	deployment := cfg.Cloud.Deployment
	if deployment == "" {
		sum := sha256.Sum256([]byte(cfg.DataDirOr()))
		deployment = "kitties-" + hex.EncodeToString(sum[:5])
	}
	r.Cloud = &cloud.Containers{
		P:    &cloud.Boat{Key: key, Deployment: deployment, MaxRunning: cfg.Cloud.MaxRunning},
		Size: cfg.Cloud.Size,
	}
	return r, strings.Join(notes, "; ")
}

// open builds the orchestrator and the chat service; the runtime is probed except for the
// commands that never touch a container.
func (a *app) open(cmd string) error {
	var opts []orchestrator.Option
	if a.demo {
		reg := provider.NewRegistry()
		reg.Add("demo", demo.NewBrain(), provider.Options{Kind: "demo", Model: "scripted"})
		for name := range a.cfg.Providers {
			reg.Add(name, demo.NewBrain(), provider.Options{Kind: "demo", Model: "scripted"})
		}
		opts = append(opts, orchestrator.WithProviders(reg), orchestrator.WithContainers(demo.NewDesktop(), nil))
		if a.cfg.DefaultProvider == "" {
			a.cfg.DefaultProvider = "demo"
		}
		a.cfg.DataDir = filepath.Join(a.cfg.DataDirOr(), "demo")
		fmt.Fprintln(a.err, "kitties: demo mode, a scripted brain and a drawn desktop; nothing here is real")
	} else if cmd != "providers" && a.cfg.CloudNeeded() {
		rt, lerr := detectRuntime(context.Background(), a.cfg.Containers.Runtime)
		var local orchestrator.Containers
		if lerr == nil {
			local = newContainers(rt)
		}
		router, note := cloudRouter(a.cfg, getenv, local, lerr)
		if note != "" {
			fmt.Fprintf(a.err, "kitties: %s\n", note)
		}
		opts = append(opts, orchestrator.WithContainers(router, nil))
	} else if cmd != "providers" {
		rt, err := detectRuntime(context.Background(), a.cfg.Containers.Runtime)
		if err != nil {
			fmt.Fprintf(a.err, "kitties: %v; the kitties run brain-only\n", err)
			opts = append(opts, orchestrator.WithContainers(nil, err), orchestrator.WithRuntimeRetry(func(ctx context.Context) (orchestrator.Containers, error) {
				rt, err := detectRuntime(ctx, a.cfg.Containers.Runtime)
				if err != nil {
					return nil, err
				}
				return newContainers(rt), nil
			}))
		} else {
			opts = append(opts, orchestrator.WithContainers(newContainers(rt), nil))
		}
	}
	fleet, err := orchestrator.New(a.cfg, opts...)
	if err != nil {
		return err
	}
	for _, p := range fleet.Settings().Providers {
		// Only where a brain is used: ls, stop or rm need no key, and say nothing about one.
		if p.Error != "" && brainCommands[cmd] {
			fmt.Fprintf(a.err, "kitties: provider %q is not available: %s\n", p.Name, p.Error)
		}
	}
	chats, err := chat.New(fleet, filepath.Join(a.cfg.DataDirOr(), "chats"))
	if err != nil {
		return err
	}
	if u := os.Getenv("USER"); u != "" {
		chats.SetUser(u)
	}
	a.skills = skills.New(filepath.Join(a.cfg.DataDirOr(), "skills"))
	a.arts = artifacts.New(filepath.Join(a.cfg.DataDirOr(), "artifacts"))
	chats.SetLibrary(a.skills, a.arts)
	ag, err := agenda.Open(a.cfg.DataDirOr(), chats)
	if err != nil {
		return err
	}
	chats.SetAgenda(ag.Journal, ag)
	// How long a kitty keeps at a task on its own follows the settings as they change.
	ag.Scheduler.SetFollowUps(func() []time.Duration { return fleet.Autonomy().FollowUpWaits(agenda.DefaultFollowUps) })
	chats.SetBrainWait(func() time.Duration { return fleet.Autonomy().BrainWaitOr(chat.DefaultBrainWait) })
	chats.SetStandIns(func() int { return fleet.Autonomy().StandInsOr(chat.DefaultStandIns) })
	chats.SetLeadLimitsAsk(func() bool { return fleet.Autonomy().LeadLimitsAsk() })
	chats.SetParallel(func() int { return fleet.Autonomy().ParallelOr(chat.DefaultParallel) })
	a.agenda = ag
	dataDir := a.cfg.DataDirOr()
	if a.approvals, err = approvals.Open(filepath.Join(dataDir, "approvals.json")); err != nil {
		return err
	}
	if a.budget, err = budget.Open(filepath.Join(dataDir, "budget.json")); err != nil {
		return err
	}
	a.memory = memory.Open(filepath.Join(dataDir, "memory"))
	// The saved secrets and the backups of the kitties' sessions need a key kept in the data
	// folder: where it cannot be written (a read-only volume) they are off, and the rest runs.
	if a.vault, a.keep = openSecretStores(dataDir, a.err); a.vault != nil {
		chats.SetVault(a.vault)
		fleet.SetKeeper(a.keep)
	}
	chats.SetBudget(a.budget)
	chats.SetApprovals(a.approvals)
	chats.SetMemory(a.memory)
	if a.guard, err = guard.Open(filepath.Join(dataDir, "guard"), a.secretValues); err != nil {
		return err
	}
	chats.SetGuard(a.guard)
	g := a.cfg.Gateway
	a.notifier = notify.New(fleet.Connectors(), fleet.Notify, func() string {
		if g.PublicURL != "" {
			return g.PublicURL
		}
		return "http://" + g.Listen
	}, nil)
	chats.SetNotifier(a.notifier)
	a.digest = digest.New(digest.Source{
		Kitties: func() []digest.Kitty {
			var out []digest.Kitty
			for _, k := range fleet.List() {
				out = append(out, digest.Kitty{ID: k.ID, Name: k.Name})
			}
			return out
		},
		Journal: ag.Journal.Recent,
		Waiting: func() []string {
			var out []string
			for _, p := range a.approvals.List(approvals.Pending, 50) {
				out = append(out, p.KittyName+": "+p.Summary)
			}
			return out
		},
		Today:  a.budget.Today,
		Config: fleet.Notify,
		Deliver: func(ctx context.Context, n notify.Notification) error {
			return a.notifier.Deliver(ctx, chats.LeadDigest(n))
		},
		Plans: chats.Plans,
		Day:   digest.MyDay(fleet.Connectors()),
	}, filepath.Join(dataDir, "digest.json"))
	a.summaries = &chat.LeadSummaries{
		Chats: chats,
		At:    func() (int, int, bool) { return fleet.Autonomy().LeadSummaryAt() },
		Deliver: func(ctx context.Context, n notify.Notification) error {
			if fleet.Notify().Connector == "" {
				return nil // the summary stays in the Reports thread
			}
			return a.notifier.Deliver(ctx, n)
		},
		Path: filepath.Join(dataDir, "lead-summary.json"),
	}
	// A removed kitty takes its held actions and its memory along.
	ag.OnForget = append(ag.OnForget, a.approvals.Forget, a.memory.ForgetKitty, a.budget.Forget, chats.LeaveChats)
	if a.vault != nil {
		ag.OnForget = append(ag.OnForget, a.vault.ForgetKitty, a.keep.Forget)
	}
	// Chats whose kitties were removed earlier have nobody to answer: take those kitties out,
	// and drop the chats left empty.
	if err := chats.Prune(func(id string) bool { _, err := fleet.Get(id); return err == nil }); err != nil {
		fmt.Fprintf(a.err, "kitties: tidying the chats: %v\n", err)
	}
	a.fleet, a.chats = fleet, chats
	a.updater = selfupdate.New(a.cfg.Updates, version)
	a.updater.Idle = func() bool { return !a.chats.AnyBusy() && !a.fleet.AnyHeld() }
	return nil
}

// secretValues are the secrets the gateway knows now, for the guardrails to take out of what
// reaches other people.
func (a *app) secretValues() []string {
	var out []string
	if a.fleet != nil {
		out = append(out, a.fleet.SecretValues()...)
	}
	if a.vault != nil {
		out = append(out, a.vault.Values()...)
	}
	a.tokensMu.Lock()
	out = append(out, a.tokens...)
	a.tokensMu.Unlock()
	return out
}

// keepSecret adds one of the gateway's own secrets (its token) to what the guardrails take out.
func (a *app) keepSecret(v string) {
	if v == "" {
		return
	}
	a.tokensMu.Lock()
	a.tokens = append(a.tokens, v)
	a.tokensMu.Unlock()
}

// fleetCommands are the commands that open the fleet; brainCommands the ones of them that talk to
// the AI profiles, where a profile that cannot work is said.
var (
	fleetCommands = map[string]bool{"gateway": true, "token": true, "mcp": true, "ls": true, "providers": true, "create": true,
		"start": true, "stop": true, "rm": true, "scale": true, "lead": true, "chat": true, "exec": true, "shot": true}
	brainCommands = map[string]bool{"gateway": true, "providers": true, "create": true, "start": true, "scale": true, "chat": true}
)

func (a *app) dispatch(cmd string, args []string) error {
	ctx, cancel := notifyContext()
	defer cancel()
	switch cmd {
	case "gateway":
		return a.cmdGateway(ctx, args)
	case "ls":
		return a.cmdList()
	case "providers":
		return a.cmdProviders()
	case "create":
		return a.cmdCreate(ctx, args)
	case "start", "stop", "rm":
		return a.cmdLifecycle(ctx, cmd, args)
	case "scale":
		return a.cmdScale(ctx, args)
	case "lead":
		return a.cmdLead(args)
	case "chat":
		return a.cmdChat(ctx, args)
	case "exec":
		return a.cmdExec(ctx, args)
	case "shot":
		return a.cmdShot(ctx, args)
	default:
		return fmt.Errorf("unknown command %q (run `kitties help`)", cmd)
	}
}

// demoConfig is the configuration of a demo without a file: one kitty of each kind.
func demoConfig() (config.Config, error) {
	cfg := config.Defaults()
	cfg.Providers = map[string]config.Provider{"demo": {Kind: "custom", Model: "scripted", BaseURL: "http://127.0.0.1:1"}}
	cfg.DefaultProvider = "demo"
	cfg.Kitties = []config.Kitty{
		{Name: "mimi", Soul: soul.Soul{Persona: "a patient researcher who loves to explain", Tone: "warm and precise", Traits: []string{"curious", "thorough"}, Skills: []string{"research", "summaries"}}},
		{Name: "tom", Soul: soul.Soul{Persona: "a hands-on engineer who tries things on the desktop before answering", Tone: "friendly and direct", Skills: []string{"browsing", "testing web apps"}}},
		{Name: "luna", Lead: true, Soul: soul.Soul{Persona: "a planner who keeps the team on track", Tone: "calm and structured", Skills: []string{"planning", "reviews"}}},
	}
	return cfg, cfg.Validate()
}

func cmdInit(path string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	force := fs.Bool("force", false, "write the example over the configuration there, keeping a copy in <file>.bak")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if path == "" {
		path = "kitties.yaml"
	}
	abs, _ := filepath.Abs(path)
	service := serviceExists()
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(out, "kitties: %s already exists, not touching it.\n", abs)
		fmt.Fprintln(out, "  See or change the gateway: kitties config, kitties expose local|lan|wan, kitties token")
		fmt.Fprintf(out, "  Start again from the example: kitties init -force (keeps a copy in %s.bak)\n", path)
		fmt.Fprintln(out, "  Write another file: kitties -config other.yaml init")
		initServiceNote(out, service)
		return 0
	} else if err == nil {
		if err := os.Rename(path, path+".bak"); err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Kept the old configuration in %s.bak.\n", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(exampleConfig), 0o600); err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	soulPath := filepath.Join(filepath.Dir(path), "souls", "tom.yaml")
	_ = os.MkdirAll(filepath.Dir(soulPath), 0o700)
	if _, err := os.Stat(soulPath); err != nil {
		_ = os.WriteFile(soulPath, []byte(exampleSoul), 0o600)
	}
	fmt.Fprintf(out, "Wrote %s and %s.\nEdit the providers (keys come from OPENAI_API_KEY, ANTHROPIC_API_KEY...), then run: kitties gateway\n", path, soulPath)
	initServiceNote(out, service)
	return 0
}

// initServiceNote says that the installed service does not read the file init writes.
func initServiceNote(out io.Writer, service bool) {
	if service {
		fmt.Fprintf(out, "The service of scripts/install.sh does not read this file: it is set up with kitties config and kitties expose (settings in %s).\n", serviceEnvFile)
	}
}

// gatewayToken is the token the gateway uses: the configured one, else the one a previous run
// left in the data directory (so a restart does not sign everybody out), else a new one. It is
// kept in the data directory where a program on this machine finds it.
func gatewayToken(cfg config.Config) (token, path string, fresh bool) {
	path = filepath.Join(cfg.DataDirOr(), "gateway.token")
	if cfg.Gateway.Token != "" {
		return cfg.Gateway.Token, path, false
	}
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); len(t) >= 16 {
			return t, path, false
		}
	}
	return gateway.NewToken(), path, true
}

// mcpToken is the token of the MCP connector, "" when it is off: the configured one, else the one
// kept in the data directory (made on the first run), so a restart keeps the connection.
func mcpToken(cfg config.Config) (token, path string, err error) {
	m := cfg.Gateway.MCP
	if !m.Enabled {
		return "", "", nil
	}
	path = filepath.Join(cfg.DataDirOr(), "mcp.token")
	if m.Token != "" {
		return m.Token, path, nil
	}
	if data, rerr := os.ReadFile(path); rerr == nil {
		if t := strings.TrimSpace(string(data)); len(t) >= 16 {
			return t, path, nil
		}
	}
	token = gateway.NewToken()
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		err = os.WriteFile(path, []byte(token+"\n"), 0o600)
	}
	return token, path, err
}

// cmdMCP is the stdio side of the connector, for clients that start a command (`claude mcp add
// kitties -- kitties mcp`): it forwards to the gateway's /mcp with the connector's token, from
// -token, KITTIES_MCP_TOKEN or the token file of this configuration's data directory.
func cmdMCP(args []string, cfg config.Config, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)
	url := fs.String("url", "", "the gateway's MCP address (default http://<gateway.listen>/mcp)")
	token := fs.String("token", "", "the connector's token (default KITTIES_MCP_TOKEN or <data>/mcp.token)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" {
		// This machine's own address: the bridge runs where the gateway does, unless -url says.
		g := cfg.Gateway
		for _, c := range mcpURLs(g.Listen, "", g.TLSCert != "", "") {
			if *url == "" || c.Where == "local" {
				*url = c.URL
			}
		}
		if g.PublicURL != "" {
			*url = strings.TrimRight(g.PublicURL, "/") + gateway.MCPPath
		}
	}
	tok := *token
	if tok == "" {
		tok = cfg.Gateway.MCP.Token
	}
	if tok == "" {
		if data, err := os.ReadFile(filepath.Join(cfg.DataDirOr(), "mcp.token")); err == nil {
			tok = strings.TrimSpace(string(data))
		}
	}
	if tok == "" {
		fmt.Fprintf(errOut, "kitties: no MCP token: turn the connector on (gateway.mcp.enabled) and start the gateway once, or pass -token\n")
		return 1
	}
	ctx, cancel := notifyContext()
	defer cancel()
	if err := mcp.Bridge(ctx, in, out, *url, tok, nil); err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	return 0
}

// cmdToken prints the token a gateway of this configuration uses, for signing in to the web page.
func cmdToken(cfg config.Config, out, errOut io.Writer) int {
	token, path, fresh := gatewayToken(cfg)
	if fresh {
		// The service scripts/install.sh sets up keeps its own: `sudo kitties token` reads it.
		if t := serviceToken(); t != "" {
			fmt.Fprintln(out, t)
			return 0
		}
		fmt.Fprintf(errOut, "kitties: no token yet (%s); start the gateway once and it makes one (for the service: sudo kitties token)\n", path)
		return 1
	}
	fmt.Fprintln(out, token)
	return 0
}

// serviceTokenFiles are where the service of scripts/install.sh keeps its token: the gateway's
// file, then the env file of an older install. A variable for the tests.
var serviceTokenFiles = []string{"/var/lib/kitties/gateway.token", "/etc/kitties/kitties.env"}

// serviceToken is the token of the installed service, "" when there is none or it cannot be read
// (it can only by root or the kitties user).
func serviceToken() string {
	for _, f := range serviceTokenFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasSuffix(f, ".env") {
				if !strings.HasPrefix(line, "KITTIES_TOKEN=") {
					continue
				}
				line = strings.TrimPrefix(line, "KITTIES_TOKEN=")
			}
			if len(line) >= 16 {
				return line
			}
		}
	}
	return ""
}

func (a *app) cmdGateway(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	fs.SetOutput(a.err)
	listen := fs.String("listen", "", "address to listen on (default gateway.listen; 0.0.0.0:7488 serves the network)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Getenv("KITTIES_RELAUNCH") != "" {
		// A copy that replaced the one that was running (Windows cannot exec in place): the port is freed first.
		time.Sleep(1500 * time.Millisecond)
	}
	if *listen != "" {
		a.cfg.Gateway.Listen = *listen
		if err := a.cfg.Validate(); err != nil {
			return err
		}
	}
	firstLAN := ""
	if *listen == "" && !inContainer() {
		// A gateway that never ran opens to the local network, behind its token, so it can be set
		// up from another computer; Settings takes it back to this machine alone.
		var err error
		if firstLAN, err = a.cfg.FirstStartLAN(lanIP()); err != nil {
			fmt.Fprintf(a.err, "kitties: could not open the first start to the local network: %v\n", err)
		}
	}
	if err := a.fleet.Bootstrap(ctx); err != nil {
		fmt.Fprintf(a.err, "kitties: some kitties did not start: %v\n", err)
	}
	if err := a.syncGoals(); err != nil {
		fmt.Fprintf(a.err, "kitties: goals from the configuration: %v\n", err)
	}
	token, tokenPath, _ := gatewayToken(a.cfg)
	a.keepSecret(token)
	// The token is left where a local program finds it (the desktop app, a script of the
	// same user), so a local executable never asks for it.
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err == nil {
		if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
			fmt.Fprintf(a.err, "kitties: could not keep the token in %s: %v\n", tokenPath, err)
		}
	}
	printWelcome(a.out, version, a.cfg.Gateway.Listen, a.cfg.Gateway.PublicURL, a.cfg.Gateway.TLSCert != "", token, tokenPath)
	if firstLAN != "" {
		printFirstStartLAN(a.out, firstLAN)
	}
	for _, k := range a.fleet.List() {
		fmt.Fprintf(a.out, "  %-16s %-8s %s\n", k.Name, k.Status, k.Provider)
	}
	// A connection that signs in but has no account (one from the file, or a sign-in never
	// finished) would fail at its first use: it is said now.
	for _, c := range a.fleet.Settings().Connectors {
		if c.NeedsSignIn {
			fmt.Fprintf(a.err, "kitties: the connection %s (%s) has no account yet: sign in from Settings, Connections, or give it a key\n", c.Name, c.Label)
		}
	}
	g := a.cfg.Gateway
	srv := gateway.New(a.fleet, a.chats, token, version).WithLibrary(a.skills, a.arts).WithAgenda(a.agenda).
		WithApprovals(a.chats).WithMemory(a.chats).WithMaintenance(a.fleet).WithUpdater(a.updater).WithNotifier(a.notifier).WithDigest(a.digest).
		WithExposure(exposureOf(a.cfg, g.Listen)).WithMCPControl(mcpControlOf(a.cfg)).WithPublicURL(g.PublicURL).WithProxy(g.TrustProxy).WithLocalToken(g.LocalTokenAllowed()).WithTLS(g.TLSCert, g.TLSKey)
	if a.vault != nil {
		srv.WithVault(a.vault)
	}
	if a.guard != nil {
		srv.WithGuard(a.guard)
	}
	srv.WithShared(shared.New(a.fleet.SharedRoot()), a.fleet.SharedQuotaMB())
	// What grows by itself (the screenshots of the tasks, the desktop images updates leave) is
	// swept on a schedule; Settings shows where the disk goes.
	janitor := &tidy.Janitor{Dir: a.cfg.DataDirOr(), Chats: a.chats, Settings: a.fleet, Images: a.fleet}
	srv.WithStorage(janitor)
	if wa := a.whatsappBridge(a.err, func(format string, args ...any) { fmt.Fprintf(a.out, format+"\n", args...) }); wa != nil {
		wa.Start(ctx)
		defer wa.Stop()
		srv.WithWhatsApp(wa)
	}
	if mt, mpath, err := mcpToken(a.cfg); err != nil {
		fmt.Fprintf(a.err, "kitties: the MCP connector stays off, its token cannot be kept: %v\n", err)
	} else if mt != "" {
		a.keepSecret(mt)
		srv.WithMCPOptions(mt, gateway.MCPOptions{Exec: g.MCP.AllowExec, OAuth: g.MCP.OAuth, Approve: g.MCP.AllowApprove,
			ClientsFile: filepath.Join(a.cfg.DataDirOr(), "mcp-clients.json")})
		// Every address it answers at (they follow the exposure), and the line for Claude Code.
		info := mcpControlOf(a.cfg).MCP()
		fmt.Fprintf(a.out, "MCP connector: %s (token in %s)\n", info.URL, mpath)
		for _, c := range info.Connect[1:] {
			fmt.Fprintf(a.out, "               %s\n", c.URL)
		}
		fmt.Fprintf(a.out, "  Claude Code: %s\n", claudeCommand(info.URL, "<token in "+mpath+">"))
	}
	if site, ok := webFS(); ok {
		srv.WithWeb(site)
	} else {
		fmt.Fprintln(a.err, "kitties: this build has no web interface inside (make web); serving the built-in page")
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(a.out, format+"\n", args...)
	}
	// The signed-in connectors keep their tokens fresh in the background; the scheduler wakes
	// the kitties up for their goals; the watchdog keeps their containers up.
	go a.fleet.KeepTokensFresh(ctx, logf)
	a.agenda.Scheduler.SetLog(logf)
	a.notifier.SetLog(logf)
	go a.agenda.Run(ctx)
	go a.digest.Run(ctx, logf)
	go a.summaries.Run(ctx, logf)
	// A kitty that stopped on its day's tokens goes on by itself once they are back.
	go a.chats.RunResumer(ctx, time.Minute)
	go janitor.Run(ctx, tidyFirst, tidyEvery, logf)
	// With notify.replies on, what the person writes to the ntfy topic reaches the kitties.
	replies := reply.New(a.chats, a.fleet, a.notifier, logf)
	if l, ok := any(a.chats).(interface{ Lead() *kitty.Kitty }); ok {
		replies.Lead = l.Lead
	}
	a.notifier.OnSent(replies.Saw)
	go replies.Listen(ctx, a.fleet.ReplyChannel, nil)
	// What is in the middle of a task is neither updated nor turned off: set before the watchdog
	// starts, which looks at it. A machine that turns itself off after a time without use is
	// noted in the kitty's journal and told to the person.
	a.fleet.SetBusy(a.chats.KittyBusy)
	a.fleet.SetOnPowerOff(func(k *kitty.Kitty, idle time.Duration) {
		text := fmt.Sprintf("its machine turned itself off after %d minutes without use", int(idle.Minutes()))
		_ = a.agenda.Note(agenda.Entry{Kitty: k.ID, Kind: agenda.KindPowerOff, Text: text})
	})
	go a.fleet.Watch(ctx, watchEvery, func(k *kitty.Kitty, what string) {
		logf("watchdog: %s is back (%s)", k.Name, what)
		_ = a.agenda.Note(agenda.Entry{Kitty: k.ID, Kind: agenda.KindRecovered, Text: what})
		a.notifier.Notify(notify.Notification{Event: notify.Recovered, Kitty: k.Name, Title: "is back", Body: what})
	}, logf)
	// A newer desktop image is downloaded and put on the idle desktops, their sessions kept.
	go a.fleet.WatchUpdates(ctx, updateFirstCheck, time.Duration(a.cfg.Containers.UpdateCheckHours)*time.Hour, func(k *kitty.Kitty) {
		logf("update: the desktop of %s is on the newest image", k.Name)
		_ = a.agenda.Note(agenda.Entry{Kitty: k.ID, Kind: agenda.KindUpdated, Text: "its desktop was updated to the newest image; its sessions were kept"})
		a.notifier.Notify(notify.Notification{Event: notify.Updated, Kitty: k.Name, Title: "its desktop was updated", Body: "It runs the newest image now; its cookies, sessions and passwords were kept."})
	}, logf)
	// Kitties itself: told about (notify) or moved to (auto) a newer release, inside the window and
	// when no kitty is working and no desktop is held.
	go a.updater.Confirm(ctx)
	if a.booted.RolledBack != "" {
		logf("update: %s did not hold and Kitties went back to %s; it will not be offered again", a.booted.RolledBack, version)
		a.notifier.Notify(rolledBackNote(a.booted.RolledBack, version))
	}
	window, _ := config.ParseWindow(a.cfg.Updates.Window)
	go a.updater.Watch(ctx, updateFirstCheck, time.Duration(a.cfg.Updates.CheckHours)*time.Hour, updateHooks(a.notifier, func() bool { return window.Contains(time.Now()) }, logf))
	err := srv.Listen(ctx, g.Listen, logf)
	// Stopping: the hand-over notices still waiting to be folded go out now, not lost.
	a.chats.FlushHandOvers()
	a.notifier.Wait()
	return err
}

// tidyFirst and tidyEvery are when the janitor sweeps: soon after the start, then every few hours.
var (
	tidyFirst = 10 * time.Minute
	tidyEvery = 6 * time.Hour
)

// openSecretStores opens the vault of saved secrets and the store of session backups, which share
// a key kept in the data folder. Where the folder cannot be written they are both off (nil) and it
// says so: a read-only volume must not keep the gateway from starting.
func openSecretStores(dataDir string, warn io.Writer) (*vault.Store, *keep.Store) {
	v, err := vault.Open(dataDir)
	if err != nil {
		fmt.Fprintf(warn, "kitties: saved secrets and session backups are off: %v\n", err)
		return nil, nil
	}
	k, err := keep.Open(dataDir)
	if err != nil {
		fmt.Fprintf(warn, "kitties: saved secrets and session backups are off: %v\n", err)
		return nil, nil
	}
	return v, k
}

// rolledBackNote tells that a version that did not hold was gone back from.
func rolledBackNote(failed, now string) notify.Notification {
	return notify.Notification{Event: notify.Updated, Title: "Kitties went back to " + now,
		Body: failed + " did not start properly, so the previous version is running again. It will not be offered again."}
}

// updateHooks is what the updates of Kitties itself tell the person, and when they may run.
func updateHooks(n notify.Sender, window func() bool, logf func(string, ...any)) selfupdate.Hooks {
	return selfupdate.Hooks{
		Window: window,
		Available: func(st selfupdate.Status) {
			body := "Update it from Settings, Preferences."
			if st.Why != "" {
				body = st.Why
			}
			n.Notify(notify.Notification{Event: notify.Updated, Title: "Kitties " + st.Latest + " is available", Body: body})
		},
		Updated: func(from, to string) {
			n.Notify(notify.Notification{Event: notify.Updated, Title: "Kitties was updated to " + to, Body: "It was " + from + "; it restarts now, and the desktops of the kitties keep running."})
		},
		Log: logf,
	}
}

// updateFirstCheck is how long after the start the gateway first looks for a newer desktop image.
var updateFirstCheck = 2 * time.Minute

// watchEvery is how often the watchdog looks at the containers.
var watchEvery = 30 * time.Second

// syncGoals gives the agenda the goals the configuration lists, by the kitties' ids.
func (a *app) syncGoals() error {
	var goals []agenda.Goal
	for _, k := range a.cfg.Kitties {
		if len(k.Goals) == 0 {
			continue
		}
		names := []string{k.Name}
		if k.Replicas > 1 {
			names = names[:0]
			for i := 1; i <= k.Replicas; i++ {
				names = append(names, fmt.Sprintf("%s-%d", k.Name, i))
			}
		}
		for _, name := range names {
			kit, err := a.fleet.Get(name)
			if err != nil {
				continue
			}
			for _, g := range k.Goals {
				goals = append(goals, agenda.Goal{Kitty: kit.ID, Title: g.Title, Text: g.Text, Every: g.Every, On: g.On, Flow: g.Flow})
			}
		}
	}
	return a.agenda.Store.Sync(goals)
}

func (a *app) cmdList() error {
	list := a.fleet.List()
	if len(list) == 0 {
		fmt.Fprintln(a.out, "no kitties yet: `kitties create <name>` or list them in the configuration and run `kitties gateway`")
		return nil
	}
	fmt.Fprintf(a.out, "%-16s %-8s %-12s %-8s %-16s %s\n", "NAME", "STATUS", "PROVIDER", "DESKTOP", "AVATAR", "ID")
	for _, k := range list {
		desktop := "no"
		if k.Desktop {
			desktop = "yes"
		}
		fmt.Fprintf(a.out, "%-16s %-8s %-12s %-8s %-16s %s\n", k.Name, k.Status, k.Provider, desktop, k.Avatar.Breed+"/"+k.Avatar.Color, k.ID)
		if k.Error != "" {
			fmt.Fprintf(a.out, "  error: %s\n", k.Error)
		}
	}
	if lead := a.fleet.Lead(); lead != nil {
		fmt.Fprintf(a.out, "lead: %s\n", lead.Name)
	}
	return nil
}

// cmdLead shows the lead kitty, makes another one the lead, or ("none") leaves the fleet without one.
func (a *app) cmdLead(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: kitties lead [<name> | none]")
	}
	if len(args) == 1 {
		on, ref := true, args[0]
		if ref == "none" || ref == "-" {
			lead := a.fleet.Lead()
			if lead == nil {
				fmt.Fprintln(a.out, "the fleet has no lead")
				return nil
			}
			on, ref = false, lead.ID
		}
		if _, err := a.fleet.Edit(ref, orchestrator.Update{Lead: &on}); err != nil {
			return err
		}
	}
	lead := a.fleet.Lead()
	if lead == nil {
		fmt.Fprintln(a.out, "the fleet has no lead: `kitties lead <name>` makes one the orchestrator of the others")
		return nil
	}
	fmt.Fprintf(a.out, "%s leads the fleet: it hands the work to the others and reports for all of them\n", lead.Name)
	return nil
}

func (a *app) cmdProviders() error {
	fmt.Fprintln(a.out, "configured profiles:")
	for _, p := range a.fleet.Providers().List() {
		fmt.Fprintf(a.out, "  %-12s %s\n", p.Name, p.Describe)
	}
	fmt.Fprintln(a.out, "supported kinds:", strings.Join(provider.Kinds(), ", "))
	return nil
}

func (a *app) cmdCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(a.err)
	prov := fs.String("provider", "", "provider profile (default: default_provider)")
	persona := fs.String("persona", "", "one line on who the kitty is")
	soulFile := fs.String("soul", "", "a soul YAML file")
	connectors := fs.String("connectors", "", "comma-separated connector names (default: every configured one)")
	noDesktop := fs.Bool("no-desktop", false, "no container, brain only")
	avatar := fs.String("avatar", "", "the pixel cat, as breed/color (tabby/orange); empty picks from the name")
	// The name may come first (kitties create ana -persona ...): flags are parsed after it.
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	} else if fs.NArg() != 0 {
		name = ""
	}
	if name == "" {
		return errors.New("usage: kitties create <name> [-provider p] [-persona text] [-soul file] [-connectors a,b] [-no-desktop]")
	}
	spec := orchestrator.Spec{Name: name, Provider: *prov, SoulFile: *soulFile, Soul: soul.Soul{Persona: *persona}}
	if *connectors != "" {
		spec.Connectors = strings.Split(*connectors, ",")
	}
	if *noDesktop {
		no := false
		spec.Desktop = &no
	}
	if *avatar != "" {
		breed, color, _ := strings.Cut(*avatar, "/")
		spec.Avatar = kitty.Avatar{Breed: breed, Color: color}
	}
	k, err := a.fleet.Create(ctx, spec)
	if err != nil {
		return err
	}
	if err := a.fleet.Start(ctx, k.ID); err != nil {
		return fmt.Errorf("%s was created but did not start: %w", k.Name, err)
	}
	fmt.Fprintf(a.out, "%s is up (%s, id %s)\n", k.Name, k.Provider, k.ID)
	return nil
}

func (a *app) cmdLifecycle(ctx context.Context, cmd string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kitties %s <name>", cmd)
	}
	ops := map[string]func(context.Context, string) error{"start": a.fleet.Start, "stop": a.fleet.Stop, "rm": a.fleet.Remove}
	if err := ops[cmd](ctx, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s: %s\n", args[0], cmd)
	return nil
}

func (a *app) cmdScale(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: kitties scale <name> <replicas>")
	}
	n, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("%q is not a number", args[1])
	}
	if err := a.fleet.Scale(ctx, args[0], n); err != nil {
		return err
	}
	return a.cmdList()
}

func (a *app) cmdExec(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: kitties exec <name> <command...>")
	}
	out, err := a.fleet.Exec(ctx, args[0], args[1:]...)
	fmt.Fprint(a.out, out)
	return err
}

func (a *app) cmdShot(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: kitties shot <name> <file.png>")
	}
	png, err := a.fleet.Screenshot(ctx, args[0])
	if err != nil {
		return err
	}
	if err := os.WriteFile(args[1], png, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "saved %s (%d bytes)\n", args[1], len(png))
	return nil
}

// cmdChat is the terminal chat: one kitty, or a group of them.
func (a *app) cmdChat(ctx context.Context, args []string) error {
	if len(args) == 0 {
		// No kitty named: the lead, which hands the work to the others.
		if lead := a.fleet.Lead(); lead != nil {
			args = []string{lead.Name}
		}
	}
	if len(args) != 1 {
		return errors.New("usage: kitties chat <name>[,<name>...] (or no name, for the lead kitty)")
	}
	members := strings.Split(args[0], ",")
	sess, err := a.chats.Create("", members)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "chat %s with %s. Type a message; /attach <file>... adds files to the next one; /quit ends it.\n", sess.ID, sess.Title)
	if sess.Group() {
		fmt.Fprintln(a.out, "(@name sends the message to one kitty; otherwise every kitty answers in turn)")
	}
	scanner := bufio.NewScanner(a.in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var files []attach.Upload
	for {
		fmt.Fprint(a.out, "> ")
		if !scanner.Scan() {
			fmt.Fprintln(a.out)
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case line == "/quit" || line == "/exit":
			return nil
		case line == "/attach" || strings.HasPrefix(line, "/attach "):
			// The files wait for the next message; the person's own paths, read as the person.
			for _, path := range strings.Fields(strings.TrimPrefix(line, "/attach")) {
				up, err := readUpload(path)
				if err != nil {
					fmt.Fprintf(a.err, "kitties: %v\n", err)
					continue
				}
				files = append(files, up)
				fmt.Fprintf(a.out, "attached %s (%d KB); it goes with your next message\n", up.Name, (len(up.Data)+1023)/1024)
			}
			continue
		}
		// The files go once: a message that was refused with them is sent again without a bad one.
		turns, err := a.chats.SendFiles(ctx, sess.ID, line, files)
		files = nil
		if err != nil {
			fmt.Fprintf(a.err, "kitties: %v\n", err)
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		working := false
		for _, t := range turns {
			fmt.Fprintf(a.out, "\n%s: %s\n", t.From, t.Text)
			for _, c := range t.Calls {
				if c.Err != "" {
					fmt.Fprintf(a.out, "  [%s failed: %s]\n", c.Name, c.Err)
				} else {
					fmt.Fprintf(a.out, "  [%s: done]\n", c.Name)
				}
			}
			working = working || t.Working
		}
		if working {
			a.followTask(ctx, sess.ID)
		}
		fmt.Fprintln(a.out)
	}
}

// followTask prints the steps of a desktop task as they happen, until the kitty is done.
func (a *app) followTask(ctx context.Context, id string) {
	fmt.Fprintln(a.out, "  (working on the desktop; ctrl+c cancels)")
	shown := map[int]int{}
	finished := map[int]bool{}
	for {
		select {
		case <-ctx.Done():
			a.chats.Cancel(id)
			a.chats.Wait()
			return
		case <-time.After(pollEvery):
		}
		sess, err := a.chats.Get(id)
		if err != nil {
			return
		}
		working := false
		for i, t := range sess.Turns {
			if t.From == "user" || finished[i] {
				continue
			}
			// The screenshot of a step lands just after the step does: while the task goes on, the
			// newest step waits until it has its shot or another step follows it.
			upto := len(t.Steps)
			if t.Working && upto > shown[i] && t.Steps[upto-1].Shot == "" {
				upto--
			}
			for _, st := range t.Steps[shown[i]:upto] {
				line := "  · " + st.Action
				if st.Action == chat.StepPlan {
					line = "  » " + st.Result
				} else if st.Error != "" {
					line += " failed: " + st.Error
				} else if st.Result != "" && st.Action != "done" {
					line += ": " + firstLine(st.Result)
				}
				if st.Shot != "" {
					line += "  [shot " + st.Shot + "]"
				}
				fmt.Fprintln(a.out, line)
			}
			shown[i] = upto
			if t.Working {
				working = true
				continue
			}
			// Finished: print the final words of a task once, then forget the turn.
			if shown[i] > 0 {
				if idx := strings.LastIndex(t.Text, "\n\n"); idx >= 0 {
					fmt.Fprintf(a.out, "\n%s: %s\n", t.From, t.Text[idx+2:])
				}
			}
			finished[i] = true
		}
		if !working {
			return
		}
	}
}

var pollEvery = 500 * time.Millisecond

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "…"
	}
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return s
}

// readUpload reads a file named on the command line, refusing one over the size limit before it
// is read into memory.
func readUpload(path string) (attach.Upload, error) {
	info, err := os.Stat(path)
	if err != nil {
		return attach.Upload{}, err
	}
	if !info.Mode().IsRegular() {
		return attach.Upload{}, fmt.Errorf("%s is not a file", path)
	}
	if info.Size() > attach.MaxOther {
		return attach.Upload{}, fmt.Errorf("%s is too big to attach", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return attach.Upload{}, err
	}
	return attach.Upload{Name: filepath.Base(path), Data: data}, nil
}
