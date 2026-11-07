package container

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLimitArgs(t *testing.T) {
	s := Spec{Name: "kitty-a", KittyName: "a", Image: "img", MemoryMB: 2048, SwapMB: 1024}
	if a := strings.Join(s.Args(), " "); !strings.Contains(a, "--memory 2048m --memory-swap 3072m") {
		t.Errorf("args = %s", a)
	}
	s.SwapMB = 0
	if a := strings.Join(s.Args(), " "); !strings.Contains(a, "--memory 2048m --memory-swap 2048m") {
		t.Errorf("no swap = %s", a)
	}
	if limitArgs(0, 1024) != nil {
		t.Error("no RAM limit leaves both to the engine")
	}
	if err := (Spec{Name: "kitty-a", Image: "img", SwapMB: -1}).Validate(); err == nil {
		t.Error("negative swap")
	}
}

func TestSetLimitsAndStale(t *testing.T) {
	f := &fake{answer: func(args []string) ([]byte, error) {
		if args[0] == "inspect" {
			return []byte("2147483648 3221225472\n"), nil
		}
		return nil, nil
	}}
	f.install(t, "docker")
	m := New(Runtime{Bin: "/usr/bin/docker"})
	ctx := context.Background()
	if err := m.SetLimits(ctx, "kitty-a", 2048, 1024); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls[len(f.calls)-1], " "); got != "update --memory 2048m --memory-swap 3072m kitty-a" {
		t.Errorf("update = %q", got)
	}
	if err := m.SetLimits(ctx, "Bad Name", 2048, 0); err == nil {
		t.Error("bad name")
	}
	if err := m.SetLimits(ctx, "kitty-a", 0, 0); err == nil {
		t.Error("no limit cannot be set in place")
	}
	if stale, err := m.LimitsStale(ctx, "kitty-a", 2048, 1024); err != nil || stale {
		t.Errorf("same limits = %v %v", stale, err)
	}
	if stale, _ := m.LimitsStale(ctx, "kitty-a", 4096, 1024); !stale {
		t.Error("other RAM")
	}
	if stale, _ := m.LimitsStale(ctx, "kitty-a", 0, 0); !stale {
		t.Error("a limit where none should be")
	}
	if _, err := m.LimitsStale(ctx, "Bad Name", 1, 1); err == nil {
		t.Error("bad name")
	}
	for _, answer := range []string{"one", "x 1", ""} {
		f.answer = func([]string) ([]byte, error) { return []byte(answer), nil }
		if _, err := m.LimitsStale(ctx, "kitty-a", 1, 1); err == nil {
			t.Errorf("answer %q", answer)
		}
	}
	f.answer = func([]string) ([]byte, error) { return []byte("no such container"), errors.New("exit 1") }
	if _, err := m.LimitsStale(ctx, "kitty-a", 1, 1); err == nil || !strings.Contains(err.Error(), "no such container") {
		t.Errorf("failed inspect = %v", err)
	}
}

func TestEngineMemory(t *testing.T) {
	f := &fake{answer: func(args []string) ([]byte, error) { return []byte("8589934592\n"), nil }}
	f.install(t, "docker")
	ctx := context.Background()
	if n, err := New(Runtime{Bin: "/usr/bin/docker"}).EngineMemoryMB(ctx); err != nil || n != 8192 {
		t.Errorf("docker = %d %v", n, err)
	}
	if _, err := New(Runtime{Bin: "/usr/bin/podman"}).EngineMemoryMB(ctx); err != nil || f.calls[len(f.calls)-1][2] != "{{.Host.MemTotal}}" {
		t.Errorf("podman asks its own way: %v %v", f.calls, err)
	}
	f.answer = func([]string) ([]byte, error) { return []byte("oops"), nil }
	if _, err := New(Runtime{Bin: "docker"}).EngineMemoryMB(ctx); err == nil {
		t.Error("bad answer")
	}
	f.answer = func([]string) ([]byte, error) { return []byte("down"), errors.New("x") }
	if _, err := New(Runtime{Bin: "docker"}).EngineMemoryMB(ctx); err == nil {
		t.Error("engine down")
	}
}
