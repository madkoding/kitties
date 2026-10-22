package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsAndNames(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "skills"))
	list, err := l.List()
	if err != nil || len(list) < 3 {
		t.Fatalf("List = %v %v", list, err)
	}
	for _, s := range list {
		if !s.Builtin || s.Title == "" || s.Summary == "" || s.Body != "" {
			t.Errorf("builtin = %+v", s)
		}
	}
	got, err := l.Get("browse-a-site")
	if err != nil || !strings.Contains(got.Body, "@tool open") || !got.Builtin {
		t.Errorf("Get builtin = %+v %v", got, err)
	}
	if Name("  Hello, World! ") != "hello-world" || len(Name(strings.Repeat("a", 100))) != 60 || Name("***") != "" {
		t.Error("Name")
	}
	if idx := l.Index(); !strings.Contains(idx, "- report-findings:") {
		t.Errorf("Index = %s", idx)
	}
	if err := l.Delete("browse-a-site"); err == nil || !strings.Contains(err.Error(), "built in") {
		t.Errorf("builtins stay: %v", err)
	}
	if _, err := l.Get("nope"); err == nil {
		t.Error("unknown")
	}
	if err := l.Delete("nope"); err == nil {
		t.Error("delete unknown")
	}
}

func TestSaveProposeAcceptDelete(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "skills"))
	if _, err := l.Save("", "# x"); err == nil {
		t.Error("a name is needed")
	}
	if _, err := l.Save("x", "  "); err == nil {
		t.Error("an empty body is refused")
	}
	if _, err := l.Save("x", strings.Repeat("a", maxBody+1)); err == nil {
		t.Error("a huge body is refused")
	}
	s, err := l.Save("Deploy To Prod", "# Deploy to prod\n\nHow to ship.\n\n1. Tag.")
	if err != nil || s.Name != "deploy-to-prod" || s.Title != "Deploy to prod" || s.Summary != "How to ship." || s.Builtin || s.UpdatedAt.IsZero() {
		t.Fatalf("Save = %+v %v", s, err)
	}
	// Own skills shadow built-ins with the same name.
	if _, err := l.Save("report-findings", "# Mine\n\nMy own way."); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Get("report-findings"); got.Builtin || got.Summary != "My own way." {
		t.Errorf("shadowed = %+v", got)
	}
	p, err := l.Propose("Learned Thing", "# Learned\n\nA new trick.")
	if err != nil || !p.Proposed || p.Name != "learned-thing" {
		t.Fatalf("Propose = %+v %v", p, err)
	}
	list, _ := l.List()
	last := list[len(list)-1]
	if !last.Proposed || last.Name != "learned-thing" {
		t.Errorf("proposals come last: %+v", last)
	}
	if got, err := l.Get("learned-thing"); err != nil || !got.Proposed || !strings.Contains(got.Body, "trick") {
		t.Errorf("Get proposal = %+v %v", got, err)
	}
	if idx := l.Index(); strings.Contains(idx, "learned-thing") {
		t.Error("a proposal is not in the index")
	}
	if found, _ := l.Search("new trick", 5); len(found) != 0 {
		t.Error("search skips proposals")
	}
	acc, err := l.Accept("learned-thing")
	if err != nil || acc.Proposed || acc.Builtin {
		t.Errorf("Accept = %+v %v", acc, err)
	}
	if _, err := os.Stat(l.proposalPath("learned-thing")); !os.IsNotExist(err) {
		t.Error("the proposal file is gone")
	}
	if _, err := l.Accept("learned-thing"); err == nil {
		t.Error("accept twice")
	}
	found, _ := l.Search("trick", 5)
	if len(found) != 1 || found[0].Name != "learned-thing" {
		t.Errorf("Search = %+v", found)
	}
	if found, _ := l.Search("", 2); len(found) != 2 {
		t.Error("limit")
	}
	if _, err := l.Propose("", "x"); err == nil {
		t.Error("propose needs a name")
	}
	if _, err := l.Propose("again", "# Again\n\nTwice."); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete("again"); err != nil {
		t.Error("a proposal can be deleted")
	}
	if err := l.Delete("deploy-to-prod"); err != nil {
		t.Error("an own skill can be deleted")
	}
	if _, err := l.Get("deploy-to-prod"); err == nil {
		t.Error("deleted")
	}
	// A folder that cannot be read.
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	bad := New(file)
	if _, err := bad.List(); err == nil {
		t.Error("unreadable library")
	}
	if _, err := bad.Save("x", "# x\n\ny"); err == nil {
		t.Error("unwritable library")
	}
	if bad.Index() != "" {
		t.Error("an unreadable library has no index")
	}
	if _, err := bad.Search("x", 1); err == nil {
		t.Error("search reports the failure")
	}
	if bad.Dir() != file {
		t.Error("Dir")
	}
	_ = os.WriteFile(filepath.Join(l.dir, "notes.txt"), []byte("ignored"), 0o600)
	_ = os.Mkdir(filepath.Join(l.dir, "sub.md"), 0o700)
	if list, err := l.List(); err != nil || len(list) == 0 {
		t.Error("stray files are ignored")
	}
	if parsed := parse("n", "no heading here"); parsed.Title != "n" || parsed.Summary != "no heading here" {
		t.Errorf("parse = %+v", parsed)
	}
	if parsed := parse("n", "# T\n\n"+strings.Repeat("x", 300)); !strings.HasSuffix(parsed.Summary, "…") {
		t.Error("long summaries are clipped")
	}
}
