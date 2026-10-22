package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleParses(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "kitties.example.yaml"))
	if err != nil {
		t.Fatalf("the shipped example must load: %v", err)
	}
	if len(cfg.Providers) != 3 || cfg.DefaultProvider != "strong" || len(cfg.Kitties) != 3 || cfg.Kitties[1].Replicas != 3 {
		t.Errorf("config = %+v", cfg)
	}
	if cfg.ProviderFor(cfg.Kitties[0]) != "strong" || cfg.ProviderFor(Kitty{}) != "strong" {
		t.Error("ProviderFor")
	}
	if d := cfg.Kitties[2].Desktop; d == nil || *d {
		t.Error("desktop: false is kept")
	}
	o := cfg.Providers["fast"].Options()
	if o.Kind != "openai" || o.Temperature == nil || *o.Temperature != 0.7 {
		t.Errorf("Options = %+v", o)
	}
	t.Setenv("MY_KEY", "secret")
	if (Provider{APIKeyEnv: "MY_KEY"}).Options().APIKey != "secret" {
		t.Error("api_key_env is read")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "none.yaml")); err == nil {
		t.Error("a missing file fails")
	}
	if _, err := Parse([]byte("a: [\n")); err == nil {
		t.Error("bad YAML fails")
	}
	if _, err := Parse([]byte("nope: 1\n")); err == nil {
		t.Error("an unknown key fails")
	}
}

func TestValidation(t *testing.T) {
	base := "providers:\n  p:\n    kind: ollama\n"
	cases := []struct{ text, want string }{
		{"providers:\n  Bad Name:\n    kind: ollama\n", "profile name"},
		{"providers:\n  p:\n    kind: nope\n", "is not one of"},
		{"providers:\n  p:\n    kind: openai\n    api_key: k\n    base_url: http://example.com/v1\n", "clear text"},
		{base + "default_provider: q\n", "not a configured profile"},
		{base + "kitties:\n  - name: Bad\n", "not a valid name"},
		{base + "kitties:\n  - name: a\n  - name: a\n", "used twice"},
		{base + "kitties:\n  - name: a\n    provider: q\n", "not a configured profile"},
		{base + "  q:\n    kind: ollama\nkitties:\n  - name: a\n", "no default_provider"},
		{base + "kitties:\n  - name: a\n    replicas: 1000\n", "between 0 and 999"},
		{base + "kitties:\n  - name: a\n    connectors: [x]\n", "not a configured connector"},
		{base + "containers:\n  runtime: lxc\n", "not auto, docker or podman"},
		{base + "containers:\n  image: ''\n", "image cannot be empty"},
		{base + "containers:\n  cpus: -1\n", "cannot be negative"},
		{base + "gateway:\n  listen: ''\n", "listen cannot be empty"},
		{base + "connectors:\n  Bad:\n    kind: webhook\n", "connector name"},
		{base + "connectors:\n  w:\n    kind: webhook\n", "needs a url"},
		{base + "connectors:\n  w:\n    kind: telegram\n", "token and a chat_id"},
		{base + "connectors:\n  w:\n    kind: shell\n", "needs a command"},
		{base + "connectors:\n  w:\n    kind: motita\n", "needs a url"},
		{base + "connectors:\n  w:\n    kind: fax\n", "not webhook"},
	}
	for _, tc := range cases {
		_, err := Parse([]byte(tc.text))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.text, err, tc.want)
		}
	}
	good := base + "  k:\n    kind: openai\n    api_key: k\n    base_url: http://localhost:8080/v1\n" +
		"kitties:\n  - name: a\n    provider: p\n    connectors: [t, s, m]\n" +
		"connectors:\n  t:\n    kind: telegram\n    token: x\n    chat_id: '1'\n  s:\n    kind: shell\n    command: echo\n  m:\n    kind: motita\nmotita:\n  url: http://127.0.0.1:7477\n"
	if _, err := Parse([]byte(good)); err != nil {
		t.Errorf("a good configuration parses: %v", err)
	}
	for _, u := range []string{"http://localhost/x", "http://127.0.0.1:1", "http://[::1]:5/", "http://host.docker.internal"} {
		if !isLocal(u) {
			t.Errorf("%s is local", u)
		}
	}
	if isLocal("http://example.com") {
		t.Error("example.com is remote")
	}
}

func TestPaths(t *testing.T) {
	c := Config{DataDir: "/x"}
	if c.DataDirOr() != "/x" {
		t.Error("explicit data_dir")
	}
	c.DataDir = ""
	if !strings.HasSuffix(c.DataDirOr(), ".kitties") {
		t.Errorf("DataDirOr = %s", c.DataDirOr())
	}
	t.Setenv("HOME", "")
	if c.DataDirOr() != ".kitties" {
		t.Errorf("no home: %s", c.DataDirOr())
	}
	if DefaultPath() != "kitties.yaml" {
		t.Errorf("no home: %s", DefaultPath())
	}
	t.Setenv("HOME", t.TempDir())
	if !strings.HasSuffix(DefaultPath(), filepath.Join(".kitties", "kitties.yaml")) {
		t.Errorf("DefaultPath = %s", DefaultPath())
	}
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	_ = os.WriteFile("kitties.yaml", nil, 0o600)
	if DefaultPath() != "kitties.yaml" {
		t.Error("a file in the working directory wins")
	}
	t.Setenv("KITTIES_CONFIG", "/etc/k.yaml")
	if DefaultPath() != "/etc/k.yaml" {
		t.Error("the variable wins")
	}
	if (&Config{}).ProviderFor(Kitty{}) != "" {
		t.Error("no providers, no name")
	}
}
