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
	_, e1 := c.ExecInput(ctx, "n", "")
	_, e2 := c.Stream(ctx, "n", 1, 1)
	_, e3 := c.Tunnel(ctx, "n", 1)
	for _, err := range []error{e1, e2, e3} {
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

func TestContainersExecQuotesAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	c := &Containers{P: f}
	if _, err := c.Up(ctx, container.Spec{Name: "kitty-a"}); err != nil {
		t.Fatal(err)
	}
	out, err := c.Exec(ctx, "kitty-a", "echo", "it's; rm -rf /")
	if err != nil || !strings.Contains(out, `'it'\''s; rm -rf /'`) {
		t.Fatalf("exec = %q %v", out, err)
	}
	cmd := f.Commands[len(f.Commands)-1]
	if !strings.HasPrefix(cmd, "cd /home/kitty/work && sudo -n -H -u kitty env DISPLAY=:99 'echo' ") {
		t.Errorf("command = %q", cmd)
	}
	if _, err := c.Exec(ctx, "kitty-a"); err == nil {
		t.Error("nothing to run")
	}
	if _, err := c.Exec(ctx, "kitty-none", "true"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
	f.ExecOut = func(string) ExecResult { return ExecResult{Stdout: "partial", Stderr: "boom", ExitCode: 2} }
	if out, err := c.Exec(ctx, "kitty-a", "false"); err == nil || !strings.Contains(err.Error(), "exit status 2") || out != "partialboom" {
		t.Errorf("failure = %q %v", out, err)
	}
	f.ExecOut = func(string) ExecResult { return ExecResult{TimedOut: true, ExitCode: -1} }
	if _, err := c.Exec(ctx, "kitty-a", "sleep"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout = %v", err)
	}
	_ = c.Stop(ctx, "kitty-a")
	if _, err := c.Exec(ctx, "kitty-a", "true"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("exec on a sleeper = %v", err)
	}
}

func TestContainersScreenshotDecodesTheGuestPicture(t *testing.T) {
	ctx := context.Background()
	f := NewFake(5)
	c := &Containers{P: f}
	_, _ = c.Up(ctx, container.Spec{Name: "kitty-a"})
	f.ExecOut = func(string) ExecResult { return ExecResult{Stdout: "UE5HZGF0YQ==\n"} } // "PNGdata"
	pic, err := c.Screenshot(ctx, "kitty-a")
	if err != nil || string(pic) != "PNGdata" {
		t.Fatalf("screenshot = %q %v", pic, err)
	}
	if last := f.Commands[len(f.Commands)-1]; !strings.Contains(last, "kitty-shot | base64 -w0") {
		t.Errorf("command = %q", last)
	}
	for name, res := range map[string]ExecResult{
		"fails":     {ExitCode: 1, Stderr: "no display"},
		"truncated": {Stdout: "UE5H", Truncated: true},
		"garbage":   {Stdout: "!!!"},
		"empty":     {},
	} {
		f.ExecOut = func(string) ExecResult { return res }
		if _, err := c.Screenshot(ctx, "kitty-a"); err == nil {
			t.Errorf("%s must fail", name)
		}
	}
	if _, err := c.Screenshot(ctx, "kitty-none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown = %v", err)
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

// zonedRecorder is a local side that tells time zones.
type zonedRecorder struct {
	recorder
	asked []string
}

func (z *zonedRecorder) ZoneStale(_ context.Context, name, zone string) (bool, error) {
	z.asked = append(z.asked, name+" "+zone)
	return true, nil
}

func TestRouterAsksTheLocalSideForZones(t *testing.T) {
	ctx := context.Background()
	local := &zonedRecorder{}
	r := &Router{Local: local, Cloud: &recorder{}, IsCloud: func(n string) bool { return strings.HasPrefix(n, "far-") }}
	if s, err := r.ZoneStale(ctx, "home", "America/Santiago"); err != nil || !s || len(local.asked) != 1 {
		t.Errorf("local: %v %v %v", s, err, local.asked)
	}
	if s, _ := r.ZoneStale(ctx, "far-1", "America/Santiago"); s || len(local.asked) != 1 {
		t.Error("a cloud machine's zone is left as it is")
	}
	// A local side that cannot tell, or none: never moved.
	if s, _ := (&Router{Local: &recorder{}}).ZoneStale(ctx, "home", "UTC"); s {
		t.Error("a side without zones")
	}
	if s, _ := (&Router{}).ZoneStale(ctx, "home", "UTC"); s {
		t.Error("no local side")
	}
}

// A machine is put in the spec's zone, or the host's, when it is made and each time it wakes.
func TestContainersPutTheMachineInTheHostsZone(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TZ", "America/Santiago")
	f := NewFake(5)
	c := &Containers{P: f}
	zoneCmds := func() []string {
		var out []string
		for _, cmd := range f.Commands {
			if strings.Contains(cmd, "/etc/localtime") {
				out = append(out, cmd)
			}
		}
		return out
	}
	if _, err := c.Up(ctx, container.Spec{Name: "kitty-a"}); err != nil {
		t.Fatal(err)
	}
	got := zoneCmds()
	if len(got) != 1 || !strings.HasPrefix(got[0], "z='America/Santiago';") || !strings.Contains(got[0], "sudo -n ln -sf") {
		t.Fatalf("made: %q", got)
	}
	// The spec's own zone wins, and a woken machine gets it again.
	_ = c.Stop(ctx, "kitty-a")
	if _, err := c.Up(ctx, container.Spec{Name: "kitty-a", Env: map[string]string{"TZ": "Asia/Tokyo"}}); err != nil {
		t.Fatal(err)
	}
	_ = c.Stop(ctx, "kitty-a")
	if err := c.Start(ctx, "kitty-a"); err != nil {
		t.Fatal(err)
	}
	got = zoneCmds()
	if len(got) != 3 || !strings.HasPrefix(got[1], "z='Asia/Tokyo';") || !strings.HasPrefix(got[2], "z='Asia/Tokyo';") {
		t.Errorf("woken: %q", got)
	}
	// A machine the gateway never made here takes the host's zone; quotes stay quoted.
	f2 := NewFake(5)
	m, _ := Ensure(ctx, f2, CreateRequest{Key: "k", Name: "kitty-b"})
	_, _ = f2.Sleep(ctx, m.ID)
	c2 := &Containers{P: f2}
	t.Setenv("TZ", "Europe/Madrid")
	if err := c2.Start(ctx, "kitty-b"); err != nil {
		t.Fatal(err)
	}
	if len(f2.Commands) != 1 || !strings.HasPrefix(f2.Commands[0], "z='Europe/Madrid';") {
		t.Errorf("unknown machine: %q", f2.Commands)
	}
	// No zone at all: nothing is run.
	c3 := &Containers{P: NewFake(5)}
	c3.setZone(ctx, "kitty-c", "id", "")
	if _, ok := c3.zones.Load("kitty-c"); ok {
		t.Error("an empty zone was remembered")
	}
	// A failed wake fails the start before any zone.
	if err := c2.Start(ctx, "kitty-none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}
