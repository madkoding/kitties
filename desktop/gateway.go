package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// THE GATEWAY THAT TRAVELS WITH THE APP.
//
// The app is only a window onto a kitties gateway. A person who downloads it should not have to
// install and start anything else, so the release puts a `kitties-gateway` next to the app (in
// Kitties.app/Contents/MacOS on a Mac) and the app starts it when nothing answers on the
// address it is set to, and stops it when it quits. A gateway that is already running, a remote
// one, or a build without the file are all left alone.

// bundledGateway is the gateway shipped next to the app, or "" when there is none. It is a
// variable so tests can point it elsewhere.
var bundledGateway = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "kitties-gateway"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(filepath.Dir(exe), name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// gatewayUp reports whether a gateway answers at the connection's address.
func (a *App) gatewayUp() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.conn.URL+"/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// localListen is the address to start the gateway on when the connection points at this
// machine, and false when it points anywhere else (then it is not ours to start).
func localListen(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return "", false
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	return net.JoinHostPort("127.0.0.1", port), true
}

// startGateway starts the bundled gateway when none answers, waits for it to come up and picks
// up the token it leaves behind. It returns what it did, for the log.
func (a *App) startGateway() string {
	if a.gatewayUp() {
		return "a gateway already answers at " + a.conn.URL
	}
	listen, ok := localListen(a.conn.URL)
	if !ok {
		return "no gateway answers at " + a.conn.URL + " and it is not this machine's to start"
	}
	bin := bundledGateway()
	if bin == "" {
		return "no gateway answers at " + a.conn.URL + " and none is bundled with this build"
	}
	cmd := exec.Command(bin, "gateway")
	cmd.Env = append(os.Environ(), "KITTIES_LISTEN="+listen)
	hideWindow(cmd)
	logPath := filepath.Join(filepath.Dir(configPath()), "gateway.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			cmd.Stdout, cmd.Stderr = f, f
			defer f.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("could not start the bundled gateway %s: %v", bin, err)
	}
	a.mu.Lock()
	a.gateway = cmd
	a.mu.Unlock()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	deadline := time.Now().Add(gatewayStartTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return "the bundled gateway stopped at once: see " + logPath
		default:
		}
		if a.gatewayUp() {
			// It writes its token next to the configuration; the app uses it without typing.
			if a.conn.Token == "" {
				a.conn.Token = localToken()
			}
			return "started the bundled gateway on " + listen
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "the bundled gateway did not answer in time: see " + logPath
}

// gatewayStartTimeout is how long the app waits for the gateway it started.
var gatewayStartTimeout = 10 * time.Second

// stopGateway stops the gateway the app started, if it did.
func (a *App) stopGateway() {
	a.mu.Lock()
	cmd := a.gateway
	a.gateway = nil
	a.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
