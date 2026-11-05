package cloud

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/container"
)

func TestContainersLifecycleOverAProvider(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	c := &Containers{P: f, Size: "small"}
	id, err := c.Up(ctx, container.Spec{Name: "kitty-mimi"})
	if err != nil || id == "" {
		t.Fatalf("up = %q %v", id, err)
	}
	if again, err := c.Up(ctx, container.Spec{Name: "kitty-mimi"}); err != nil || again != id {
		t.Errorf("up twice = %q %v, want %q (nothing made twice)", again, err, id)
	}
	if err := c.Stop(ctx, "kitty-mimi"); err != nil {
		t.Fatal(err)
	}
	infos, _ := c.List(ctx)
	if len(infos) != 1 || infos[0].Running || infos[0].Kitty != "mimi" || infos[0].Image != "cloud" {
		t.Errorf("list = %+v", infos)
	}
	if err := c.Start(ctx, "kitty-mimi"); err != nil {
		t.Fatal(err)
	}
	// Up on a sleeper wakes it instead of making another.
	_ = c.Stop(ctx, "kitty-mimi")
	if again, err := c.Up(ctx, container.Spec{Name: "kitty-mimi"}); err != nil || again != id {
		t.Errorf("up on a sleeper = %q %v", again, err)
	}
	if err := c.Remove(ctx, "kitty-mimi"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "kitty-mimi"); err != nil {
		t.Errorf("removing what is gone is not an error: %v", err)
	}
	for _, err := range []error{c.Start(ctx, "kitty-none"), c.Stop(ctx, "kitty-none")} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown = %v", err)
		}
	}
}

func TestContainersNeverTakeAnUnreadableInventoryForEmpty(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	f.FailList(true)
	c := &Containers{P: f}
	if _, err := c.Up(ctx, container.Spec{Name: "kitty-x"}); err == nil {
		t.Error("up must fail when the inventory cannot be read")
	}
	if err := c.Remove(ctx, "kitty-x"); err == nil {
		t.Error("remove must fail when the inventory cannot be read")
	}
	if _, err := c.List(ctx); err == nil {
		t.Error("list must fail")
	}
	f.FailList(false)
	if all, _ := f.List(ctx); len(all) != 0 {
		t.Errorf("%d machines were made behind a failed inventory", len(all))
	}
}

func TestContainersDesktopChannelIsNotAvailableYet(t *testing.T) {
	ctx := context.Background()
	c := &Containers{P: NewFake(1)}
	_, e1 := c.Exec(ctx, "n")
	_, e2 := c.ExecInput(ctx, "n", "")
	_, e3 := c.Screenshot(ctx, "n")
	_, e4 := c.Stream(ctx, "n", 1, 1)
	_, e5 := c.Tunnel(ctx, "n", 1)
	for _, err := range []error{e1, e2, e3, e4, e5} {
		if !errors.Is(err, ErrDesktopChannel) {
			t.Errorf("err = %v", err)
		}
	}
	if err := c.Pull(ctx, "x"); err != nil {
		t.Error(err)
	}
	if stale, err := c.ImageStale(ctx, "n", "x"); stale || err != nil {
		t.Error("a cloud machine has no stale image")
	}
}

// recorder is a Backend that remembers which names reached it.
type recorder struct {
	Containers
	calls []string
	pulls int
	stale bool
}

func (r *recorder) Up(_ context.Context, s container.Spec) (string, error) {
	r.calls = append(r.calls, "up "+s.Name)
	return "id", nil
}
func (r *recorder) Start(_ context.Context, n string) error {
	r.calls = append(r.calls, "start "+n)
	return nil
}
func (r *recorder) Stop(_ context.Context, n string) error {
	r.calls = append(r.calls, "stop "+n)
	return nil
}
func (r *recorder) Remove(_ context.Context, n string) error {
	r.calls = append(r.calls, "remove "+n)
	return nil
}
func (r *recorder) Exec(_ context.Context, n string, _ ...string) (string, error) {
	r.calls = append(r.calls, "exec "+n)
	return "", nil
}
func (r *recorder) ExecInput(_ context.Context, n, _ string, _ ...string) (string, error) {
	r.calls = append(r.calls, "execinput "+n)
	return "", nil
}
func (r *recorder) Screenshot(_ context.Context, n string) ([]byte, error) {
	r.calls = append(r.calls, "shot "+n)
	return nil, nil
}
func (r *recorder) Stream(_ context.Context, n string, _, _ int) (io.ReadCloser, error) {
	r.calls = append(r.calls, "stream "+n)
	return nil, nil
}
func (r *recorder) Tunnel(_ context.Context, n string, _ int) (net.Conn, error) {
	r.calls = append(r.calls, "tunnel "+n)
	return nil, nil
}
func (r *recorder) Pull(context.Context, string) error { r.pulls++; return nil }
func (r *recorder) ImageStale(context.Context, string, string) (bool, error) {
	return r.stale, nil
}
func (r *recorder) List(context.Context) ([]container.Info, error) {
	return []container.Info{{Name: "from-" + strings.Join(r.calls, ",")}}, nil
}

func TestRouterSendsEachNameToItsSide(t *testing.T) {
	ctx := context.Background()
	local, far := &recorder{stale: true}, &recorder{}
	r := &Router{Local: local, Cloud: far, IsCloud: func(n string) bool { return strings.HasPrefix(n, "far-") }}
	for _, name := range []string{"home", "far-1"} {
		_, _ = r.Up(ctx, container.Spec{Name: name})
		_ = r.Start(ctx, name)
		_ = r.Stop(ctx, name)
		_ = r.Remove(ctx, name)
		_, _ = r.Exec(ctx, name)
		_, _ = r.ExecInput(ctx, name, "")
		_, _ = r.Screenshot(ctx, name)
		_, _ = r.Stream(ctx, name, 1, 1)
		_, _ = r.Tunnel(ctx, name, 1)
	}
	if len(local.calls) != 9 || len(far.calls) != 9 {
		t.Fatalf("local %d, cloud %d calls", len(local.calls), len(far.calls))
	}
	for _, c := range local.calls {
		if !strings.HasSuffix(c, " home") {
			t.Errorf("a cloud name reached the local side: %s", c)
		}
	}
	for _, c := range far.calls {
		if !strings.HasSuffix(c, " far-1") {
			t.Errorf("a local name reached the cloud side: %s", c)
		}
	}
	if err := r.Pull(ctx, "img"); err != nil || local.pulls != 1 || far.pulls != 0 {
		t.Error("images are the local side's")
	}
	if s, _ := r.ImageStale(ctx, "home", "img"); !s {
		t.Error("local staleness is asked of the local side")
	}
	if s, _ := r.ImageStale(ctx, "far-1", "img"); s {
		t.Error("a cloud machine is never stale")
	}
	if infos, err := r.List(ctx); err != nil || len(infos) != 2 {
		t.Errorf("list joins both: %v %v", infos, err)
	}
}

func TestRouterMissingSidesFailWithTheirReason(t *testing.T) {
	ctx := context.Background()
	why := errors.New("set BOAT_API_KEY")
	r := &Router{CloudErr: why, IsCloud: func(n string) bool { return n == "far" }}
	if _, err := r.Up(ctx, container.Spec{Name: "far"}); !errors.Is(err, why) {
		t.Errorf("cloud missing = %v", err)
	}
	if _, err := r.Up(ctx, container.Spec{Name: "home"}); err == nil || !strings.Contains(err.Error(), "no container runtime") {
		t.Errorf("local missing = %v", err)
	}
	for _, f := range []func() error{
		func() error { return r.Start(ctx, "far") },
		func() error { return r.Stop(ctx, "far") },
		func() error { return r.Remove(ctx, "far") },
		func() error { _, err := r.Exec(ctx, "far"); return err },
		func() error { _, err := r.ExecInput(ctx, "far", ""); return err },
		func() error { _, err := r.Screenshot(ctx, "far"); return err },
		func() error { _, err := r.Stream(ctx, "far", 1, 1); return err },
		func() error { _, err := r.Tunnel(ctx, "far", 1); return err },
	} {
		if err := f(); !errors.Is(err, why) {
			t.Errorf("err = %v", err)
		}
	}
	if err := r.Pull(ctx, "x"); err != nil {
		t.Error("pull with no local side is nothing to do")
	}
	if s, err := r.ImageStale(ctx, "home", "x"); s || err != nil {
		t.Error("stale with no local side")
	}
	if infos, err := r.List(ctx); err != nil || len(infos) != 0 {
		t.Errorf("list of nothing = %v %v", infos, err)
	}
	nobody := &Router{}
	if _, err := nobody.Up(ctx, container.Spec{Name: "x"}); err == nil {
		t.Error("no sides at all")
	}
}

type failingList struct{ Containers }

func (*failingList) List(context.Context) ([]container.Info, error) { return nil, errors.New("down") }

func TestRouterListToleratesOneSideDown(t *testing.T) {
	ctx := context.Background()
	r := &Router{Local: &failingList{}, Cloud: &recorder{}}
	if infos, err := r.List(ctx); err != nil || len(infos) != 1 {
		t.Errorf("one side down: %v %v", infos, err)
	}
	r = &Router{Local: &failingList{}}
	if _, err := r.List(ctx); err == nil {
		t.Error("every side down is an error")
	}
}
