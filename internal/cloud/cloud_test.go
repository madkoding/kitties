package cloud

import (
	"context"
	"errors"
	"testing"
)

func TestFakeKeepsTheContract(t *testing.T) {
	Contract(t, func(*testing.T) Provider { return NewFake(5) })
}

func TestEnsureAfterALostAnswerFindsTheMachine(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	f.LoseNextCreate()
	m, err := Ensure(ctx, f, CreateRequest{Key: "k1", Name: "mimi"})
	if err != nil || m.Key != "k1" {
		t.Fatalf("ensure = %+v %v", m, err)
	}
	if all, _ := f.List(ctx); len(all) != 1 {
		t.Errorf("%d machines, want 1: a lost answer must not make a blank replacement", len(all))
	}
}

func TestEnsureNeverCreatesWhenItCannotTell(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	f.LoseNextCreate()
	f.FailList(true)
	if _, err := Ensure(ctx, f, CreateRequest{Key: "k1", Name: "mimi"}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ensure = %v, want ErrAmbiguous", err)
	}
	f.FailList(false)
	if all, _ := f.List(ctx); len(all) != 1 {
		t.Errorf("%d machines: nothing may be created behind an ambiguous answer", len(all))
	}
}

func TestEnsureStopsAtTheRunningLimit(t *testing.T) {
	ctx := context.Background()
	f := NewFake(1)
	if _, err := Ensure(ctx, f, CreateRequest{Key: "a", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(ctx, f, CreateRequest{Key: "b", Name: "b"}); !errors.Is(err, ErrLimit) {
		t.Errorf("second = %v, want ErrLimit", err)
	}
}

func TestEnsureNeedsAKeyAndAName(t *testing.T) {
	if _, err := Ensure(context.Background(), NewFake(1), CreateRequest{}); err == nil {
		t.Error("an empty request must fail")
	}
}

func TestFakeUnknownMachineAndWakeLimit(t *testing.T) {
	ctx := context.Background()
	f := NewFake(1)
	if _, err := f.Sleep(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("sleep = %v", err)
	}
	if _, err := f.Wake(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wake = %v", err)
	}
	a, _ := f.Create(ctx, CreateRequest{Key: "a", Name: "a"})
	if _, err := f.Sleep(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Create(ctx, CreateRequest{Key: "b", Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Wake(ctx, a.ID); !errors.Is(err, ErrLimit) {
		t.Errorf("wake over the limit = %v, want ErrLimit", err)
	}
	// Sleeping a sleeper and waking a runner change nothing.
	if m, _ := f.Sleep(ctx, a.ID); m.State != Asleep {
		t.Errorf("state = %s", m.State)
	}
}

type failingCreate struct{ *Fake }

func (failingCreate) Create(context.Context, CreateRequest) (Machine, error) {
	return Machine{}, errors.New("quota exceeded")
}

func TestEnsureReturnsTheCreateErrorWhenNothingWasMade(t *testing.T) {
	_, err := Ensure(context.Background(), failingCreate{NewFake(5)}, CreateRequest{Key: "k", Name: "n"})
	if err == nil || errors.Is(err, ErrAmbiguous) || err.Error() != "quota exceeded" {
		t.Errorf("ensure = %v, want the create error", err)
	}
}

func TestFakeGetAndListFailure(t *testing.T) {
	ctx := context.Background()
	f := NewFake(0)
	a, _ := f.Create(ctx, CreateRequest{Key: "a", Name: "a"})
	if m, err := f.Get(ctx, a.ID); err != nil || m.ID != a.ID {
		t.Errorf("get = %+v %v", m, err)
	}
	f.FailList(true)
	if _, err := f.List(ctx); err == nil {
		t.Error("list must fail while the api is down")
	}
}
