package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s := Open(filepath.Join(t.TempDir(), "memory"))
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { now = now.Add(time.Minute); return now }
	n := 0
	s.id = func() string { n++; return "n" + string(rune('0'+n%10)) + string(rune('a'+n/10)) }
	return s
}

func TestScopesAndSlugs(t *testing.T) {
	for _, ok := range []string{"user", "work", "project:launch", "project:q4 report", "project:a-b_c"} {
		if !ValidScope(ok) {
			t.Errorf("%q is a scope", ok)
		}
	}
	for _, bad := range []string{"", "other", "project:", "project:UPPER", "project:" + strings.Repeat("x", 41), "project:-x"} {
		if ValidScope(bad) {
			t.Errorf("%q is not a scope", bad)
		}
	}
	if Slug(" Launch Plan! 2026 ") != "launch-plan-2026" || Slug("---") != "" || len(Slug(strings.Repeat("ab", 40))) > 40 || Slug("ÑÚ x") != "x" {
		t.Errorf("Slug = %q %q", Slug(" Launch Plan! 2026 "), Slug("ÑÚ x"))
	}
}

func TestRememberReinforcesAndEvicts(t *testing.T) {
	s := open(t)
	a, err := s.Remember("k1", "", "  Prefers   short answers ", "kitty", false)
	if err != nil || a.Scope != ScopeUser || a.Text != "Prefers short answers" || a.Strength != 1 || a.Source != "kitty" || a.Pinned {
		t.Fatalf("remember = %+v %v", a, err)
	}
	// The same thing again, in other letters, strengthens the one note and keeps pins.
	b, _ := s.Remember("k1", ScopeUser, "prefers short ANSWERS", "feedback", true)
	if b.ID != a.ID || b.Strength != 2 || !b.Pinned {
		t.Errorf("reinforce = %+v", b)
	}
	if len(s.Notes("k1", "")) != 1 {
		t.Error("one note")
	}
	// The same text in another scope is another note.
	_, _ = s.Remember("k1", ScopeWork, "Prefers short answers", "kitty", false)
	_, _ = s.Remember("k1", "project:launch", "Ship on Friday", "kitty", false)
	if len(s.Notes("k1", "")) != 3 || len(s.Notes("k1", ScopeWork)) != 1 {
		t.Error("scopes")
	}
	for _, bad := range []struct{ scope, text string }{{"user", "   "}, {"nope", "x"}} {
		if _, err := s.Remember("k1", bad.scope, bad.text, "kitty", false); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
	// Past the limit the weakest unpinned note leaves; pinned ones never do.
	s2 := open(t)
	pin, _ := s2.Remember("k", ScopeUser, "pinned", "person", true)
	for i := 0; i < MaxNotes+5; i++ {
		_, _ = s2.Remember("k", ScopeWork, "lesson "+strings.Repeat("x", i), "kitty", false)
	}
	notes := s2.Notes("k", "")
	if len(notes) != MaxNotes {
		t.Errorf("kept %d", len(notes))
	}
	found := false
	for _, n := range notes {
		found = found || n.ID == pin.ID
	}
	if !found {
		t.Error("a pinned note stays")
	}
	// A memory that is all pinned cannot evict: it just grows past the limit.
	s3 := open(t)
	for i := 0; i < MaxNotes+2; i++ {
		_, _ = s3.Remember("k", ScopeUser, "p"+strings.Repeat("y", i), "person", true)
	}
	if len(s3.Notes("k", "")) != MaxNotes+2 {
		t.Error("pinned notes are never evicted")
	}
	// Strongest first, then newest.
	s4 := open(t)
	_, _ = s4.Remember("k", ScopeUser, "old", "kitty", false)
	_, _ = s4.Remember("k", ScopeUser, "new", "kitty", false)
	_, _ = s4.Remember("k", ScopeUser, "old", "kitty", false)
	if got := s4.Notes("k", ""); got[0].Text != "old" || got[1].Text != "new" {
		t.Errorf("order = %+v", got)
	}
	// Forget.
	if err := s.Forget("k1", a.ID); err != nil || len(s.Notes("k1", "")) != 2 {
		t.Error("Forget")
	}
	if err := s.Forget("k1", "zz"); err == nil {
		t.Error("Forget unknown")
	}
}

func TestFeedbackAdaptsDemand(t *testing.T) {
	s := open(t)
	if p := s.Profile("k"); p.Demand != 3 || !p.Auto {
		t.Fatalf("default profile = %+v", p)
	}
	rate := func(r string) (Profile, bool) {
		return s.Record("k", Feedback{Rating: r, Source: "turn", Excerpt: strings.Repeat("e", 400)})
	}
	// Three downs and a few ups: nothing yet. The fourth down in the window raises the level.
	for _, r := range []string{"down", "up", "down", "up", "down"} {
		if _, changed := rate(r); changed {
			t.Fatal("too early")
		}
	}
	p, changed := rate("down")
	if !changed || p.Demand != 4 || len(p.Recent) != 0 || p.Down != 4 || p.Up != 2 || p.Pending != 6 {
		t.Errorf("raised = %+v %v", p, changed)
	}
	// Ten ups in a row with no down relax it again.
	var last Profile
	for i := 0; i < 10; i++ {
		last, changed = rate("up")
	}
	if !changed || last.Demand != 3 {
		t.Errorf("relaxed = %+v %v", last, changed)
	}
	// The ends of the scale hold.
	for i := 0; i < 40; i++ {
		_, _ = rate("down")
	}
	if s.Demand("k") != 5 {
		t.Errorf("max = %d", s.Demand("k"))
	}
	for i := 0; i < 100; i++ {
		_, _ = rate("up")
	}
	if s.Demand("k") != 1 {
		t.Errorf("min = %d", s.Demand("k"))
	}
	// By hand the level stays where it is put, whatever the feedback.
	if _, err := s.SetProfile("k", 4, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		_, _ = rate("up")
	}
	if p := s.Profile("k"); p.Demand != 4 || p.Auto {
		t.Errorf("manual = %+v", p)
	}
	if _, err := s.SetProfile("k", 0, true); err == nil {
		t.Error("level out of range")
	}
	if _, err := s.SetProfile("k", 6, true); err == nil {
		t.Error("level out of range")
	}
	// Verdicts are kept (clipped, newest last, bounded).
	fb := s.Recent("k", 2)
	if len(fb) != 2 || len(fb[0].Excerpt) > 304 || fb[0].At.IsZero() {
		t.Errorf("recent = %+v", fb)
	}
	if len(s.Recent("k", 0)) != MaxFeedback {
		t.Errorf("bounded: %d", len(s.Recent("k", 0)))
	}
}

func TestDistilledPersistenceAndPrompt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := Open(dir)
	_, _ = s.Remember("k", ScopeUser, "Always answer in Spanish", "person", true)
	_, _ = s.Remember("k", ScopeUser, "likes bullets", "kitty", false)
	_, _ = s.Remember("k", ScopeWork, "invoices come on the 1st", "kitty", false)
	s.Record("k", Feedback{Rating: "down"})
	// A distillation replaces what is not pinned, never duplicates a pinned note, ignores nonsense.
	s.Distilled("k", []Note{{Scope: ScopeUser, Text: "Keep answers short"}, {Scope: ScopeUser, Text: "always answer in spanish"}, {Scope: "bogus", Text: "x"}, {Scope: ScopeWork, Text: "  "}, {Scope: "project:launch", Text: "Ship Friday", Strength: 3}})
	notes := s.Notes("k", "")
	if len(notes) != 3 || s.Pending("k") != 0 {
		t.Fatalf("distilled = %+v pending %d", notes, s.Pending("k"))
	}
	texts := map[string]string{}
	for _, n := range notes {
		texts[n.Text] = n.Source
	}
	if texts["Always answer in Spanish"] != "person" || texts["Keep answers short"] != "distilled" || texts["Ship Friday"] != "distilled" {
		t.Errorf("texts = %v", texts)
	}
	// It survives a restart: notes, profile and verdicts.
	again := Open(dir)
	if len(again.Notes("k", "")) != 3 || again.Profile("k").Down != 1 || len(again.Recent("k", 0)) != 1 {
		t.Error("reopen")
	}
	// The prompt says how demanding the person is, what was learned and the project's notes.
	_, _ = s.SetProfile("k", 4, true)
	p := s.Prompt("k", "Pat", "launch")
	for _, want := range []string{"How demanding Pat is with you: 4 of 5", "double-check", "- Always answer in Spanish", "- Keep answers short", "What you know about this project:\n- Ship Friday"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(s.Prompt("k", "Pat", ""), "this project") || !strings.Contains(s.Prompt("nobody", "", ""), "How demanding the person is with you: 3 of 5") {
		t.Error("project notes only with a project; a new kitty gets the middle level")
	}
	_, _ = s.Remember("k", ScopeWork, "invoices come on the 1st", "kitty", false)
	if !strings.Contains(s.Prompt("k", "Pat", ""), "Lessons about the work:\n- invoices come on the 1st") {
		t.Error("work lessons")
	}
	if LevelText(0) != levels[1] || LevelText(9) != levels[5] {
		t.Error("LevelText clamps")
	}
	// A damaged file is a fresh memory; a profile out of range is the middle.
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{nope"), 0o600)
	if Open(dir).Profile("bad").Demand != 3 {
		t.Error("damaged file")
	}
	_ = os.WriteFile(filepath.Join(dir, "odd.json"), []byte(`{"profile":{"demand":9}}`), 0o600)
	if Open(dir).Profile("odd").Demand != 3 {
		t.Error("odd level")
	}
	// Forgetting a kitty removes its file; names are made safe for the file system.
	if err := s.ForgetKitty("k"); err != nil || len(s.Notes("k", "")) != 0 {
		t.Error("ForgetKitty")
	}
	if err := s.ForgetKitty("k"); err != nil {
		t.Error("twice is fine")
	}
	if got := s.path("../../etc/x"); filepath.Dir(got) != s.dir {
		t.Errorf("path = %s", got)
	}
	if randomID() == randomID() {
		t.Error("ids are random")
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		_ = os.Mkdir(ro, 0o500)
		bad := Open(filepath.Join(ro, "sub"))
		if _, err := bad.Remember("k", ScopeUser, "x", "kitty", false); err != nil {
			t.Error("an unwritable memory still works in memory")
		}
	}
}
