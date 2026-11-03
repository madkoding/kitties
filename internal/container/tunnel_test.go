package container

import (
	"context"
	"io"
	"strings"
	"testing"
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
	if want := "docker exec -i kitty-ana socat - TCP:127.0.0.1:6901"; strings.Join(got, " ") != want {
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
