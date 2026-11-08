package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/kitty"
)

// pruningContainers is a runtime that can delete the images updates leave behind.
type pruningContainers struct {
	*fakeContainers
	pruned int
	err    error
}

func (p *pruningContainers) PruneImages(context.Context) (string, error) {
	p.pruned++
	return "2.4GB", p.err
}

// An update that made a desktop anew deletes the image it left behind; a round that renewed
// nothing does not prune, and a runtime that cannot prune is fine.
func TestUpdatesPruneTheImagesLeftBehind(t *testing.T) {
	ctx := context.Background()
	p := &pruningContainers{fakeContainers: newFake()}
	m, err := New(testConfig(t), WithContainers(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.Create(ctx, Spec{Name: "tom"})
	_ = m.Start(ctx, "tom")
	hear := func(*kitty.Kitty) {}
	var warned []string
	warn := func(format string, args ...any) { warned = append(warned, format) }
	m.CheckUpdates(ctx, hear, warn)
	if p.pruned != 0 {
		t.Errorf("nothing stale, pruned %d", p.pruned)
	}
	p.stale = true
	m.CheckUpdates(ctx, hear, warn)
	if p.pruned != 1 || len(warned) != 0 {
		t.Errorf("after a renewal: pruned %d warned %v", p.pruned, warned)
	}
	p.err = errors.New("no daemon")
	m.CheckUpdates(ctx, hear, warn)
	if len(warned) != 1 || !strings.Contains(warned[0], "update") {
		t.Errorf("a failed prune is warned: %v", warned)
	}
	k, _ := m.Get("tom")
	if err := m.update(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if p.pruned != 3 {
		t.Errorf("a manual update prunes too: %d", p.pruned)
	}
	if freed, err := m.PruneImages(ctx); freed != "2.4GB" || err == nil {
		t.Errorf("PruneImages = %q %v", freed, err)
	}
	// A runtime without the prune.
	plain, _ := New(testConfig(t), WithContainers(newFake(), nil))
	if freed, err := plain.PruneImages(ctx); freed != "" || err != nil {
		t.Errorf("no prune = %q %v", freed, err)
	}
}
