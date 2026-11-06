package shared

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, path, text string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestValidPath(t *testing.T) {
	for p, want := range map[string]bool{
		"report.pdf": true, "drafts/notes.md": true, "a b/c (1).png": true, "a/b/c/d.txt": true,
		"": false, "/etc/passwd": false, "../x": false, "a/../x": false, ".hidden": false, "a/.git/config": false,
		"a/b/c/d/e.txt": false, "a\\b": false, "a//b": false, "x\ny": false,
	} {
		if ValidPath(p) != want {
			t.Errorf("ValidPath(%q) = %v", p, !want)
		}
	}
}

func TestListOpenDelete(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	t0 := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	put(t, filepath.Join(root, "mimi", "report.md"), "# hi", t0)
	put(t, filepath.Join(root, "mimi", "pics", "cat.png"), "png", t0.Add(time.Hour))
	put(t, filepath.Join(root, "mimi", ".hidden"), "x", t0)
	put(t, filepath.Join(root, "mimi", ".git", "config"), "x", t0)
	put(t, filepath.Join(root, "mimi", "a", "b", "c", "d", "deep.txt"), "x", t0)
	put(t, filepath.Join(root, "toby", "a.csv"), "1,2", t0)
	put(t, filepath.Join(root, "outside.txt"), "secret", t0)
	if err := os.Symlink(filepath.Join(root, "outside.txt"), filepath.Join(root, "mimi", "link.txt")); err != nil {
		t.Fatal(err)
	}
	list := s.List([]string{"mimi", "toby", "Bad Name", "nobody"})
	var got []string
	for _, f := range list {
		got = append(got, f.Kitty+":"+f.Path+":"+f.Type)
	}
	want := "mimi:pics/cat.png:image/png mimi:report.md:text/markdown toby:a.csv:text/csv; charset=utf-8"
	if strings.Join(got, " ") != want {
		t.Errorf("List = %v\nwant  %s", got, want)
	}
	f, info, err := s.Open("mimi", "pics/cat.png")
	if err != nil || info.Size() != 3 {
		t.Fatalf("Open = %v %v", info, err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "png" {
		t.Errorf("read %q", b)
	}
	for _, p := range []string{"link.txt", "../outside.txt", "pics", "nope.txt"} {
		if _, _, err := s.Open("mimi", p); err == nil {
			t.Errorf("Open(%q) reached it", p)
		}
	}
	if _, _, err := s.Open("Bad", "x"); err == nil {
		t.Error("a bad kitty name")
	}
	if _, _, err := s.Open("ghost", "x.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a kitty without a folder: %v", err)
	}
	if err := s.Delete("mimi", "report.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "mimi", "report.md")); !os.IsNotExist(err) {
		t.Error("not deleted")
	}
	for _, p := range []string{"link.txt", "pics", "report.md"} {
		if err := s.Delete("mimi", p); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete(%q) = %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "outside.txt")); err != nil {
		t.Error("a link's target was deleted")
	}
	if err := s.Delete("mimi", "../outside.txt"); err == nil {
		t.Error("a path that climbs out")
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(root, "toby")
		_ = os.Chmod(ro, 0o500)
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := s.Delete("toby", "a.csv"); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("a file the gateway may not delete: %v", err)
		}
	}
}

func TestListCapsTheFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxFiles+5; i++ {
		put(t, filepath.Join(root, "mimi", "f"+strconv.Itoa(i)), "x", time.Now())
	}
	if n := len(New(root).List([]string{"mimi"})); n != maxFiles {
		t.Errorf("listed %d", n)
	}
}
