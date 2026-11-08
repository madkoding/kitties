package chat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A chat with two turns of screenshots: an old one and a recent one, plus an attached file.
func tidyChat(t *testing.T) (*Service, string) {
	t.Helper()
	f, _, _, _ := fleet(t)
	s := service(t, f)
	sess, err := s.Create("work", []string{"ana"})
	if err != nil {
		t.Fatal(err)
	}
	id := sess.ID
	s.mu.Lock()
	sess = s.sessions[id]
	sess.Turns = []Turn{
		{From: "ana", Kitty: "a1", Steps: []Step{{Action: "click", Shot: "0-0.png"}, {Action: "click", Shot: "0-1.png"}}},
		{From: "ana", Kitty: "a1", Steps: []Step{{Action: "click", Shot: "1-0.png"}}},
	}
	_ = s.save(sess)
	s.mu.Unlock()
	dir := filepath.Join(s.dir, id)
	old := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"0-0.png", "0-1.png", "1-0.png"} {
		if err := os.WriteFile(filepath.Join(dir, n), make([]byte, 1000), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"0-0.png", "0-1.png"} {
		_ = os.Chtimes(filepath.Join(dir, n), old, old)
	}
	_ = os.MkdirAll(filesDir(s.dir, id), 0o700)
	_ = os.WriteFile(filepath.Join(filesDir(s.dir, id), "abcdefabcdef"), make([]byte, 500), 0o600)
	return s, id
}

func TestDiskUsageByKind(t *testing.T) {
	s, _ := tidyChat(t)
	u := s.DiskUsage()
	if u.Shots != 3000 || u.ShotCount != 3 || u.Files != 500 || u.History == 0 {
		t.Errorf("usage = %+v", u)
	}
	if u := (&Service{dir: filepath.Join(t.TempDir(), "none")}).DiskUsage(); u != (DiskUsage{}) {
		t.Errorf("no folder = %+v", u)
	}
}

func TestSweepShotsByAge(t *testing.T) {
	s, id := tidyChat(t)
	n, freed := s.SweepShots(time.Now().Add(-30*24*time.Hour), 0)
	if n != 2 || freed != 2000 {
		t.Fatalf("swept %d files, %d bytes", n, freed)
	}
	if _, err := s.Shot(id, "0-0.png"); err == nil {
		t.Error("an old screenshot is still there")
	}
	if _, err := s.Shot(id, "1-0.png"); err != nil {
		t.Errorf("a recent one went: %v", err)
	}
	// The steps stay, without the picture, also after a restart.
	again, err := New(s.fleet, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := again.Get(id)
	if len(got.Turns[0].Steps) != 2 || got.Turns[0].Steps[0].Shot != "" || got.Turns[1].Steps[0].Shot != "1-0.png" {
		t.Errorf("steps = %+v", got.Turns)
	}
	if u := s.DiskUsage(); u.Files != 500 {
		t.Errorf("the attached file must stay: %+v", u)
	}
	// Nothing left that old.
	if n, _ := s.SweepShots(time.Now().Add(-30*24*time.Hour), 0); n != 0 {
		t.Errorf("second sweep = %d", n)
	}
}

func TestSweepShotsByCapKeepsTheWorkingTask(t *testing.T) {
	s, id := tidyChat(t)
	s.mu.Lock()
	s.sessions[id].Turns[0].Working = true
	s.mu.Unlock()
	// A cap of 1000 bytes: the oldest go first, but the turn at work keeps its pictures.
	n, _ := s.SweepShots(time.Time{}, 1000)
	if n != 1 {
		t.Errorf("swept %d", n)
	}
	if _, err := s.Shot(id, "0-0.png"); err != nil {
		t.Error("a working task's screenshot went")
	}
	if _, err := s.Shot(id, "1-0.png"); err == nil {
		t.Error("the finished one should go to meet the cap")
	}
	// No limits at all: nothing goes.
	s.mu.Lock()
	s.sessions[id].Turns[0].Working = false
	s.mu.Unlock()
	if n, _ := s.SweepShots(time.Time{}, 0); n != 0 {
		t.Errorf("no limits swept %d", n)
	}
	// A folder of a chat that is gone is swept too, with nothing to save.
	orphan := filepath.Join(s.dir, "gone")
	_ = os.MkdirAll(orphan, 0o700)
	_ = os.WriteFile(filepath.Join(orphan, "0-0.png"), []byte("x"), 0o600)
	if n, _ := s.SweepShots(time.Now().Add(time.Hour), 0); n != 3 {
		t.Errorf("everything older than now: %d", n)
	}
}

func TestSweepShotsOfAnEmptyFolder(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	if n, b := s.SweepShots(time.Now(), 1); n != 0 || b != 0 {
		t.Errorf("empty = %d %d", n, b)
	}
}
