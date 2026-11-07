package cloud

import (
	"context"
	"strings"
	"testing"
)

type limitedRecorder struct {
	recorder
	set []string
}

func (l *limitedRecorder) SetLimits(_ context.Context, name string, memoryMB, swapMB int) error {
	l.set = append(l.set, name)
	return nil
}

func (l *limitedRecorder) LimitsStale(context.Context, string, int, int) (bool, error) {
	return true, nil
}

func (l *limitedRecorder) EngineMemoryMB(context.Context) (int, error) { return 8192, nil }

func TestRouterSizesOnlyLocalMachines(t *testing.T) {
	ctx := context.Background()
	local := &limitedRecorder{}
	r := &Router{Local: local, Cloud: &recorder{}, IsCloud: func(n string) bool { return strings.HasPrefix(n, "far-") }}
	if err := r.SetLimits(ctx, "home", 2048, 1024); err != nil || len(local.set) != 1 {
		t.Errorf("local: %v %v", err, local.set)
	}
	if err := r.SetLimits(ctx, "far-1", 2048, 1024); err == nil || len(local.set) != 1 {
		t.Errorf("a cloud machine has its provider's size: %v", err)
	}
	if s, _ := r.LimitsStale(ctx, "home", 1, 1); !s {
		t.Error("local stale")
	}
	if s, _ := r.LimitsStale(ctx, "far-1", 1, 1); s {
		t.Error("a cloud machine is never stale")
	}
	if n, err := r.EngineMemoryMB(ctx); n != 8192 || err != nil {
		t.Errorf("engine = %d %v", n, err)
	}
	plain := &Router{Local: &recorder{}}
	if err := plain.SetLimits(ctx, "home", 1, 1); err == nil {
		t.Error("an engine that cannot")
	}
	if _, err := plain.EngineMemoryMB(ctx); err == nil {
		t.Error("an engine that cannot tell")
	}
}
