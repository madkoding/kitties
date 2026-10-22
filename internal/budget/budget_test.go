package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCountsPerDayAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "budget.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.AddTokens("a", 100)
	s.AddTokens("a", 50)
	s.AddTokens("a", 0)
	s.AddActions("a", 2)
	s.AddActions("a", -1)
	s.AddTokens("b", 7)
	if got := s.Today("a"); got.Tokens != 150 || got.Actions != 2 {
		t.Fatalf("a today = %+v", got)
	}
	if got := s.Today("nobody"); got != (Day{}) {
		t.Fatalf("unknown kitty = %+v", got)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again.now = s.now
	if got := again.Today("a"); got.Tokens != 150 {
		t.Fatalf("after reopening: %+v", got)
	}

	clock = clock.Add(24 * time.Hour)
	if got := again.Today("a"); got != (Day{}) {
		t.Fatalf("a new day starts empty, got %+v", got)
	}
}

func TestTellOncePerDay(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "b.json"))
	clock := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	if !s.Tell("a") || s.Tell("a") {
		t.Fatal("the warning must be given once a day")
	}
	clock = clock.Add(24 * time.Hour)
	if !s.Tell("a") {
		t.Fatal("and again the next day")
	}
}

func TestOldDaysAreForgotten(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "b.json"))
	clock := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for i := 0; i < keep+5; i++ {
		s.AddTokens("a", 1)
		clock = clock.Add(24 * time.Hour)
	}
	if n := len(s.d["a"]); n != keep {
		t.Fatalf("%d days kept, want %d", n, keep)
	}
}

func TestForgetAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.json")
	s, _ := Open(path)
	if err := s.Forget("a"); err != nil {
		t.Fatal(err)
	}
	s.AddActions("a", 1)
	if err := s.Forget("a"); err != nil || s.Today("a").Actions != 0 {
		t.Fatalf("forget: %v", err)
	}
	os.WriteFile(path, []byte("{nope"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("a corrupt file must be reported")
	}
	os.WriteFile(path, []byte("null"), 0o600)
	if s, err := Open(path); err != nil || s.d == nil {
		t.Fatalf("null file: %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("a directory is not a store")
	}
	// A path that cannot be written is reported by save, and the count stays.
	blocked, _ := Open(filepath.Join(dir, "file", "x.json"))
	os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600)
	blocked.AddTokens("a", 3)
	if blocked.Today("a").Tokens != 3 {
		t.Fatal("the count must survive a failed save")
	}
}
