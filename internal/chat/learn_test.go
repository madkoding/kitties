package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/memory"
	"github.com/madkoding/kitties/internal/provider"
)

// remembering is a service with a memory, an agenda journal and the actions held for approval.
func remembering(t *testing.T) (*Service, *scripted, *scripted, *memory.Store) {
	t.Helper()
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	m := memory.Open(filepath.Join(t.TempDir(), "memory"))
	s.SetMemory(m)
	s.SetUser("Pat")
	return s, sa, sb, m
}

func TestMemoryInThePromptAndActions(t *testing.T) {
	s, sa, _, m := remembering(t)
	j, _ := withAgenda(t, s)
	sess, _ := s.Create("Launch plan", []string{"ana"})
	// The first answer sees the middle level and the instructions to keep notes.
	sa.replies = []string{"@memory remember {\"text\": \"Pat wants short answers\"}\n@memory remember {\"text\": \"Launch is on Friday\", \"scope\": \"project\"}\n@memory remember {\"text\": \"Check dates twice\", \"scope\": \"work\"}", "Noted."}
	turns, err := s.Send(context.Background(), sess.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	system := sa.seen[0][0].Content
	for _, want := range []string{"How demanding Pat is with you: 3 of 5", "@memory remember"} {
		if !strings.Contains(system, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, system)
		}
	}
	if turns[0].Text == "" {
		t.Error("turn")
	}
	ana, _ := s.fleet.Get("ana")
	notes := m.Notes(ana.ID, "")
	if len(notes) != 3 {
		t.Fatalf("notes = %+v", notes)
	}
	scopes := map[string]bool{}
	for _, n := range notes {
		scopes[n.Scope] = true
	}
	if !scopes["user"] || !scopes["work"] || !scopes["project:launch-plan"] {
		t.Errorf("scopes = %v", scopes)
	}
	if kinds(j, "a1") != "learned,learned,learned" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// Next time the kitty reads them back, the project's too, because this thread is that project.
	sa.replies = []string{"Short answer."}
	_, _ = s.Send(context.Background(), sess.ID, "again")
	system = sa.seen[len(sa.seen)-1][0].Content
	for _, want := range []string{"- Pat wants short answers", "Lessons about the work:\n- Check dates twice", "What you know about this project:\n- Launch is on Friday"} {
		if !strings.Contains(system, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, system)
		}
	}
	// Another thread does not get that project's notes.
	other, _ := s.Create("Something else", []string{"ana"})
	sa.replies = []string{"ok"}
	_, _ = s.Send(context.Background(), other.ID, "hi")
	if strings.Contains(sa.seen[len(sa.seen)-1][0].Content, "Launch is on Friday") {
		t.Error("a project's notes are for its own thread")
	}
	// list and forget, by id, in text and native form.
	sa.replies = []string{"@memory list", "listed"}
	_, _ = s.Send(context.Background(), sess.ID, "what do you know?")
	fed := sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content
	if !strings.Contains(fed, "[user] Pat wants short answers (id ") {
		t.Errorf("list = %q", fed)
	}
	id := notes[0].ID
	sa.replies = []string{"@memory forget {\"id\": \"" + id + "\"}\n@memory forget {\"id\": \"zz\"}\n@memory frobnicate", "ok"}
	_, _ = s.Send(context.Background(), sess.ID, "forget")
	fed = sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content
	if !strings.Contains(fed, "forgotten") || !strings.Contains(fed, "no such note") || !strings.Contains(fed, "unknown memory verb") {
		t.Errorf("forget = %q", fed)
	}
	if len(m.Notes(ana.ID, "")) != 2 {
		t.Error("one note forgotten")
	}
	// Plain words after @memory are the text; a note that is empty or in a bad scope is refused.
	sa.replies = []string{"@memory remember Prefers bullets\n@memory remember {\"text\": \"\"}\n@memory remember {\"text\": \"x\", \"scope\": \"bogus\"}", "ok"}
	_, _ = s.Send(context.Background(), sess.ID, "words")
	fed = sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content
	if !strings.Contains(fed, "noted (user") || strings.Count(fed, "failed") != 2 {
		t.Errorf("remember = %q", fed)
	}
	// A thread with no name for a project cannot keep project notes; an empty memory lists so.
	blank, _ := s.Create("???", []string{"ana"})
	sa.replies = []string{"@memory remember {\"text\": \"x\", \"scope\": \"project\"}", "ok"}
	_, _ = s.Send(context.Background(), blank.ID, "go")
	if fed := sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content; !strings.Contains(fed, "no project") {
		t.Errorf("no project = %q", fed)
	}
	bo, _ := s.fleet.Get("bo")
	if out := s.runMemory("", action{name: "list"}, bo); out.text != "(no notes yet)" {
		t.Errorf("empty list = %+v", out)
	}
	// Native: the three tools are offered and their calls are memory actions.
	names := map[string]bool{}
	for _, tool := range s.toolSet(ana, nil) {
		names[tool.Name] = true
	}
	if !names["memory_remember"] || !names["memory_forget"] || !names["memory_list"] {
		t.Errorf("tools = %v", names)
	}
	if a := fromCalls([]provider.ToolCall{{ID: "1", Name: "memory_remember", Args: map[string]any{"text": "x"}}}); a[0].kind != "memory" || a[0].name != "remember" {
		t.Errorf("native = %+v", a[0])
	}
	// Without memory the actions say so and no tools are offered.
	plain := service(t, s.fleet)
	if out := plain.runMemory("", action{name: "list"}, ana); out.err == "" {
		t.Error("no memory here")
	}
	for _, tool := range plain.toolSet(ana, nil) {
		if strings.HasPrefix(tool.Name, "memory_") {
			t.Error("memory tools without a memory")
		}
	}
	if plain.memoryPrompt(ana, nil) != "" {
		t.Error("no memory, no prompt")
	}
	if s.memoryPrompt(ana, nil) == "" || (&Service{}).userName() != "the person" {
		t.Error("prompt without a thread")
	}
}

func TestFeedbackShapesMemoryAndDemand(t *testing.T) {
	s, sa, _, m := remembering(t)
	j, _ := withAgenda(t, s)
	ana, _ := s.fleet.Get("ana")
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"first", "second", "third", "fourth", "fifth", "sixth"}
	for i := 0; i < 6; i++ {
		_, _ = s.Send(context.Background(), sess.ID, "q"+string(rune('a'+i)))
	}
	// Validation.
	for name, fn := range map[string]func() error{
		"bad rating":   func() error { return s.Feedback(sess.ID, 1, "meh", "") },
		"unknown chat": func() error { return s.Feedback("zz", 1, "up", "") },
		"bad index":    func() error { return s.Feedback(sess.ID, 99, "up", "") },
		"negative":     func() error { return s.Feedback(sess.ID, -1, "up", "") },
		"user turn":    func() error { return s.Feedback(sess.ID, 0, "up", "") },
	} {
		if fn() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	// A thumbs up is kept on the turn, counted, and learned from only once.
	if err := s.Feedback(sess.ID, 1, "up", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Feedback(sess.ID, 1, "up", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(sess.ID)
	if got.Turns[1].Feedback != "up" || m.Profile(ana.ID).Up != 1 {
		t.Errorf("up = %+v profile %+v", got.Turns[1], m.Profile(ana.ID))
	}
	// Taking the verdict back keeps the turn clean and learns nothing.
	if err := s.Feedback(sess.ID, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(sess.ID); got.Turns[1].Feedback != "" || m.Profile(ana.ID).Up != 1 {
		t.Error("cleared")
	}
	// A thumbs down with a word becomes a correction, in the notes, at once; and it carries what was asked.
	if err := s.Feedback(sess.ID, 3, "down", "too long, keep it to two lines"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	notes := m.Notes(ana.ID, "user")
	if len(notes) != 1 || notes[0].Text != "Correction: too long, keep it to two lines" || notes[0].Source != "feedback" {
		t.Errorf("notes = %+v", notes)
	}
	if fb := m.Recent(ana.ID, 1)[0]; fb.Request != "qb" || fb.Excerpt != "second" || fb.Rating != "down" || fb.Source != "turn" {
		t.Errorf("verdict = %+v", fb)
	}
	// A liked turn with a word is a note too.
	_ = s.Feedback(sess.ID, 5, "up", "loved the table")
	s.Wait()
	if got := m.Notes(ana.ID, "user"); len(got) < 2 {
		t.Errorf("liked = %+v", got)
	}
	// Enough downs in the last ten raise the level, and the journal says so.
	sa.replies = []string{"[]"} // what the automatic distillation answers
	for _, i := range []int{7, 9, 11} {
		_ = s.Feedback(sess.ID, i, "down", "")
	}
	s.Wait()
	if m.Demand(ana.ID) != 4 {
		t.Errorf("demand = %d, want 4", m.Demand(ana.ID))
	}
	found := false
	for _, e := range j.Recent("a1", 0) {
		found = found || strings.Contains(e.Text, "looks more demanding: level of demand now 4 of 5")
	}
	if !found {
		t.Errorf("the journal says it: %s", kinds(j, "a1"))
	}
}

func TestDistillation(t *testing.T) {
	s, sa, _, m := remembering(t)
	j, _ := withAgenda(t, s)
	ana, _ := s.fleet.Get("ana")
	_, _ = m.Remember(ana.ID, memory.ScopeUser, "Pat wrote this one", "person", true)
	_, _ = m.Remember(ana.ID, memory.ScopeUser, "Correction: too long", "feedback", false)
	m.Record(ana.ID, memory.Feedback{Rating: "down", Note: "too long", Request: "summarise", Excerpt: "a very long summary"})
	// The brain is asked with the notes and the verdicts, and its list replaces what is not pinned.
	sa.replies = []string{"Here you go:\n[{\"scope\": \"user\", \"text\": \"Keep answers to two lines\"}, {\"scope\": \"WORK\", \"text\": \"Summaries first\"}, {\"text\": \"No scope means user\"}]\nDone."}
	notes, err := s.Distill(context.Background(), "ana")
	if err != nil {
		t.Fatal(err)
	}
	asked := sa.seen[0][1].Content
	if !strings.Contains(asked, "[user] Correction: too long") || !strings.Contains(asked, "- down | asked: summarise | the kitty did: a very long summary | the person said: too long") {
		t.Errorf("the brain was asked %q", asked)
	}
	texts := []string{}
	for _, n := range notes {
		texts = append(texts, n.Scope+":"+n.Text)
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"user:Pat wrote this one", "user:Keep answers to two lines", "work:Summaries first", "user:No scope means user"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "Correction: too long") || m.Pending(ana.ID) != 0 || !strings.Contains(kinds(j, "a1"), "learned") {
		t.Errorf("replaced: %s pending %d journal %s", joined, m.Pending(ana.ID), kinds(j, "a1"))
	}
	// Failures: nothing in the reply, a list that is not JSON, a brain that fails.
	for name, reply := range map[string]string{"no list": "I cannot", "bad json": "[{\"scope\": ", "mixed": "[1, 2]"} {
		sa.replies = []string{reply}
		if _, err := s.Distill(context.Background(), "ana"); err == nil {
			t.Errorf("%s must fail", name)
		}
	}
	if len(m.Notes(ana.ID, "")) != len(notes) {
		t.Error("a failed distillation changes nothing")
	}
	sa.err = errors.New("no key")
	if _, err := s.Distill(context.Background(), "ana"); err == nil {
		t.Error("a failing brain")
	}
	sa.err = nil
	if _, err := s.Distill(context.Background(), "zz"); err == nil {
		t.Error("unknown kitty")
	}
	if _, err := s.distill(context.Background(), &kitty.Kitty{ID: "nobrain"}); err == nil {
		t.Error("no brain")
	}
	// Enough verdicts start one in the background, once at a time.
	sa.replies = []string{"[{\"scope\": \"user\", \"text\": \"Auto distilled\"}]"}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "q")
	for i := 0; i < 3; i++ {
		_ = s.Feedback(sess.ID, 1, map[bool]string{true: "up", false: "down"}[i%2 == 0], "note "+string(rune('a'+i)))
	}
	s.Wait()
	if !strings.Contains(strings.Join(func() []string {
		var out []string
		for _, n := range m.Notes(ana.ID, "") {
			out = append(out, n.Text)
		}
		return out
	}(), "|"), "Auto distilled") {
		t.Errorf("auto distillation: %+v", m.Notes(ana.ID, ""))
	}
	s.mu.Lock()
	s.distilling[ana.ID] = true
	s.mu.Unlock()
	s.distillLater(ana) // already running: nothing starts
	s.Wait()
	s.mu.Lock()
	delete(s.distilling, ana.ID)
	s.mu.Unlock()
	// parseNotes bounds the list.
	var many strings.Builder
	many.WriteString("[")
	for i := 0; i < 30; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"text":"n"}`)
	}
	many.WriteString("]")
	if got, _ := parseNotes(many.String()); len(got) != 20 {
		t.Errorf("bounded: %d", len(got))
	}
}

func TestLearningFromApprovalsAndAPI(t *testing.T) {
	s, sa, _, m := remembering(t)
	_ = sa
	ana, _ := s.fleet.Get("ana")
	// A no with a reason is a correction; a yes is a thumbs up.
	s.learnApproval(approvals.Approval{Kitty: ana.ID, Status: approvals.Rejected, Reason: "never post on weekends", Summary: "slack.send", Chat: "c"})
	s.learnApproval(approvals.Approval{Kitty: ana.ID, Status: approvals.Approved, Summary: "slack.send", Chat: "c"})
	s.learnApproval(approvals.Approval{Kitty: "gone", Status: approvals.Approved})
	if p := m.Profile(ana.ID); p.Up != 1 || p.Down != 1 {
		t.Errorf("profile = %+v", p)
	}
	if got := m.Notes(ana.ID, "user"); len(got) != 1 || got[0].Text != "Correction: never post on weekends" {
		t.Errorf("notes = %+v", got)
	}
	if fb := m.Recent(ana.ID, 0); fb[0].Source != "approval" || fb[0].Excerpt != "slack.send" {
		t.Errorf("verdicts = %+v", fb)
	}
	// What the page reads and writes.
	view, err := s.MemoryOf("ana")
	if err != nil || len(view.Notes) != 1 || view.Profile.Demand != 3 || !strings.Contains(view.Level, "balanced") || view.Trust != nil {
		t.Fatalf("view = %+v %v", view, err)
	}
	store, _ := approvals.Open(filepath.Join(t.TempDir(), "a.json"))
	s.SetApprovals(store)
	a, _ := store.Request(approvals.Approval{Kitty: ana.ID, Connector: "slack", Action: "send", Risk: "ask", Args: map[string]any{}})
	_, _ = store.Decide(a.ID, true, "", false)
	if view, _ := s.MemoryOf("ana"); view.Trust["slack.send"].Streak != 1 || view.Trust["slack.send"].Needs != 6 {
		t.Errorf("trust = %+v", view.Trust)
	}
	note, err := s.Remember("ana", "work", "Invoices are due on the 1st")
	if err != nil || !note.Pinned || note.Source != "person" {
		t.Errorf("the person's note = %+v %v", note, err)
	}
	if _, err := s.Remember("ana", "bogus", "x"); err == nil {
		t.Error("bad scope")
	}
	if _, err := s.Remember("zz", "user", "x"); err == nil {
		t.Error("unknown kitty")
	}
	if err := s.ForgetNote("ana", note.ID); err != nil {
		t.Error(err)
	}
	if err := s.ForgetNote("zz", note.ID); err == nil {
		t.Error("unknown kitty")
	}
	if p, err := s.SetProfile("ana", 5, false); err != nil || p.Demand != 5 || p.Auto {
		t.Errorf("profile = %+v %v", p, err)
	}
	if _, err := s.SetProfile("ana", 9, false); err == nil {
		t.Error("level out of range")
	}
	if _, err := s.SetProfile("zz", 3, true); err == nil {
		t.Error("unknown kitty")
	}
	if _, err := s.MemoryOf("zz"); err == nil {
		t.Error("unknown kitty")
	}
	// The level now moves how soon an action is trusted.
	if s.demandOf(ana) != 5 {
		t.Error("the memory feeds the trust")
	}
	if err := s.ForgetKitty(ana.ID); err != nil || len(m.Notes(ana.ID, "")) != 0 {
		t.Error("ForgetKitty")
	}
	// Without memory every call says so.
	plain := service(t, s.fleet)
	checks := []error{
		func() error { _, e := plain.MemoryOf("ana"); return e }(),
		func() error { _, e := plain.Remember("ana", "user", "x"); return e }(),
		plain.ForgetNote("ana", "x"),
		func() error { _, e := plain.SetProfile("ana", 3, true); return e }(),
		func() error { _, e := plain.Distill(context.Background(), "ana"); return e }(),
		plain.Feedback("c", 0, "up", ""),
	}
	for i, e := range checks {
		if e != ErrNoMemory {
			t.Errorf("check %d = %v", i, e)
		}
	}
	if plain.ForgetKitty("x") != nil {
		t.Error("forgetting with no memory is fine")
	}
	plain.SetMemory(nil)
	s.SetMemory(nil)
	if s.demand != nil || s.learnFromApproval != nil {
		t.Error("SetMemory(nil) unhooks")
	}
}

func TestLongThreadsAreSummarised(t *testing.T) {
	s, sa, _, _ := remembering(t)
	s.CompactAfter, s.CompactKeep = 6, 2
	sess, _ := s.Create("Big project", []string{"ana"})
	for i := 0; i < 3; i++ { // six turns: three questions and three answers
		sa.replies = []string{"answer " + string(rune('a'+i))}
		_, _ = s.Send(context.Background(), sess.ID, "question "+string(rune('a'+i)))
	}
	if got, _ := s.Get(sess.ID); got.Summary != "" {
		t.Error("not yet")
	}
	// The seventh turn goes over the limit: the brain summarises the early ones first, then answers.
	sa.replies = []string{"The person asked a, b and c and got answers.", "answer d"}
	_, _ = s.Send(context.Background(), sess.ID, "question d")
	got, _ := s.Get(sess.ID)
	if got.Summary != "The person asked a, b and c and got answers." || got.SummaryUpTo != 5 || len(got.Turns) != 8 {
		t.Fatalf("summary = %q up to %d, %d turns", got.Summary, got.SummaryUpTo, len(got.Turns))
	}
	asked := sa.seen[len(sa.seen)-2] // the summarising call
	if !strings.Contains(asked[1].Content, "[Pat]: question a") || !strings.Contains(asked[1].Content, "[ana]: answer b") || strings.Contains(asked[1].Content, "question d") || strings.Contains(asked[1].Content, "answer c") {
		t.Errorf("summarised: %q", asked[1].Content)
	}
	// The answer saw the summary and only the latest turns, not the early ones.
	answered := sa.seen[len(sa.seen)-1]
	if !strings.Contains(answered[0].Content, "Earlier in this conversation (a summary of its first 5 messages") || !strings.Contains(answered[0].Content, "got answers") {
		t.Errorf("system = %q", answered[0].Content)
	}
	var seen strings.Builder
	for _, m := range answered[1:] {
		seen.WriteString(m.Content + "\n")
	}
	if strings.Contains(seen.String(), "question a") || !strings.Contains(seen.String(), "question d") {
		t.Errorf("the thread seen = %q", seen.String())
	}
	// The next round folds the old summary and the new turns together.
	for i := 0; i < 2; i++ {
		sa.replies = []string{"answer " + string(rune('e'+i))}
		_, _ = s.Send(context.Background(), sess.ID, "question "+string(rune('e'+i)))
	}
	sa.replies = []string{"Now it also covers d, e and f.", "answer g"}
	_, _ = s.Send(context.Background(), sess.ID, "question g")
	got, _ = s.Get(sess.ID)
	folded := sa.seen[len(sa.seen)-2][1].Content
	if got.Summary != "Now it also covers d, e and f." || got.SummaryUpTo <= 5 || !strings.Contains(folded, "Summary so far:\nThe person asked a, b and c") {
		t.Errorf("second summary = %q up to %d; asked %q", got.Summary, got.SummaryUpTo, folded)
	}
	// A brain that cannot summarise leaves the thread as it was; a thread within the limit is untouched.
	s.CompactAfter = 2
	cut := got.SummaryUpTo
	sa.err = errors.New("down")
	s.compact(context.Background(), sess.ID, sa)
	if again, _ := s.Get(sess.ID); again.SummaryUpTo != cut {
		t.Error("a failed summary changes nothing")
	}
	sa.err = nil
	sa.replies = []string{"   "}
	s.compact(context.Background(), sess.ID, sa)
	if again, _ := s.Get(sess.ID); again.SummaryUpTo != cut {
		t.Error("an empty summary changes nothing")
	}
	s.CompactAfter = 0
	s.compact(context.Background(), sess.ID, sa)
	s.CompactAfter, s.CompactKeep = 2, 99
	s.compact(context.Background(), sess.ID, sa)
	s.compact(context.Background(), "gone", sa)
}
