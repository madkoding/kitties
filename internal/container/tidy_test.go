package container

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUpBoundsTheLogs(t *testing.T) {
	f := &fake{answer: ok}
	f.install(t, "docker")
	if _, err := New(Runtime{Bin: "/usr/bin/docker"}).Up(context.Background(), Spec{Name: "kitty-a", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(f.calls[len(f.calls)-1], " ")
	if !strings.Contains(got, "--log-driver json-file --log-opt max-size=10m --log-opt max-file=3") {
		t.Errorf("docker run = %s", got)
	}
	if _, err := New(Runtime{Bin: "/usr/bin/podman"}).Up(context.Background(), Spec{Name: "kitty-a", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(f.calls[len(f.calls)-1], " ")
	if !strings.Contains(got, "--log-opt max-size=10m") || strings.Contains(got, "max-file") || strings.Contains(got, "--log-driver") {
		t.Errorf("podman run = %s", got)
	}
	// A spec with its own options keeps them.
	if _, err := New(Runtime{Bin: "docker"}).Up(context.Background(), Spec{Name: "kitty-a", Image: "img", Logs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if got = strings.Join(f.calls[len(f.calls)-1], " "); strings.Contains(got, "--log-opt") {
		t.Errorf("own options = %s", got)
	}
}

func TestPruneImages(t *testing.T) {
	f := &fake{answer: func([]string) ([]byte, error) {
		return []byte("Deleted Images:\nuntagged: x\n\nTotal reclaimed space: 2.4GB\n"), nil
	}}
	f.install(t, "docker")
	m := New(Runtime{Bin: "docker"})
	freed, err := m.PruneImages(context.Background())
	if err != nil || freed != "2.4GB" {
		t.Errorf("prune = %q %v", freed, err)
	}
	if got := strings.Join(f.calls[0], " "); got != "image prune -f --filter label=kitties.home" {
		t.Errorf("prune ran %q", got)
	}
	f.answer = func([]string) ([]byte, error) { return []byte("Total reclaimed space: 0B"), nil }
	if freed, _ := m.PruneImages(context.Background()); freed != "0B" {
		t.Errorf("nothing = %q", freed)
	}
	f.answer = func([]string) ([]byte, error) { return nil, nil }
	if freed, _ := m.PruneImages(context.Background()); freed != "" {
		t.Errorf("podman's silence = %q", freed)
	}
	f.answer = func([]string) ([]byte, error) { return []byte("no daemon"), errors.New("x") }
	if _, err := m.PruneImages(context.Background()); err == nil || !strings.Contains(err.Error(), "no daemon") {
		t.Errorf("failure: %v", err)
	}
}
