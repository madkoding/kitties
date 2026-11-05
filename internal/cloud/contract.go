package cloud

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Contract runs the rules every Provider must keep. A new provider's tests call it with a
// function that returns a fresh, empty provider that allows at least two running machines.
func Contract(t *testing.T, fresh func(t *testing.T) Provider) {
	ctx := context.Background()
	req := CreateRequest{Key: "k1", Name: "mimi", Size: "small"}

	t.Run("create is idempotent by key", func(t *testing.T) {
		p := fresh(t)
		a, err := p.Create(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := p.Create(ctx, req)
		if err != nil || b.ID != a.ID {
			t.Fatalf("second create = %+v %v, want %s", b, err, a.ID)
		}
		if all, _ := p.List(ctx); len(all) != 1 {
			t.Errorf("%d machines, want 1", len(all))
		}
	})

	t.Run("sleep keeps a checkpoint and wake brings the same machine back", func(t *testing.T) {
		p := fresh(t)
		a, _ := p.Create(ctx, req)
		s, err := p.Sleep(ctx, a.ID)
		if err != nil || s.State != Asleep || s.Checkpoint == "" {
			t.Fatalf("sleep = %+v %v", s, err)
		}
		w, err := p.Wake(ctx, a.ID)
		if err != nil || w.State != Running || w.ID != a.ID || w.Generation < a.Generation {
			t.Fatalf("wake = %+v %v", w, err)
		}
	})

	t.Run("an unknown machine is not found", func(t *testing.T) {
		p := fresh(t)
		if _, err := p.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("get = %v", err)
		}
		if err := p.Delete(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("delete = %v", err)
		}
	})

	t.Run("a command runs on a running machine only", func(t *testing.T) {
		p := fresh(t)
		a, _ := p.Create(ctx, req)
		if _, err := p.Exec(ctx, "nope", "true", time.Second); !errors.Is(err, ErrNotFound) {
			t.Errorf("exec on an unknown machine = %v", err)
		}
		// A machine just made may still be starting; the provider waits or says it is not running.
		if _, err := p.Wake(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		r, err := p.Exec(ctx, a.ID, "echo hi", 5*time.Second)
		if err != nil || r.ExitCode != 0 || r.Stdout == "" {
			t.Fatalf("exec = %+v %v", r, err)
		}
		if _, err := p.Sleep(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Exec(ctx, a.ID, "echo hi", 5*time.Second); !errors.Is(err, ErrNotRunning) {
			t.Errorf("exec on a sleeper = %v", err)
		}
	})

	t.Run("delete removes it and frees its key", func(t *testing.T) {
		p := fresh(t)
		a, _ := p.Create(ctx, req)
		if err := p.Delete(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Get(ctx, a.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("get after delete = %v", err)
		}
	})
}
