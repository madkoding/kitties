package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "artifacts"))
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Error("empty")
	}
	a, err := s.Put("report.md", []byte("# hi"), "mimi")
	if err != nil || a.Name != "report.md" || a.Type != "text/markdown" || a.Size != 4 || a.By != "mimi" || a.UpdatedAt.IsZero() {
		t.Fatalf("Put = %+v %v", a, err)
	}
	if _, err := s.Put("scene.excalidraw", []byte("{}"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("../x", nil, ""); err == nil {
		t.Error("no folders")
	}
	if _, err := s.Put(".hidden", nil, ""); err == nil {
		t.Error("no hidden files")
	}
	if _, err := s.Put("big.bin", make([]byte, maxFile+1), ""); err == nil {
		t.Error("too big")
	}
	data, err := s.Get("report.md")
	if err != nil || string(data) != "# hi" {
		t.Error("Get")
	}
	if _, err := s.Get("nope.md"); err == nil {
		t.Error("Get unknown")
	}
	if _, err := s.Get("../etc"); err == nil {
		t.Error("Get bad name")
	}
	list, _ := s.List()
	if len(list) != 2 || list[1].Name != "report.md" && list[0].Name != "report.md" {
		t.Errorf("List = %+v", list)
	}
	if idx := s.Index(1); !strings.Contains(idx, "and 1 more") || !strings.Contains(idx, "bytes") {
		t.Errorf("Index = %s", idx)
	}
	if idx := s.Index(10); !strings.Contains(idx, "by mimi") {
		t.Errorf("Index = %s", idx)
	}
	if err := s.Delete("report.md"); err != nil {
		t.Error(err)
	}
	if err := s.Delete("report.md"); err == nil {
		t.Error("delete twice")
	}
	if err := s.Delete("../x"); err == nil {
		t.Error("delete bad name")
	}
	if Type("a.png") != "image/png" || Type("a") != "text/plain" || Type("a.unknownext") != "application/octet-stream" || Type("a.txt") != "text/plain" {
		t.Error("Type")
	}
	if !ValidName("my report v2.md") || ValidName("a/b") || ValidName("x") == false || ValidName("") {
		t.Error("ValidName")
	}
	// Owners survive and stray entries are ignored.
	_ = os.WriteFile(s.ownersPath(), []byte("scene.excalidraw\ttom\ngarbage\n"), 0o600)
	list, _ = s.List()
	if list[0].By != "tom" {
		t.Errorf("owners = %+v", list)
	}
	_ = os.Mkdir(filepath.Join(s.dir, "folder"), 0o700)
	if list, _ := s.List(); len(list) != 1 {
		t.Error("folders are not artifacts")
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	bad := New(file)
	if _, err := bad.List(); err == nil {
		t.Error("unreadable store")
	}
	if _, err := bad.Put("x.txt", []byte("x"), ""); err == nil {
		t.Error("unwritable store")
	}
	if bad.Index(5) != "" || bad.Dir() != file {
		t.Error("Index/Dir on a broken store")
	}
	full := New(filepath.Join(t.TempDir(), "full"))
	for i := 0; i < maxFiles; i++ {
		if _, err := full.Put("f"+strings.Repeat("0", 3-len(itoa(i)))+itoa(i)+".txt", []byte("x"), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := full.Put("one-more.txt", []byte("x"), ""); err == nil {
		t.Error("the store is bounded")
	}
	if _, err := full.Put("f000.txt", []byte("y"), ""); err != nil {
		t.Error("an existing file can still be replaced")
	}
}

func itoa(n int) string {
	return strings.TrimLeft(strings.Repeat("0", 0)+string(rune('0'+n/100))+string(rune('0'+(n/10)%10))+string(rune('0'+n%10)), "")
}
