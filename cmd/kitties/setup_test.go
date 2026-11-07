package main

import (
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
)

// localSetup points the setup commands at a gateway of this user's own, in a temporary folder,
// with no service installed.
func localSetup(t *testing.T) (cfg, data string) {
	t.Helper()
	t.Setenv("KITTIES_IN_DOCKER", "")
	if inContainer() {
		t.Skip("running in a container: the compose file decides where it listens")
	}
	dir := t.TempDir()
	for _, k := range []string{"KITTIES_CONFIG", "KITTIES_LISTEN", "KITTIES_PUBLIC_URL", "KITTIES_TOKEN", "KITTIES_TRUST_PROXY", "KITTIES_DATA_DIR"} {
		t.Setenv(k, "")
	}
	oldEnv, oldIP := serviceEnvFile, lanIP
	t.Cleanup(func() { serviceEnvFile, lanIP = oldEnv, oldIP })
	serviceEnvFile = filepath.Join(dir, "no-service", "kitties.env")
	lanIP = func() string { return "127.0.0.2" }
	data = filepath.Join(dir, "data")
	cfg = filepath.Join(dir, "kitties.yaml")
	return cfg, data
}

func TestExposeFromTheTerminal(t *testing.T) {
	cfg, data := localSetup(t)
	_ = os.WriteFile(cfg, []byte("data_dir: "+data+"\nproviders:\n  fast:\n    kind: openai\ngateway:\n  listen: 127.0.0.1:7600\n"), 0o600)
	t.Setenv("OPENAI_API_KEY", "")
	code, out, errOut := exec(t, "", "-config", cfg, "expose")
	if code != 0 || !strings.Contains(out, "Exposure: local, listening on 127.0.0.1:7600") || !strings.Contains(out, "http://127.0.0.1:7600") {
		t.Errorf("expose shows: %d %s %s", code, out, errOut)
	}
	if strings.Contains(errOut, "API key") || strings.Contains(errOut, "provider") {
		t.Errorf("expose says nothing of the profiles: %s", errOut)
	}
	if code, out, _ := exec(t, "", "-config", cfg, "expose", "lan"); code != 0 || !strings.Contains(out, "127.0.0.2:7600") || !strings.Contains(out, "Restart the gateway") {
		t.Errorf("expose lan: %d %s", code, out)
	}
	if config.SavedExposure(data) != "lan" {
		t.Error("the choice is kept")
	}
	// WAN asks first: anything but yes changes nothing.
	if code, out, _ := exec(t, "no\n", "-config", cfg, "expose", "wan"); code != 1 || !strings.Contains(out, "Nothing changed") || config.SavedExposure(data) != "lan" {
		t.Errorf("wan without a yes: %d %s", code, out)
	}
	if code, out, _ := exec(t, "yes\n", "-config", cfg, "expose", "WAN"); code != 0 || !strings.Contains(out, "0.0.0.0:7600") || config.SavedExposure(data) != "wan" {
		t.Errorf("wan with a yes: %d %s", code, out)
	}
	if code, out, _ := exec(t, "", "-config", cfg, "expose", "wan"); code != 0 || !strings.Contains(out, "already wan") {
		t.Errorf("the same choice again: %d %s", code, out)
	}
	if code, out, _ := exec(t, "", "-config", cfg, "expose"); code != 0 || !strings.Contains(out, "Exposure: wan") || !strings.Contains(out, "http://127.0.0.2:7600") {
		t.Errorf("wan opens at the LAN address: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "expose", "local", "-yes"); code != 0 || config.SavedExposure(data) != "local" {
		t.Error("back to local, a flag after the mode")
	}
	// The first start's own choice is said, and choosing the same takes the mark away.
	_ = config.SaveExposure(data, "lan")
	_ = os.WriteFile(filepath.Join(data, config.ExposureAutoFile), []byte("first start\n"), 0o600)
	if _, out, _ := exec(t, "", "-config", cfg, "expose"); !strings.Contains(out, "chosen by the first start") && !strings.Contains(out, "Exposure: lan") {
		t.Errorf("auto lan: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "expose", "lan"); code != 0 || config.ExposureAuto(data) {
		t.Error("choosing lan on purpose drops the mark")
	}
	for _, bad := range [][]string{{"expose", "moon"}, {"expose", "lan", "wan"}, {"expose", "-bogus"}} {
		if code, _, _ := exec(t, "", append([]string{"-config", cfg}, bad...)...); code != 2 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	lanIP = func() string { return "" }
	if code, _, errOut := exec(t, "", "-config", cfg, "expose", "lan"); code != 1 || !strings.Contains(errOut, "local network") {
		t.Errorf("no LAN address: %d %s", code, errOut)
	}
	t.Setenv("KITTIES_IN_DOCKER", "1")
	if code, _, errOut := exec(t, "", "-config", cfg, "expose", "wan", "-yes"); code != 1 || !strings.Contains(errOut, "container") {
		t.Errorf("a container: %d %s", code, errOut)
	}
}

func TestConfigFromTheTerminal(t *testing.T) {
	cfg, data := localSetup(t)
	t.Setenv("KITTIES_DATA_DIR", data)
	// No file yet: it is shown, and the first change writes it.
	code, out, errOut := exec(t, "", "-config", cfg, "config")
	if code != 0 || !strings.Contains(out, "not written yet") || !strings.Contains(out, "public_url   none") || !strings.Contains(out, "kitties token rotate") {
		t.Errorf("config shows: %d %s %s", code, out, errOut)
	}
	if code, out, _ := exec(t, "", "-config", cfg, "config", "set", "public_url", "https://kitties.example.com/"); code != 0 || !strings.Contains(out, "Set public_url") {
		t.Errorf("set public_url: %d %s", code, out)
	}
	if got := mustRead(t, cfg); got != "gateway:\n  public_url: \"https://kitties.example.com\"\n" {
		t.Errorf("the file written: %q", got)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "config", "set", "port", "8080"); code != 0 {
		t.Error("set port")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "config", "set", "trust_proxy", "yes"); code != 2 {
		t.Error("trust_proxy takes true or false")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "config", "set", "trust_proxy", "1"); code != 0 {
		t.Error("set trust_proxy")
	}
	c, err := config.Load(cfg)
	if err != nil || c.Gateway.Listen != "127.0.0.1:8080" || c.Gateway.PublicURL != "https://kitties.example.com" || !c.Gateway.TrustProxy {
		t.Errorf("loaded: %+v %v", c.Gateway, err)
	}
	if _, out, _ := exec(t, "", "-config", cfg, "config"); !strings.Contains(out, "listen       127.0.0.1:8080") || !strings.Contains(out, "Open: https://kitties.example.com") {
		t.Errorf("config after: %s", out)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "config", "unset", "public_url"); code != 0 {
		t.Error("unset public_url")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "config", "unset", "port"); code != 0 {
		t.Error("unset port")
	}
	if c, _ := config.Load(cfg); c.Gateway.PublicURL != "" || c.Gateway.Listen != config.DefaultListen {
		t.Errorf("after unset: %+v", c.Gateway)
	}
	t.Setenv("KITTIES_PUBLIC_URL", "https://env.example.com")
	if _, out, _ := exec(t, "", "-config", cfg, "config", "set", "public_url", "http://a.example.com"); !strings.Contains(out, "wins over the file") {
		t.Errorf("the environment wins: %s", out)
	}
	t.Setenv("KITTIES_PUBLIC_URL", "")
	for _, bad := range [][]string{
		{"config", "set", "port", "0"}, {"config", "set", "port", "x"}, {"config", "set", "public_url", "ftp://x"},
		{"config", "set", "token", "x"}, {"config", "set", "port"}, {"config", "drop", "port"}, {"config", "-bogus"},
	} {
		if code, _, _ := exec(t, "", append([]string{"-config", cfg}, bad...)...); code != 2 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	// A file that does not load is not touched.
	_ = os.WriteFile(cfg, []byte("default_provider: nope\n"), 0o600)
	if code, _, _ := exec(t, "", "-config", cfg, "config", "set", "port", "9"); code != 1 || mustRead(t, cfg) != "default_provider: nope\n" {
		t.Error("a file that does not load")
	}
	_ = os.WriteFile(cfg, []byte("gateway: {listen: \"a:1\"}\n"), 0o600)
	if code, _, errOut := exec(t, "", "-config", cfg, "config", "set", "port", "9"); code != 1 || !strings.Contains(errOut, "by hand") {
		t.Errorf("a gateway block on one line: %d %s", code, errOut)
	}
}

func TestTokenRotateFromTheTerminal(t *testing.T) {
	cfg, data := localSetup(t)
	_ = os.WriteFile(cfg, []byte("data_dir: "+data+"\n"), 0o600)
	code, out, _ := exec(t, "", "-config", cfg, "token", "rotate")
	if code != 0 || !strings.Contains(out, "New token: ") {
		t.Fatalf("rotate: %d %s", code, out)
	}
	tok := strings.TrimSpace(mustRead(t, filepath.Join(data, "gateway.token")))
	if !strings.Contains(out, tok) {
		t.Error("the new token is the one printed")
	}
	if code, got, _ := exec(t, "", "-config", cfg, "token"); code != 0 || strings.TrimSpace(got) != tok {
		t.Errorf("token prints it: %q", got)
	}
	if code, _, _ := exec(t, "", "-config", cfg, "token", "spin"); code != 2 {
		t.Error("token spin")
	}
	if code, _, _ := exec(t, "", "-config", cfg, "token", "rotate", "-bogus"); code != 2 {
		t.Error("token rotate -bogus")
	}
	t.Setenv("KITTIES_TOKEN", "fixed-token-0123456789")
	if code, _, errOut := exec(t, "", "-config", cfg, "token", "rotate"); code != 1 || !strings.Contains(errOut, "KITTIES_TOKEN") {
		t.Errorf("a fixed token: %d %s", code, errOut)
	}
}

// serviceSetup installs a fake service of scripts/install.sh: its files in a temporary folder, its
// commands recorded.
func serviceSetup(t *testing.T, env string) (data string, calls *[]string, root *bool) {
	t.Helper()
	localSetup(t)
	dir := t.TempDir()
	oldEnv, oldData, oldTokens, oldLook, oldRoot, oldRun, oldExe := serviceEnvFile, serviceDataDir, serviceTokenFiles, lookPath, isRoot, serviceRunner, selfExe
	t.Cleanup(func() {
		serviceEnvFile, serviceDataDir, serviceTokenFiles, lookPath, isRoot, serviceRunner, selfExe = oldEnv, oldData, oldTokens, oldLook, oldRoot, oldRun, oldExe
	})
	serviceEnvFile = filepath.Join(dir, "kitties.env")
	serviceDataDir = filepath.Join(dir, "data")
	serviceTokenFiles = []string{filepath.Join(serviceDataDir, "gateway.token"), serviceEnvFile}
	_ = os.WriteFile(serviceEnvFile, []byte(env), 0o600)
	isRoot = func() bool { return true }
	r := true
	root = &r
	isRoot = func() bool { return *root }
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	selfExe = func() (string, error) { return "/usr/local/bin/kitties", nil }
	var c []string
	calls = &c
	serviceRunner = func(name string, args []string, _ io.Reader, _, _ io.Writer) error {
		c = append(c, name+" "+strings.Join(args, " "))
		return nil
	}
	return serviceDataDir, calls, root
}

func TestSetupChangesTheInstalledService(t *testing.T) {
	data, calls, root := serviceSetup(t, "# Read by the systemd unit.\nKITTIES_LISTEN=127.0.0.1:7488\nOPENAI_API_KEY=\n")
	code, out, errOut := exec(t, "", "expose")
	if code != 0 || !strings.Contains(out, "the service (settings "+serviceEnvFile) || !strings.Contains(out, "Exposure: local") {
		t.Errorf("expose on the service: %d %s %s", code, out, errOut)
	}
	if code, out, _ := exec(t, "", "expose", "lan"); code != 0 || !strings.Contains(out, "Restarted the service") {
		t.Errorf("expose lan: %d %s", code, out)
	}
	if last := (*calls)[len(*calls)-1]; last != "systemctl restart kitties" || config.SavedExposure(data) != "lan" {
		t.Errorf("the service restarts: %q", last)
	}
	n := len(*calls)
	if code, out, _ := exec(t, "", "config", "set", "public_url", "https://k.example.com", "-no-restart"); code != 0 || !strings.Contains(out, "kitties gateway restart") || len(*calls) != n {
		t.Errorf("-no-restart: %d %s", code, out)
	}
	if code, _, _ := exec(t, "", "config", "set", "port", "9000"); code != 0 {
		t.Error("set port")
	}
	if code, _, _ := exec(t, "", "config", "set", "trust_proxy", "true"); code != 0 {
		t.Error("set trust_proxy")
	}
	env := mustRead(t, serviceEnvFile)
	if !strings.Contains(env, "# Read by the systemd unit.\nKITTIES_LISTEN=127.0.0.1:9000\nOPENAI_API_KEY=\nKITTIES_PUBLIC_URL=https://k.example.com\nKITTIES_TRUST_PROXY=true\n") {
		t.Errorf("the environment file: %q", env)
	}
	if code, out, _ := exec(t, "", "config"); code != 0 || !strings.Contains(out, "trust_proxy  true") || !strings.Contains(out, "data "+data) {
		t.Errorf("config on the service: %d %s", code, out)
	}
	if code, _, _ := exec(t, "", "config", "unset", "trust_proxy"); code != 0 || strings.Contains(mustRead(t, serviceEnvFile), "TRUST_PROXY") {
		t.Error("unset on the service")
	}
	// The token: printed, rotated.
	if code, _, errOut := exec(t, "", "token"); code != 1 || !strings.Contains(errOut, "no token yet") {
		t.Errorf("no token yet: %d %s", code, errOut)
	}
	if code, out, _ := exec(t, "", "token", "rotate"); code != 0 || !strings.Contains(out, "New token") {
		t.Errorf("rotate on the service: %d %s", code, out)
	}
	tok := strings.TrimSpace(mustRead(t, filepath.Join(data, "gateway.token")))
	if code, out, _ := exec(t, "", "token"); code != 0 || strings.TrimSpace(out) != tok {
		t.Errorf("token of the service: %q", out)
	}
	// Init says the service does not read the file it writes.
	if _, out, _ := exec(t, "", "-config", filepath.Join(t.TempDir(), "k.yaml"), "init"); !strings.Contains(out, "does not read this file") {
		t.Errorf("init with a service: %s", out)
	}
	// A failing restart is said.
	serviceRunner = func(string, []string, io.Reader, io.Writer, io.Writer) error { return errors.New("exit status 1") }
	if code, _, errOut := exec(t, "", "expose", "local"); code != 1 || !strings.Contains(errOut, "did not restart") {
		t.Errorf("failed restart: %d %s", code, errOut)
	}
	lookPath = func(name string) (string, error) { return "", osexec.ErrNotFound }
	if code, out, _ := exec(t, "", "expose", "lan"); code != 0 || !strings.Contains(out, "systemctl not found") {
		t.Errorf("no systemctl: %d %s", code, out)
	}
	// -config means a gateway of this user's own, even with the service there.
	if _, out, _ := exec(t, "", "-config", filepath.Join(t.TempDir(), "k.yaml"), "config"); !strings.Contains(out, "this user's") {
		t.Errorf("-config: %s", out)
	}
	_ = root
}

func TestSetupAsksForRootForTheService(t *testing.T) {
	_, calls, root := serviceSetup(t, "KITTIES_LISTEN=127.0.0.1:7488\n")
	*root = false
	// This user cannot read the service's files: the command runs again through sudo.
	oldReadable := readable
	t.Cleanup(func() { readable = oldReadable })
	readable = func(string) bool { return false }
	if code, _, _ := exec(t, "", "expose", "wan", "-yes"); code != 0 {
		t.Error("through sudo")
	}
	if last := (*calls)[len(*calls)-1]; last != "sudo /usr/local/bin/kitties expose wan -yes" {
		t.Errorf("ran %q", last)
	}
	if code, _, _ := exec(t, "", "token"); code != 0 || (*calls)[len(*calls)-1] != "sudo /usr/local/bin/kitties token" {
		t.Errorf("token through sudo: %v", *calls)
	}
	serviceRunner = func(string, []string, io.Reader, io.Writer, io.Writer) error {
		return errors.New("sudo: a password is required")
	}
	if code, _, errOut := exec(t, "", "config"); code != 1 || !strings.Contains(errOut, "sudo:") {
		t.Errorf("sudo failed: %d %s", code, errOut)
	}
	serviceRunner = func(string, []string, io.Reader, io.Writer, io.Writer) error {
		return osexec.Command("sh", "-c", "exit 3").Run()
	}
	if code, _, _ := exec(t, "", "config"); code != 3 {
		t.Errorf("the exit code of the command run through sudo: %d", code)
	}
	selfExe = func() (string, error) { return "", errors.New("no executable") }
	if code, _, _ := exec(t, "", "config"); code != 1 {
		t.Error("no executable")
	}
	lookPath = func(name string) (string, error) { return "", osexec.ErrNotFound }
	if code, _, errOut := exec(t, "", "config"); code != 1 || !strings.Contains(errOut, "need root") {
		t.Errorf("no sudo: %d %s", code, errOut)
	}
	// An environment file that cannot be read is said.
	*root = true
	_ = os.Remove(serviceEnvFile)
	_ = os.Mkdir(serviceEnvFile, 0o700)
	if code, _, errOut := exec(t, "", "config"); code != 1 || !strings.Contains(errOut, "service's settings") {
		t.Errorf("an environment file that cannot be read: %d %s", code, errOut)
	}
}

func TestUnknownCommandsLoadNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "k.yaml")
	_ = os.WriteFile(cfg, []byte("providers:\n  fast:\n    kind: openai\n"), 0o600)
	t.Setenv("OPENAI_API_KEY", "")
	code, _, errOut := exec(t, "", "-config", cfg, "frobnicate")
	if code != 1 || !strings.Contains(errOut, `unknown command "frobnicate"`) || strings.Contains(errOut, "provider") {
		t.Errorf("unknown command: %d %s", code, errOut)
	}
}

func TestSetupHelpers(t *testing.T) {
	cases := []struct{ in, key, value, want string }{
		{"", "public_url", "https://a", "gateway:\n  public_url: \"https://a\"\n"},
		{"data_dir: x\n", "trust_proxy", "true", "data_dir: x\n\ngateway:\n  trust_proxy: true\n"},
		{"data_dir: x\n", "public_url", "", "data_dir: x\n"},
		{"gateway:\n    listen: 127.0.0.1:7488\n    token: \"\"   # c\nconnectors: {}\n", "listen", "127.0.0.1:9", "gateway:\n    listen: \"127.0.0.1:9\"\n    token: \"\"   # c\nconnectors: {}\n"},
		{"gateway: # the API\n  listen: a:1\n\nconnectors: {}\n", "public_url", "https://b", "gateway: # the API\n  public_url: \"https://b\"\n  listen: a:1\n\nconnectors: {}\n"},
		{"gateway:\n  listen: a:1\n  public_url: x\n", "public_url", "", "gateway:\n  listen: a:1\n"},
		{"gateway:\n  listen: a:1\n", "public_url", "", "gateway:\n  listen: a:1\n"},
		{"gateway:\nconnectors: {}\n", "trust_proxy", "false", "gateway:\n  trust_proxy: false\nconnectors: {}\n"},
		{"gateway:\n  mcp:\n    listen: no\n", "listen", "a:2", "gateway:\n  listen: \"a:2\"\n  mcp:\n    listen: no\n"},
	}
	for _, c := range cases {
		got, err := setGatewayKey(c.in, c.key, c.value)
		if err != nil || got != c.want {
			t.Errorf("setGatewayKey(%q, %s, %q) = %q %v, want %q", c.in, c.key, c.value, got, err, c.want)
		}
	}
	if _, err := setGatewayKey("gateway: {listen: a:1}\n", "listen", "b:2"); err == nil {
		t.Error("a gateway block on one line is left alone")
	}
	if got := setEnvLine("A=1\nB=2\nA=3\n", "A", "4"); got != "A=4\nB=2\n" {
		t.Errorf("setEnvLine: %q", got)
	}
	if got := setEnvLine("", "A", "1"); got != "A=1\n" {
		t.Errorf("setEnvLine on nothing: %q", got)
	}
	if got := setEnvLine("A=1\nB=2\n", "A", ""); got != "B=2\n" {
		t.Errorf("setEnvLine remove: %q", got)
	}
	env := parseEnv("# c\n\nA = \"x\"\nB='y'\nnothing\n")
	if env["A"] != "x" || env["B"] != "y" || len(env) != 2 {
		t.Errorf("parseEnv: %v", env)
	}
	if line, err := readLine(strings.NewReader("abc")); line != "abc" || err == nil {
		t.Errorf("readLine without a newline: %q %v", line, err)
	}
	s := &setup{cfg: config.Config{Gateway: config.Gateway{Listen: "nonsense", TLSCert: "c"}}}
	if s.openURL() != "https://nonsense" {
		t.Errorf("openURL: %s", s.openURL())
	}
	old := lanIP
	t.Cleanup(func() { lanIP = old })
	lanIP = func() string { return "" }
	s.cfg.Gateway = config.Gateway{Listen: "0.0.0.0:1"}
	if s.openURL() != "http://127.0.0.1:1" {
		t.Errorf("openURL without a LAN: %s", s.openURL())
	}
}
