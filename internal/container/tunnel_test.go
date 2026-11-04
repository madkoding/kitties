package container

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTunnel(t *testing.T) {
	var got []string
	old := startTunnel
	startTunnel = func(_ context.Context, bin string, a ...string) (io.WriteCloser, io.ReadCloser, func() error, error) {
		got = append([]string{bin}, a...)
		r, w := io.Pipe()
		// An echo: what is written comes back.
		return w, r, func() error { return nil }, nil
	}
	t.Cleanup(func() { startTunnel = old })
	m := New(Runtime{Bin: "docker"})
	c, err := m.Tunnel(context.Background(), "kitty-ana", VoxlPort)
	if err != nil {
		t.Fatal(err)
	}
	if want := "docker exec -i kitty-ana socat - TCP:127.0.0.1:6901,nodelay"; strings.Join(got, " ") != want {
		t.Errorf("command = %q, want %q", strings.Join(got, " "), want)
	}
	go func() { _, _ = c.Write([]byte("hello")) }()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "hello" {
		t.Errorf("read %q %v", buf, err)
	}
	if err := c.Close(); err != nil || c.Close() != nil {
		t.Errorf("close twice = %v", err)
	}
	for _, name := range []string{"a b", "-x", ""} {
		if _, err := m.Tunnel(context.Background(), name, VoxlPort); err == nil {
			t.Errorf("%q must fail", name)
		}
	}
	if _, err := m.Tunnel(context.Background(), "kitty-ana", 0); err == nil {
		t.Error("port 0 must fail")
	}
}

// The real command: a stand-in for the engine that echoes its input, as socat does a connection.
func TestTunnelRunsTheCommand(t *testing.T) {
	// A script that ignores its arguments and echoes, in place of `docker exec -i name socat ...`.
	dir := t.TempDir()
	bin := filepath.Join(dir, "engine")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(Runtime{Bin: bin})
	c, err := m.Tunnel(context.Background(), "kitty-ana", VoxlPort)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping\n" {
		t.Errorf("echo = %q %v", buf, err)
	}
	if c.LocalAddr().Network() == "" || c.RemoteAddr().String() == "" {
		t.Error("a connection has addresses")
	}
	for _, set := range []func(time.Time) error{c.SetDeadline, c.SetReadDeadline, c.SetWriteDeadline} {
		if set(time.Time{}) != nil {
			t.Error("deadlines are accepted")
		}
	}
	if err := c.Close(); err != nil {
		t.Error(err)
	}
	// A program that does not exist cannot be started.
	if _, err := New(Runtime{Bin: filepath.Join(dir, "none")}).Tunnel(context.Background(), "kitty-ana", VoxlPort); err == nil {
		t.Error("no engine, no tunnel")
	}
}

// With an address on the engine's network the connection is direct: no command is run.
func TestTunnelDirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = io.Copy(c, c)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	oldRun, oldStart := runCmd, startTunnel
	t.Cleanup(func() { runCmd, startTunnel = oldRun, oldStart; addrCache.Delete("kitty-direct") })
	runCmd = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] != "inspect" {
			t.Errorf("unexpected command %v", args)
		}
		return []byte("bad 127.0.0.1 "), nil
	}
	startTunnel = func(context.Context, string, ...string) (io.WriteCloser, io.ReadCloser, func() error, error) {
		t.Error("the direct connection needs no command")
		return nil, nil, nil, errors.New("no")
	}
	c, err := New(Runtime{Bin: "docker"}).Tunnel(context.Background(), "kitty-direct", port)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte("hi"))
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "hi" {
		t.Errorf("echo = %q %v", buf, err)
	}
}

// An address that cannot be reached falls back to the command, and is not tried again for a while.
func TestTunnelDirectUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listens there now
	oldRun, oldStart := runCmd, startTunnel
	t.Cleanup(func() { runCmd, startTunnel = oldRun, oldStart; addrCache.Delete("kitty-far") })
	inspects, starts := 0, 0
	runCmd = func(context.Context, string, ...string) ([]byte, error) { inspects++; return []byte("127.0.0.1"), nil }
	startTunnel = func(context.Context, string, ...string) (io.WriteCloser, io.ReadCloser, func() error, error) {
		starts++
		r, w := io.Pipe()
		return w, r, func() error { return nil }, nil
	}
	m := New(Runtime{Bin: "docker"})
	for i := 0; i < 2; i++ {
		c, err := m.Tunnel(context.Background(), "kitty-far", port)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if inspects != 1 || starts != 2 {
		t.Errorf("inspects=%d starts=%d, want 1 and 2", inspects, starts)
	}
}
