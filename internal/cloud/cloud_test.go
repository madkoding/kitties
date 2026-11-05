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
