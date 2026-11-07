package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/madkoding/kitties/internal/config"
)

// kitties expose, kitties config and kitties token rotate set the gateway up from the terminal,
// without the web page: where it can be reached from, its port, public address and proxy, and its
// token. They never load the AI profiles or the kitties, so nothing about a missing key is said.
//
// What they change is "the gateway": the service of scripts/install.sh when it is installed (its
// environment file and data folder, through sudo when needed), or else the gateway of this user
// (the configuration file and ~/.kitties). -config, KITTIES_CONFIG or KITTIES_DATA_DIR always mean
// the gateway of this user.

// Where the service of scripts/install.sh keeps its settings and its data; variables for the tests.
var (
	serviceEnvFile = "/etc/kitties/kitties.env"
	serviceDataDir = "/var/lib/kitties"
	selfExe        = os.Executable
	readable       = func(path string) bool { _, err := os.ReadFile(path); return err == nil }
)

// setup is the gateway the setup commands change.
type setup struct {
	service    bool
	envFile    string // the service's environment file
	configPath string // the configuration file of this user's gateway
	hasFile    bool   // the configuration file exists
	dataDir    string
	// raw is the gateway block as written (file and environment); cfg has the saved exposure applied.
	raw, cfg config.Config
	in       io.Reader
	out, err io.Writer
}

// serviceInstalled reports whether the setup commands should change the installed service: it is
// there (its environment file exists, even where this user cannot read it) and nothing names a
// configuration of this user's own.
func serviceInstalled(configFlag string) bool {
	if configFlag != "" || os.Getenv("KITTIES_CONFIG") != "" || os.Getenv("KITTIES_DATA_DIR") != "" {
		return false
	}
	return serviceExists()
}

// serviceExists reports whether the service of scripts/install.sh is installed here.
func serviceExists() bool {
	_, err := os.Stat(serviceEnvFile)
	return err == nil || errors.Is(err, os.ErrPermission)
}

// asRoot runs this same command again through sudo, for the files of the service that only root
// (and the kitties user) can read. It returns the exit code.
func asRoot(args []string, in io.Reader, out, errOut io.Writer) int {
	if _, err := lookPath("sudo"); err != nil {
		fmt.Fprintf(errOut, "kitties: the service's settings (%s) need root: run this as root\n", serviceEnvFile)
		return 1
	}
	exe, err := selfExe()
	if err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	if err := serviceRunner("sudo", append([]string{exe}, args...), in, out, errOut); err != nil {
		var exit *osexec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			return exit.ExitCode()
		}
		fmt.Fprintf(errOut, "kitties: sudo: %v\n", err)
		return 1
	}
	return 0
}

// openSetup reads the gateway's settings: the service's or this user's.
func openSetup(service bool, configFlag string, in io.Reader, out, errOut io.Writer) (*setup, error) {
	s := &setup{service: service, in: in, out: out, err: errOut}
	if service {
		s.envFile = serviceEnvFile
		data, err := os.ReadFile(serviceEnvFile)
		if err != nil {
			return nil, fmt.Errorf("read the service's settings: %w", err)
		}
		env := parseEnv(string(data))
		s.raw = config.Defaults()
		if err := s.raw.ApplyEnv(func(k string) string { return env[k] }); err != nil {
			return nil, fmt.Errorf("%s: %w", serviceEnvFile, err)
		}
		// The unit sets the data folder; the environment file does not.
		if env["KITTIES_DATA_DIR"] == "" {
			s.raw.DataDir = serviceDataDir
		}
	} else {
		s.configPath = configFlag
		if s.configPath == "" {
			s.configPath = config.DefaultPath()
		}
		cfg, err := config.Load(s.configPath)
		switch {
		case err == nil:
			s.hasFile = true
		case errors.Is(err, os.ErrNotExist):
			cfg = config.Defaults()
		default:
			return nil, err
		}
		if err := cfg.ApplyEnv(os.Getenv); err != nil {
			return nil, err
		}
		s.raw = cfg
	}
	s.dataDir = s.raw.DataDirOr()
	s.cfg = s.raw
	s.cfg.ApplyExposure()
	return s, nil
}

// where says which gateway is being changed.
func (s *setup) where() string {
	if s.service {
		return fmt.Sprintf("the service (settings %s, data %s)", s.envFile, s.dataDir)
	}
	file := s.configPath
	if !s.hasFile {
		file += ", not written yet"
	}
	return fmt.Sprintf("this user's (configuration %s, data %s)", file, s.dataDir)
}

// openURL is where the page of the gateway opens from another computer (or this one).
func (s *setup) openURL() string {
	g := s.cfg.Gateway
	if g.PublicURL != "" {
		return g.PublicURL
	}
	scheme := "http"
	if g.TLSCert != "" {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(g.Listen)
	if err != nil {
		return scheme + "://" + g.Listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		if host = lanIP(); host == "" {
			host = "127.0.0.1"
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// restart makes a change take effect: the service is restarted; a gateway of this user's own is
// restarted by whoever runs it.
func (s *setup) restart(noRestart bool) int {
	if !s.service {
		fmt.Fprintln(s.out, "Restart the gateway to apply it (stop it, then run `kitties gateway`).")
		return 0
	}
	if noRestart {
		fmt.Fprintln(s.out, "Restart the service to apply it: kitties gateway restart")
		return 0
	}
	if _, err := lookPath("systemctl"); err != nil {
		fmt.Fprintln(s.out, "systemctl not found: restart the gateway to apply it.")
		return 0
	}
	if err := serviceRunner("systemctl", []string{"restart", serviceName()}, s.in, s.out, s.err); err != nil {
		fmt.Fprintf(s.err, "kitties: the change is saved but the service did not restart: %v (kitties gateway logs)\n", err)
		return 1
	}
	fmt.Fprintf(s.out, "Restarted the service; the page is at %s\n", s.openURL())
	return 0
}

// cmdSetup runs expose, config and token for the gateway they change. args are the whole command
// line, run again through sudo when the service's files need root.
func cmdSetup(cmd string, rest []string, configFlag string, args []string, in io.Reader, out, errOut io.Writer) int {
	service := serviceInstalled(configFlag)
	if cmd == "token" && len(rest) == 0 {
		if !service {
			return -1 // the token of this user's gateway: the usual path
		}
		if t := serviceToken(); t != "" {
			fmt.Fprintln(out, t)
			return 0
		}
		if isRoot() {
			fmt.Fprintf(errOut, "kitties: the service has no token yet (%s/gateway.token): start it once (kitties gateway start)\n", serviceDataDir)
			return 1
		}
		return asRoot(args, in, out, errOut)
	}
	if service && !isRoot() {
		if !readable(serviceEnvFile) {
			return asRoot(args, in, out, errOut)
		}
	}
	s, err := openSetup(service, configFlag, in, out, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "kitties: %v\n", err)
		return 1
	}
	switch cmd {
	case "expose":
		return s.expose(rest)
	case "config":
		return s.config(rest)
	default:
		return s.token(rest)
	}
}

// positional takes the leading words before the flags (kitties expose wan -yes) and parses the
// flags, after which more words may come.
func positional(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		words, args = append(words, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return append(words, fs.Args()...), nil
}

func (s *setup) expose(args []string) int {
	fs := flag.NewFlagSet("expose", flag.ContinueOnError)
	fs.SetOutput(s.err)
	yes := fs.Bool("yes", false, "do not ask before opening it to every network (wan)")
	noRestart := fs.Bool("no-restart", false, "save the choice without restarting the service")
	words, err := positional(fs, args)
	if err != nil {
		return 2
	}
	mode := s.cfg.Gateway.Listen
	current := config.ExposureOf(mode)
	if len(words) == 0 {
		fmt.Fprintf(s.out, "Gateway: %s\n", s.where())
		note := ""
		if current == config.ExposureLAN && config.ExposureAuto(s.dataDir) {
			note = " (chosen by the first start, to set it up from another computer)"
		}
		fmt.Fprintf(s.out, "Exposure: %s, listening on %s%s\n", current, s.cfg.Gateway.Listen, note)
		fmt.Fprintf(s.out, "Open: %s\n", s.openURL())
		fmt.Fprintln(s.out, "Change it: kitties expose local (this machine) | lan (the local network) | wan (every network)")
		return 0
	}
	if len(words) > 1 {
		fmt.Fprintln(s.err, "kitties: usage: kitties expose [local|lan|wan] [-yes] [-no-restart]")
		return 2
	}
	want := strings.ToLower(words[0])
	if !config.ValidExposure(want) {
		fmt.Fprintf(s.err, "kitties: %q is not an exposure: local, lan or wan\n", words[0])
		return 2
	}
	if inContainer() {
		fmt.Fprintln(s.err, "kitties: this gateway runs in a container: set KITTIES_LISTEN and the published port in the compose file")
		return 1
	}
	_, port, err := net.SplitHostPort(s.raw.Gateway.Listen)
	if err != nil {
		fmt.Fprintf(s.err, "kitties: the listen address %q has no port\n", s.raw.Gateway.Listen)
		return 1
	}
	listen, err := config.ListenFor(want, port, lanIP())
	if err != nil {
		fmt.Fprintf(s.err, "kitties: %v\n", err)
		return 1
	}
	if want == current && config.SavedExposure(s.dataDir) == want && !config.ExposureAuto(s.dataDir) {
		fmt.Fprintf(s.out, "The gateway is already %s (%s); nothing changed.\n", want, listen)
		return 0
	}
	if want == config.ExposureWAN && !*yes {
		fmt.Fprintln(s.out, "WAN listens on every network (0.0.0.0): anyone who reaches this machine sees the sign-in page,\n"+
			"and on a public machine that is the whole internet. Only the token stands in the way; https (public_url\n"+
			"behind a proxy) keeps it from travelling in the clear.")
		fmt.Fprint(s.out, "Type yes to open it: ")
		line, _ := readLine(s.in)
		if strings.ToLower(strings.TrimSpace(line)) != "yes" {
			fmt.Fprintln(s.out, "Nothing changed.")
			return 1
		}
	}
	if err := config.SaveExposure(s.dataDir, want); err != nil {
		fmt.Fprintf(s.err, "kitties: %v\n", err)
		return 1
	}
	ownLike(s.dataDir, filepath.Join(s.dataDir, config.ExposureFile))
	fmt.Fprintf(s.out, "Exposure: %s, the gateway will listen on %s\n", want, listen)
	if want != config.ExposureLocal {
		fmt.Fprintln(s.out, "Signing in takes the token: kitties token")
	}
	return s.restart(*noRestart)
}

// readLine reads one line of the answer, without buffering beyond it.
func readLine(in io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}

// setting is one key of `kitties config set`: its name in the file, its variable for the service.
type setting struct {
	yaml, env, help string
}

var settings = map[string]setting{
	"port":        {"listen", "KITTIES_LISTEN", "the port the gateway listens on (7488); where it listens is `kitties expose`"},
	"public_url":  {"public_url", "KITTIES_PUBLIC_URL", "the address people open from outside, behind https (https://kitties.example.com)"},
	"trust_proxy": {"trust_proxy", "KITTIES_TRUST_PROXY", "true when a reverse proxy (Caddy, nginx) sits in front"},
}

func (s *setup) config(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(s.err)
	noRestart := fs.Bool("no-restart", false, "save the change without restarting the service")
	words, err := positional(fs, args)
	if err != nil {
		return 2
	}
	if len(words) == 0 {
		s.show()
		return 0
	}
	verb, words := words[0], words[1:]
	if (verb == "set" && len(words) != 2) || (verb == "unset" && len(words) != 1) || (verb != "set" && verb != "unset") {
		fmt.Fprintln(s.err, "kitties: usage: kitties config [set <key> <value> | unset <key>] [-no-restart]; keys: port, public_url, trust_proxy")
		return 2
	}
	key := words[0]
	st, ok := settings[key]
	if !ok {
		fmt.Fprintf(s.err, "kitties: %q is not a setting: port, public_url or trust_proxy (where it listens is `kitties expose`, the token `kitties token rotate`)\n", key)
		return 2
	}
	value := ""
	if verb == "set" {
		if value, err = s.value(key, words[1]); err != nil {
			fmt.Fprintf(s.err, "kitties: %s: %v\n", key, err)
			return 2
		}
	} else if key == "port" {
		value = config.DefaultListen
	}
	if err := s.write(st, value); err != nil {
		fmt.Fprintf(s.err, "kitties: %v\n", err)
		return 1
	}
	if verb == "set" {
		fmt.Fprintf(s.out, "Set %s to %s.\n", key, words[1])
	} else {
		fmt.Fprintf(s.out, "Unset %s.\n", key)
	}
	if !s.service && os.Getenv(st.env) != "" {
		fmt.Fprintf(s.out, "Note: %s is set in the environment and wins over the file.\n", st.env)
	}
	return s.restart(*noRestart)
}

// value checks a value and turns it into what is written.
func (s *setup) value(key, v string) (string, error) {
	switch key {
	case "port":
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("a port is a number from 1 to 65535")
		}
		host, _, err := net.SplitHostPort(s.raw.Gateway.Listen)
		if err != nil {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, v), nil
	case "public_url":
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("a public URL is like https://kitties.example.com")
		}
		return strings.TrimRight(v, "/"), nil
	default:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", errors.New("true or false")
		}
		return strconv.FormatBool(b), nil
	}
}

// write keeps one setting ("" removes it) where the gateway reads it: the service's environment
// file, or the gateway block of this user's configuration file (made when there is none). A file
// that no longer loads is put back as it was.
func (s *setup) write(st setting, value string) error {
	path := s.configPath
	if s.service {
		path = s.envFile
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var next string
	if s.service {
		next = setEnvLine(string(old), st.env, value)
	} else if next, err = setGatewayKey(string(old), st.yaml, value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		return err
	}
	if s.service {
		return nil
	}
	if _, err := config.Load(path); err != nil {
		if old == nil {
			_ = os.Remove(path)
		} else {
			_ = os.WriteFile(path, old, 0o600)
		}
		return fmt.Errorf("the change was undone: %w", err)
	}
	return nil
}

// show prints the gateway's settings, the token aside.
func (s *setup) show() {
	g := s.cfg.Gateway
	or := func(v, none string) string {
		if v == "" {
			return none
		}
		return v
	}
	fmt.Fprintf(s.out, "Gateway: %s\n", s.where())
	fmt.Fprintf(s.out, "  exposure     %s (kitties expose local|lan|wan)\n", config.ExposureOf(g.Listen))
	fmt.Fprintf(s.out, "  listen       %s\n", g.Listen)
	fmt.Fprintf(s.out, "  public_url   %s\n", or(g.PublicURL, "none"))
	fmt.Fprintf(s.out, "  trust_proxy  %t\n", g.TrustProxy)
	tls := "off"
	if g.TLSCert != "" {
		tls = g.TLSCert
	}
	fmt.Fprintf(s.out, "  tls          %s\n", tls)
	token := "kept in " + s.dataDir + "/gateway.token (kitties token shows it, kitties token rotate replaces it)"
	if g.Token != "" {
		token = "fixed in the configuration or KITTIES_TOKEN"
	}
	fmt.Fprintf(s.out, "  token        %s\n", token)
	mcp := "off"
	if g.MCP.Enabled {
		mcp = "on"
	}
	fmt.Fprintf(s.out, "  mcp          %s\n", mcp)
	fmt.Fprintf(s.out, "Open: %s\n", s.openURL())
	fmt.Fprintln(s.out, "Change: kitties config set port|public_url|trust_proxy <value>, kitties config unset <key>")
}

func (s *setup) token(args []string) int {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	fs.SetOutput(s.err)
	noRestart := fs.Bool("no-restart", false, "write the new token without restarting the service")
	words, err := positional(fs, args)
	if err != nil {
		return 2
	}
	if len(words) != 1 || words[0] != "rotate" {
		fmt.Fprintln(s.err, "kitties: usage: kitties token [rotate [-no-restart]]")
		return 2
	}
	e := exposureOf(s.raw, s.raw.Gateway.Listen)
	if err := e.RotateGatewayToken(); err != nil {
		fmt.Fprintf(s.err, "kitties: %v\n", err)
		return 1
	}
	ownLike(s.dataDir, filepath.Join(s.dataDir, "gateway.token"))
	token, _, _ := gatewayToken(s.raw)
	fmt.Fprintf(s.out, "New token: %s\nEvery browser and program that used the old one has to sign in again.\n", token)
	return s.restart(*noRestart)
}

// parseEnv reads an environment file of KEY=value lines.
func parseEnv(text string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return env
}

// setEnvLine sets name=value in an environment file, in place of the lines it had ("" removes them).
func setEnvLine(text, name, value string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	var out []string
	done := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), name+"=") {
			if !done && value != "" {
				out = append(out, name+"="+value)
			}
			done = true
			continue
		}
		out = append(out, l)
	}
	if !done && value != "" {
		out = append(out, name+"="+value)
	}
	return strings.Join(out, "\n") + "\n"
}

var (
	gatewayBlock = regexp.MustCompile(`^gateway:\s*(#.*)?$`)
	indentRe     = regexp.MustCompile(`^([ \t]+)\S`)
)

// setGatewayKey sets key: value in the gateway block of a YAML configuration ("" removes it),
// leaving the rest of the file, comments included, as it was.
func setGatewayKey(text, key, value string) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if strings.TrimSpace(text) == "" {
		lines = nil
	}
	entry := func(indent string) string {
		v := value
		if key != "trust_proxy" {
			v = strconv.Quote(value)
		}
		return indent + key + ": " + v
	}
	start := -1
	for i, l := range lines {
		if gatewayBlock.MatchString(l) {
			start = i
			break
		}
		if strings.HasPrefix(l, "gateway:") {
			return "", errors.New("the gateway block is written on one line: change it by hand")
		}
	}
	if start < 0 {
		if value == "" {
			return text, nil
		}
		lines = append(lines, "", "gateway:", entry("  "))
		return strings.TrimLeft(strings.Join(lines, "\n"), "\n") + "\n", nil
	}
	indent := ""
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := indentRe.FindStringSubmatch(l)
		if m == nil {
			break // the next top-level key
		}
		if indent == "" {
			indent = m[1]
		}
		if m[1] == indent && strings.HasPrefix(strings.TrimLeft(l, " \t"), key+":") {
			if value == "" {
				return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n") + "\n", nil
			}
			lines[i] = entry(indent)
			return strings.Join(lines, "\n") + "\n", nil
		}
	}
	if value == "" {
		return strings.Join(lines, "\n") + "\n", nil
	}
	if indent == "" {
		indent = "  "
	}
	lines = append(lines[:start+1], append([]string{entry(indent)}, lines[start+1:]...)...)
	return strings.Join(lines, "\n") + "\n", nil
}
