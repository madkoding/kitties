package keep

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupsAreSealedAndComeBack(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load("k1"); !errors.Is(err, ErrNone) {
		t.Errorf("Load before = %v", err)
	}
	if _, ok := s.Info("k1"); ok {
		t.Error("a backup that is not there has info")
	}
	secret := []byte("cookie: session=hunter2")
	if err := s.Save("k1", secret); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "backups", "k1.bin"))
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Error("the backup is on disk in the clear")
	}
	if st, _ := os.Stat(filepath.Join(dir, "backups", "k1.bin")); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	got, info, err := s.Load("k1")
	if err != nil || !bytes.Equal(got, secret) || time.Since(info.Saved) > time.Minute || info.Bytes != len(raw) {
		t.Errorf("Load = %q %+v %v", got, info, err)
	}
	// Saving again replaces it; a backup is of one kitty only.
	_ = s.Save("k1", []byte("newer"))
	if got, _, _ := s.Load("k1"); string(got) != "newer" {
		t.Errorf("replaced = %q", got)
	}
	if err := os.Rename(filepath.Join(dir, "backups", "k1.bin"), filepath.Join(dir, "backups", "k2.bin")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load("k2"); err == nil || !strings.Contains(err.Error(), "cannot be opened") {
		t.Errorf("a backup moved to another kitty opened: %v", err)
	}
	// Forgotten with its kitty, and again without error.
	_ = s.Save("k3", []byte("x"))
	if err := s.Forget("k3"); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget("k3"); err != nil {
		t.Errorf("forgetting twice = %v", err)
	}
	if _, ok := s.Info("k3"); ok {
		t.Error("still there")
	}
}

func TestChecks(t *testing.T) {
	s, _ := Open(t.TempDir())
	for _, id := range []string{"", "../x", "a/b", ".hidden"} {
		if err := s.Save(id, []byte("x")); err == nil {
			t.Errorf("Save(%q) was accepted", id)
		}
		if _, _, err := s.Load(id); err == nil {
			t.Errorf("Load(%q) was accepted", id)
		}
		if _, ok := s.Info(id); ok {
			t.Errorf("Info(%q)", id)
		}
		if err := s.Forget(id); err == nil {
			t.Errorf("Forget(%q)", id)
		}
	}
	if err := s.Save("k", nil); err == nil {
		t.Error("an empty backup was kept")
	}
	if err := s.Save("k", make([]byte, Max+1)); err == nil {
		t.Error("a huge backup was kept")
	}
	t.Setenv("KITTIES_VAULT_KEY", "nope")
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("a bad key was accepted")
	}
}

func TestBoxTooShort(t *testing.T) {
	s, _ := Open(t.TempDir())
	_ = os.MkdirAll(s.dir, 0o700)
	_ = os.WriteFile(filepath.Join(s.dir, "k.bin"), []byte("tiny"), 0o600)
	if _, _, err := s.Load("k"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("a truncated backup = %v", err)
	}
}

func TestSaveFailsWhereTheFolderCannotBeMade(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := os.WriteFile(s.dir, []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("k", []byte("x")); err == nil {
		t.Error("saved where there is no folder")
	}
	// A path that cannot be read is an error, not a missing backup.
	if err := os.Remove(s.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "k.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "k.bin", "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load("k"); err == nil || errors.Is(err, ErrNone) {
		t.Errorf("a directory in the way = %v", err)
	}
	if err := s.Forget("k"); err == nil {
		t.Error("forgot a directory")
	}
}
