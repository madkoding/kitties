package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for the gateway program: started with the argument
// "gateway" and DESKTOP_TEST_GATEWAY set, it behaves as the variable says.
func TestMain(m *testing.M) {
	if mode := os.Getenv("DESKTOP_TEST_GATEWAY"); mode != "" && len(os.Args) > 1 && os.Args[1] == "gateway" {
		switch mode {
		case "crash":
			os.Exit(3)
		case "hang":
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		home, _ := os.UserHomeDir()
		_ = os.MkdirAll(filepath.Join(home, ".kitties"), 0o700)
		_ = os.WriteFile(filepath.Join(home, ".kitties", "gateway.token"), []byte("fresh-token\n"), 0o600)
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
		_ = http.ListenAndServe(os.Getenv("KITTIES_LISTEN"), mux)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestLocalListen(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:7488": "127.0.0.1:7488",
		"http://localhost:9000": "127.0.0.1:9000",
		"http://[::1]:7488":     "127.0.0.1:7488",
		"http://localhost":      "127.0.0.1:80",
	} {
		if got, ok := localListen(in); !ok || got != want {
			t.Errorf("localListen(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"https://localhost:7488", "http://kitties.example.com:7488", "http://10.0.0.5:7488", "://bad"} {
		if _, ok := localListen(in); ok {
			t.Errorf("%q is not this machine's to start", in)
		}
	}
}

func TestStartGatewayLeavesAloneWhatIsNotItsToStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) }))
	defer up.Close()
	a := NewApp()
	a.conn = Connection{URL: up.URL}
	if note := a.startGateway(); !strings.Contains(note, "already answers") || a.gateway != nil {
		t.Errorf("a gateway is running: %q", note)
	}
	a.conn = Connection{URL: "http://kitties.example.invalid:7488"}
	if note := a.startGateway(); !strings.Contains(note, "not this machine's to start") {
		t.Errorf("remote: %q", note)
	}
	old := bundledGateway
	defer func() { bundledGateway = old }()
	bundledGateway = func() string { return "" }
	a.conn = Connection{URL: "http://" + freePort(t)}
	if note := a.startGateway(); !strings.Contains(note, "none is bundled") {
		t.Errorf("no bundle: %q", note)
	}
	if bundledGateway() != "" {
		t.Error("the stub is empty")
	}
	if got := old(); got != "" {
		t.Errorf("no gateway next to the test binary, got %q", got)
	}
	a.stopGateway() // nothing started: nothing to stop
}

func TestStartGatewayRunsTheBundledOneAndStopsIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DESKTOP_TEST_GATEWAY", "serve")
	old := bundledGateway
	defer func() { bundledGateway = old }()
	bundledGateway = func() string { return os.Args[0] }
	addr := freePort(t)
	a := NewApp()
	a.conn = Connection{URL: "http://" + addr}
	note := a.startGateway()
	if !strings.Contains(note, "started the bundled gateway on "+addr) {
		t.Fatalf("note = %q", note)
	}
	if a.conn.Token != "fresh-token" {
		t.Errorf("the token the gateway left is picked up: %q", a.conn.Token)
	}
	if !a.gatewayUp() {
		t.Fatal("the gateway answers")
	}
	a.stopGateway()
	for deadline := time.Now().Add(5 * time.Second); a.gatewayUp() && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
	}
	if a.gatewayUp() {
		t.Error("the gateway the app started stops with it")
	}
	a.stopGateway() // twice is harmless
}

func TestStartGatewayReportsWhyItFailed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old, oldWait := bundledGateway, gatewayStartTimeout
	defer func() { bundledGateway, gatewayStartTimeout = old, oldWait }()
	gatewayStartTimeout = 400 * time.Millisecond
	a := NewApp()
	bundledGateway = func() string { return os.Args[0] }
	for mode, want := range map[string]string{"crash": "stopped at once", "hang": "did not answer in time"} {
		t.Setenv("DESKTOP_TEST_GATEWAY", mode)
		a.conn = Connection{URL: "http://" + freePort(t)}
		if note := a.startGateway(); !strings.Contains(note, want) || !strings.Contains(note, "gateway.log") {
			t.Errorf("%s: %q", mode, note)
		}
		a.stopGateway()
	}
	bundledGateway = func() string { return filepath.Join(t.TempDir(), "missing") }
	a.conn = Connection{URL: "http://" + freePort(t)}
	if note := a.startGateway(); !strings.Contains(note, "could not start") {
		t.Errorf("missing program: %q", note)
	}
}

func TestGatewayNoteIsKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := NewApp()
	a.startup(t.Context())
	if a.GatewayNote() == "" {
		t.Error("startup says what it did about the gateway")
	}
}
