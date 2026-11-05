package cloud

import (
	"context"
	"errors"
	"testing"
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

	t.Run("sleep keeps a checkpoint and wake restores the same machine", func(t *testing.T) {
		p := fresh(t)
		a, _ := p.Create(ctx, req)
		s, err := p.Sleep(ctx, a.ID)
		if err != nil || s.State != Asleep || s.Checkpoint == "" {
			t.Fatalf("sleep = %+v %v", s, err)
		}
		w, err := p.Wake(ctx, a.ID)
		if err != nil || w.State != Running || w.ID != a.ID || w.Generation <= a.Generation || w.Checkpoint != s.Checkpoint {
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
