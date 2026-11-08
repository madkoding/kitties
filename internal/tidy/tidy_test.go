package tidy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
)

type fakeChats struct {
	before time.Time
	max    int64
	swept  atomic.Int32
}

func (f *fakeChats) DiskUsage() chat.DiskUsage {
	return chat.DiskUsage{History: 100, Files: 200, Shots: 3000, ShotCount: 3}
}
func (f *fakeChats) SweepShots(before time.Time, max int64) (int, int64) {
	f.before, f.max = before, max
	f.swept.Add(1)
	return 2, 2000
}

type fakeSettings struct{ r config.Retention }

func (f *fakeSettings) Retention() config.Retention { return f.r }
func (f *fakeSettings) SetRetention(r config.Retention) error {
	f.r = r
	return nil
}

type fakeImages struct {
	freed string
	err   error
}

func (f *fakeImages) PruneImages(context.Context) (string, error) { return f.freed, f.err }

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
}

func area(u Usage, key string) Area {
	for _, a := range u.Areas {
		if a.Key == key {
			return a
		}
	}
	return Area{}
}

func TestUsageByArea(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "homes", "mimi", ".bashrc"), 4000)
	write(t, filepath.Join(dir, "work", "mimi", "a.txt"), 1000)
	write(t, filepath.Join(dir, "shared", "mimi", "b.pdf"), 500)
	write(t, filepath.Join(dir, "backups", "k1.bin"), 50)
	write(t, filepath.Join(dir, "journal", "k1.jsonl"), 60)
	write(t, filepath.Join(dir, "whatsapp", "settings.json"), 70)
	write(t, filepath.Join(dir, "gateway.log"), 80)
	write(t, filepath.Join(dir, "gateway.log.1"), 20)
	write(t, filepath.Join(dir, "settings.json"), 10)
	write(t, filepath.Join(dir, "skills", "x.md"), 5)
	write(t, filepath.Join(dir, "chats", "c1.json"), 999) // told by the chats themselves
	j := &Janitor{Dir: dir, Chats: &fakeChats{}, Settings: &fakeSettings{r: config.Retention{ShotDays: 7}}}
	u := j.Usage()
	want := map[string]int64{AreaHomes: 4000, AreaShots: 3000, AreaWork: 1000, AreaShared: 500, AreaAttachments: 200,
		AreaChats: 100, AreaLogs: 100, AreaWhatsApp: 70, AreaJournal: 60, AreaBackups: 50, AreaOther: 15}
	var total int64
	for k, n := range want {
		if got := area(u, k).Bytes; got != n {
			t.Errorf("%s = %d, want %d", k, got, n)
		}
		total += n
	}
	if u.Total != total || u.Areas[0].Key != AreaHomes || area(u, AreaShots).Count != 3 {
		t.Errorf("usage = %+v", u)
	}
	if u.Free <= 0 {
		t.Errorf("free = %d", u.Free)
	}
	if u.Retention.ShotDays != 7 || u.Defaults.ShotDays != config.DefaultShotDays || u.Last != nil {
		t.Errorf("retention = %+v defaults %+v", u.Retention, u.Defaults)
	}
	// Without chats or settings, the folders alone.
	if u := (&Janitor{Dir: dir}).Usage(); area(u, AreaShots).Bytes != 0 || area(u, AreaHomes).Bytes != 4000 {
		t.Errorf("bare = %+v", u)
	}
}

func TestCleanFollowsTheRetention(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	c := &fakeChats{}
	s := &fakeSettings{}
	im := &fakeImages{freed: "2.4GB"}
	j := &Janitor{Dir: t.TempDir(), Chats: c, Settings: s, Images: im, Now: func() time.Time { return now }}
	res := j.Clean(context.Background())
	if res.Shots != 2 || res.Freed != 2000 || res.Images != "2.4GB" || res.Error != "" {
		t.Errorf("clean = %+v", res)
	}
	if !c.before.Equal(now.Add(-30*24*time.Hour)) || c.max != 2048<<20 {
		t.Errorf("defaults: before %v max %d", c.before, c.max)
	}
	if u := j.Usage(); u.Last == nil || u.Last.Shots != 2 {
		t.Errorf("last = %+v", u.Last)
	}
	// Forever and no cap: nothing by age, nothing by size.
	s.r = config.Retention{ShotDays: -1, ShotsMB: -1}
	im.err = errors.New("no daemon")
	res = j.Clean(context.Background())
	if !c.before.IsZero() || c.max != 0 || res.Error != "no daemon" {
		t.Errorf("forever: before %v max %d res %+v", c.before, c.max, res)
	}
	// Nothing to sweep with: an empty result.
	if res := (&Janitor{}).Clean(context.Background()); res.Shots != 0 || res.At.IsZero() {
		t.Errorf("bare clean = %+v", res)
	}
	// Chats without settings use the defaults.
	c2 := &fakeChats{}
	(&Janitor{Chats: c2, Now: func() time.Time { return now }}).Clean(context.Background())
	if c2.max != 2048<<20 {
		t.Errorf("no settings max = %d", c2.max)
	}
}

func TestRunSweepsAndLogs(t *testing.T) {
	c := &fakeChats{}
	j := &Janitor{Dir: t.TempDir(), Chats: c, Images: &fakeImages{freed: "1GB", err: errors.New("half")}}
	ctx, cancel := context.WithCancel(context.Background())
	var lines []string
	done := make(chan struct{})
	go func() {
		j.Run(ctx, time.Millisecond, time.Hour, func(format string, args ...any) {
			lines = append(lines, format)
			if len(lines) == 3 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not end")
	}
	if !strings.Contains(strings.Join(lines, "|"), "screenshots") || !strings.Contains(strings.Join(lines, "|"), "images") {
		t.Errorf("lines = %v", lines)
	}
	// Without a log it still sweeps.
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() {
		for c.swept.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		cancel2()
	}()
	j.Run(ctx2, time.Millisecond, time.Millisecond, nil)
}

func TestBytes(t *testing.T) {
	for n, want := range map[int64]string{12: "12 B", 1536: "1.5 KB", 320 << 20: "320 MB", 3 << 30: "3.0 GB", 25 << 30: "25 GB", 5 << 40: "5.0 TB"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDirSizeStopsAtItsCap(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d"} {
		write(t, filepath.Join(dir, n), 10)
	}
	old := sizeCap
	sizeCap = 2
	t.Cleanup(func() { sizeCap = old })
	if got := dirSize(dir); got >= 40 {
		t.Errorf("capped size = %d", got)
	}
	if freeBytes(filepath.Join(dir, "missing")) != 0 {
		t.Error("free of a missing folder")
	}
}

func TestSetRetention(t *testing.T) {
	s := &fakeSettings{}
	if err := (&Janitor{Settings: s}).SetRetention(config.Retention{ShotDays: 3}); err != nil || s.r.ShotDays != 3 {
		t.Errorf("set = %v %+v", err, s.r)
	}
	if err := (&Janitor{}).SetRetention(config.Retention{}); err == nil {
		t.Error("no settings to keep it in")
	}
}
