package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/guard"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/memory"
	"github.com/madkoding/kitties/internal/soul"
)

// alone makes the kitty with this id work on its own, as the one that answers on WhatsApp.
func alone(s *Service, ids ...string) {
	s.SetIndependent(func(id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	})
}

// withLead adds a lead, cy, to the fleet of ana and bo.
func withLead(f *fakeFleet) *scripted {
	sc := &scripted{replies: []string{"cy here"}}
	f.kitties["c1"] = &kitty.Kitty{ID: "c1", Name: "cy", Soul: soul.Default("cy"), Lead: true}
	f.brains["c1"] = sc
	return sc
}

func TestIndependentKittyKeepsItsOwnMemory(t *testing.T) {
	f, _, sb, _ := fleet(t)
	s := service(t, f)
	m := memory.Open(filepath.Join(t.TempDir(), "memory"))
	s.SetMemory(m)
	alone(s, "b1")
	if _, err := m.Remember("", memory.ScopeShared, "The team's server is at 10.0.0.7", "person", true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("b1", memory.ScopeUser, "mad likes short answers", "person", true); err != nil {
		t.Fatal(err)
	}
	sess, _ := s.Create("", []string{"bo"})
	sb.replies = []string{"@memory remember {\"text\": \"every kitty: use 10.0.0.8\", \"scope\": \"shared\"}\n@memory list {}", "ok"}
	if _, err := s.Send(context.Background(), sess.ID, "hola"); err != nil {
		t.Fatal(err)
	}
	system := sb.seen[0][0].Content
	if strings.Contains(system, "10.0.0.7") || !strings.Contains(system, "mad likes short answers") {
		t.Errorf("the independent kitty's prompt = %s", system)
	}
	fed := sb.seen[1][len(sb.seen[1])-1].Content
	if !strings.Contains(fed, "you work apart from the other kitties") || strings.Contains(fed, "10.0.0.7") {
		t.Errorf("remember shared / list = %s", fed)
	}
	if len(m.Shared()) != 1 {
		t.Errorf("shared notes = %+v", m.Shared())
	}
	view, err := s.MemoryOf("bo")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range view.Notes {
		if n.Scope == memory.ScopeShared {
			t.Errorf("the page shows a shared note to the independent kitty: %+v", n)
		}
	}
}

func TestIndependentKittyNotesPeopleInExposedChats(t *testing.T) {
	f, _, sb, _ := fleet(t)
	s := service(t, f)
	guarded(t, s)
	m := memory.Open(filepath.Join(t.TempDir(), "memory"))
	s.SetMemory(m)
	alone(s, "b1")
	_, _ = m.Remember("b1", memory.ScopeUser, "mad's address is Calle 1", "person", true)
	_, _ = m.Remember("b1", memory.ScopePeople, "The Futbol group plays on Sundays", "kitty", false)
	id := exposedChat(t, s, "bo")
	sb.replies = []string{"@memory remember {\"text\": \"Ana brings the ball\", \"scope\": \"user\"}", "Got it!"}
	if _, err := s.Send(context.Background(), id, "Ana brings the ball on Sunday"); err != nil {
		t.Fatal(err)
	}
	system := sb.seen[0][0].Content
	if !strings.Contains(system, "THIS CHAT IS WITH SOMEONE ELSE") || !strings.Contains(system, "plays on Sundays") || strings.Contains(system, "Calle 1") {
		t.Errorf("exposed prompt = %s", system)
	}
	notes := m.Own("b1", memory.ScopePeople)
	if len(notes) != 2 {
		t.Fatalf("people notes = %+v", notes)
	}
	// A kitty that does not work on its own keeps no memory in exposed chats, as before.
	alone(s)
	sb.replies = []string{"@memory remember {\"text\": \"Bo is late\"}", "ok"}
	if _, err := s.Send(context.Background(), id, "Bo is late"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.seen[2][0].Content, "plays on Sundays") || len(m.Own("b1", "")) != 3 {
		t.Errorf("a kitty that is not independent used its memory where someone else writes")
	}
}

func TestIndependentKittyDoesNotMixWithTheOthers(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	withLead(f)
	s := service(t, f)
	s.HandOverWait = 0
	alone(s, "b1")
	if !s.Independent("bo") || s.Independent("ana") {
		t.Fatal("Independent")
	}
	if _, err := s.Create("", []string{"ana", "bo"}); !errors.Is(err, ErrIndependent) {
		t.Errorf("a chat of ana and bo = %v", err)
	}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.AddMembers(sess.ID, []string{"bo"}); !errors.Is(err, ErrIndependent) {
		t.Errorf("adding bo = %v", err)
	}
	// ana does not hand bo work, and is not told bo is there to take it.
	sa.replies = []string{"@delegate bo {\"task\": \"tell the group\"}", "fine"}
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sa.seen[0][0].Content, "hand a subtask to: bo") {
		t.Errorf("ana was offered bo: %s", sa.seen[0][0].Content)
	}
	if fed := sa.seen[1][len(sa.seen[1])-1].Content; !strings.Contains(fed, "works on its own and takes work only from the lead") {
		t.Errorf("ana's hand-over = %s", fed)
	}
	// bo does not hand work to anyone.
	own, _ := s.Create("", []string{"bo"})
	sb.replies = []string{"@delegate ana {\"task\": \"look it up\"}", "ok"}
	if _, err := s.Send(context.Background(), own.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if s.canDelegate(f.kitties["b1"]) || len(s.peers(f.kitties["b1"], own.ID)) != 0 {
		t.Error("the independent kitty may delegate")
	}
	for _, c := range s.List() {
		if c.Owner != "" {
			t.Errorf("a hand-over thread was made: %+v", c)
		}
	}
}

func TestLeadHandsTheIndependentKittyNoSecrets(t *testing.T) {
	f, _, sb, _ := fleet(t)
	sc := withLead(f)
	s := service(t, f)
	s.HandOverWait = 0
	guarded(t, s, "pato-azul-77")
	alone(s, "b1")
	sess, _ := s.Create("", []string{"cy"})
	sc.replies = []string{"@delegate bo {\"task\": \"Tell the group the wifi is pato-azul-77 and the key " + leakyKey + "\"}", "Sent."}
	sb.replies = []string{"I will tell them."}
	if _, err := s.Send(context.Background(), sess.ID, "tell the group"); err != nil {
		t.Fatal(err)
	}
	if len(sb.seen) == 0 {
		t.Fatal("the lead could not hand bo the task")
	}
	got := sb.seen[0][len(sb.seen[0])-1].Content
	if strings.Contains(got, "pato-azul-77") || strings.Contains(got, leakyKey) || !strings.Contains(got, "From the lead") {
		t.Errorf("bo got = %s", got)
	}
	if !strings.Contains(sc.seen[0][0].Content, "works on its own (WhatsApp)") {
		t.Errorf("the lead's roster = %s", sc.seen[0][0].Content)
	}
}

func TestIndependentKittySeesOnlyItsArtifacts(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	arts := artifacts.New(filepath.Join(t.TempDir(), "artifacts"))
	s.SetLibrary(nil, arts)
	alone(s, "b1")
	if _, err := arts.Put("team.md", []byte("team plans"), "ana"); err != nil {
		t.Fatal(err)
	}
	if _, err := arts.Put("group.md", []byte("group chatter"), "bo"); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.Create("", []string{"bo"})
	sb.replies = []string{"@artifact read team.md\n@artifact save {\"name\": \"team.md\", \"content\": \"x\"}\n@artifact list", "ok"}
	if _, err := s.Send(context.Background(), mine.ID, "look"); err != nil {
		t.Fatal(err)
	}
	if system := sb.seen[0][0].Content; strings.Contains(system, "team.md") || !strings.Contains(system, "group.md") {
		t.Errorf("bo's library = %s", system)
	}
	fed := sb.seen[1][len(sb.seen[1])-1].Content
	if strings.Contains(fed, "team plans") || !strings.Contains(fed, "another kitty's file") {
		t.Errorf("bo read = %s", fed)
	}
	theirs, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@artifact read group.md", "ok"}
	if _, err := s.Send(context.Background(), theirs.ID, "look"); err != nil {
		t.Fatal(err)
	}
	if system := sa.seen[0][0].Content; strings.Contains(system, "group.md") || !strings.Contains(system, "team.md") {
		t.Errorf("ana's library = %s", system)
	}
	if fed := sa.seen[1][len(sa.seen[1])-1].Content; strings.Contains(fed, "group chatter") {
		t.Errorf("ana read bo's file: %s", fed)
	}
}

func TestTheLeadIsNeverIndependent(t *testing.T) {
	f, _, _, _ := fleet(t)
	withLead(f)
	s := service(t, f)
	alone(s, "c1")
	if s.Independent("cy") {
		t.Error("the lead works on its own")
	}
	if _, err := s.Create("", []string{"cy", "ana"}); err != nil {
		t.Error(err)
	}
}

func TestIndependentKittySendsOnlyItsOwnFiles(t *testing.T) {
	f, sa, _, echo := fleet(t)
	s := service(t, f)
	arts := artifacts.New(filepath.Join(t.TempDir(), "artifacts"))
	s.SetLibrary(nil, arts)
	alone(s, "a1")
	_, _ = arts.Put("team.pdf", []byte("team"), "bo")
	_, _ = arts.Put("mine.pdf", []byte("mine"), "ana")
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@connector echo {\"text\": \"here\", \"file\": \"team.pdf\"}\n@connector echo {\"text\": \"here\", \"file\": \"mine.pdf\"}", "ok"}
	if _, err := s.Send(context.Background(), sess.ID, "send it"); err != nil {
		t.Fatal(err)
	}
	if len(echo.calls) != 1 || echo.calls[0]["file"] != "mine.pdf" {
		t.Errorf("calls = %v", echo.calls)
	}
	if fed := sa.seen[1][len(sa.seen[1])-1].Content; !strings.Contains(fed, "team.pdf is not a file you saved") {
		t.Errorf("feedback = %s", fed)
	}
}

func TestIndependentKittyAsksTheLeadFromAGroupWithThePersonsYes(t *testing.T) {
	f, _, sb, _ := fleet(t)
	sc := withLead(f)
	s := service(t, f)
	s.HandOverWait = 0
	guarded(t, s, "pato-azul-77")
	store, err := approvals.Open(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetApprovals(store)
	alone(s, "b1")
	var relayed []string
	s.SetRelay(func(_ context.Context, chatID, text string) error {
		relayed = append(relayed, chatID+": "+text)
		return nil
	})
	id := exposedChat(t, s, "bo")
	sb.replies = []string{"@ask_lead {\"question\": \"Is mad free on Friday for the match?\"}", "Let me check and come back."}
	if _, err := s.Send(context.Background(), id, "is mad coming on Friday?"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.seen[0][0].Content, "@ask_lead") {
		t.Errorf("bo was not told it may ask the lead: %s", sb.seen[0][0].Content)
	}
	pending := store.List("pending", 10)
	if len(pending) != 1 || pending[0].Connector != AskLeadConnector || len(sc.seen) != 0 {
		t.Fatalf("pending = %+v, lead asked %d times", pending, len(sc.seen))
	}
	// The person says yes: the lead hears it, answers, and bo says it in the group.
	sc.replies = []string{"@answer_kitty bo {\"text\": \"Yes, he comes; the wifi is pato-azul-77\"}", "Told bo."}
	sb.replies = []string{"Yes, mad comes on Friday!"}
	if _, err := s.DecideApproval(context.Background(), pending[0].ID, true, "", false); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if len(sc.seen) == 0 || !strings.Contains(sc.seen[0][len(sc.seen[0])-1].Content, "repeats your answer there") {
		t.Fatalf("the lead was not told where its answer goes")
	}
	// What bo would say waits for a second look before it reaches the group.
	if len(relayed) != 0 {
		t.Fatalf("said before the person looked: %v", relayed)
	}
	say := store.List("pending", 10)
	if len(say) != 1 || say[0].Action != "say:"+id || !strings.Contains(say[0].Summary, "Yes, mad comes on Friday!") || !strings.Contains(say[0].Summary, "Is mad free on Friday") {
		t.Fatalf("pending say = %+v", say)
	}
	if _, err := s.DecideApproval(context.Background(), say[0].ID, true, "", false); err != nil {
		t.Fatal(err)
	}
	if len(relayed) != 1 || !strings.Contains(relayed[0], id+": Yes, mad comes on Friday!") {
		t.Errorf("relayed = %v", relayed)
	}
	for _, m := range sb.seen[len(sb.seen)-1] {
		if strings.Contains(m.Content, "pato-azul-77") {
			t.Errorf("the lead's secret reached bo: %s", m.Content)
		}
	}
	// A no leaves the lead out.
	sb.replies = []string{"@ask_lead {\"question\": \"again?\"}", "I will check."}
	_, _ = s.Send(context.Background(), id, "and Saturday?")
	pending = store.List("pending", 10)
	asked := len(sc.seen)
	if _, err := s.DecideApproval(context.Background(), pending[0].ID, false, "", false); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if len(sc.seen) != asked {
		t.Error("the lead was asked after a no")
	}
	// A no to what it would say keeps it off WhatsApp.
	sc.replies = []string{"@answer_kitty bo {\"text\": \"No\"}", "ok"}
	sb.replies = []string{"@ask_lead {\"question\": \"Sunday?\"}", "I will check.", "No, sorry."}
	_, _ = s.Send(context.Background(), id, "and Sunday?")
	pending = store.List("pending", 10)
	_, _ = s.DecideApproval(context.Background(), pending[0].ID, true, "", false)
	s.Wait()
	pending = store.List("pending", 10)
	if len(pending) != 1 || pending[0].Action != "say:"+id {
		t.Fatalf("pending = %+v", pending)
	}
	if _, err := s.DecideApproval(context.Background(), pending[0].ID, false, "", false); err != nil {
		t.Fatal(err)
	}
	if len(relayed) != 1 {
		t.Errorf("a rejected answer was said: %v", relayed)
	}
}

func TestOtherKittiesDoNotAskTheLeadFromGroups(t *testing.T) {
	f, _, sb, _ := fleet(t)
	withLead(f)
	s := service(t, f)
	guarded(t, s)
	store, _ := approvals.Open(filepath.Join(t.TempDir(), "approvals.json"))
	s.SetApprovals(store)
	id := exposedChat(t, s, "bo")
	sb.replies = []string{"@ask_lead {\"question\": \"x?\"}", "ok"}
	if _, err := s.Send(context.Background(), id, "hola"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.seen[0][0].Content, "@ask_lead") || len(store.List("pending", 10)) != 0 {
		t.Error("a kitty that is not independent may ask the lead from a group")
	}
}

func TestAlwaysTrustsOneGroupWithTheLeadsAnswers(t *testing.T) {
	f, _, sb, _ := fleet(t)
	sc := withLead(f)
	s := service(t, f)
	s.HandOverWait = 0
	guarded(t, s)
	store, _ := approvals.Open(filepath.Join(t.TempDir(), "approvals.json"))
	s.SetApprovals(store)
	alone(s, "b1")
	var relayed []string
	s.SetRelay(func(_ context.Context, _ string, text string) error {
		relayed = append(relayed, text)
		return nil
	})
	id := exposedChat(t, s, "bo")
	ask := func(q, answer string) {
		t.Helper()
		sc.replies = []string{"@answer_kitty bo {\"text\": \"" + answer + "\"}", "ok"}
		sb.replies = []string{"@ask_lead {\"question\": \"" + q + "\"}", "I will check.", answer}
		if _, err := s.Send(context.Background(), id, q); err != nil {
			t.Fatal(err)
		}
	}
	// The first time, both yeses are "always": for this chat, from now on.
	ask("Friday?", "Yes, Friday.")
	p := store.List("pending", 10)
	if _, err := s.DecideApproval(context.Background(), p[0].ID, true, "", true); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	p = store.List("pending", 10)
	if _, err := s.DecideApproval(context.Background(), p[0].ID, true, "", true); err != nil {
		t.Fatal(err)
	}
	if len(relayed) != 1 {
		t.Fatalf("relayed = %v", relayed)
	}
	// The next question goes to the lead and back to the group without asking.
	ask("Saturday?", "No, not Saturday.")
	s.Wait()
	if len(store.List("pending", 10)) != 0 || len(relayed) != 2 || relayed[1] != "No, not Saturday." {
		t.Errorf("pending %v relayed %v", store.List("pending", 10), relayed)
	}
	// Another chat still asks.
	other, _ := s.Create("WhatsApp +56922222222", []string{"bo"})
	_ = s.Expose(other.ID, "WhatsApp +56922222222", true)
	sb.replies = []string{"@ask_lead {\"question\": \"x?\"}", "I will check."}
	_, _ = s.Send(context.Background(), other.ID, "x?")
	if len(store.List("pending", 10)) != 1 {
		t.Errorf("another chat did not ask: %v", store.List("pending", 10))
	}
}

func TestIndependentKittyBrowsesOnItsOwnComputerInGroups(t *testing.T) {
	s, d, sa := deskService(t,
		"Let me look it up.\n@tool page_goto {\"url\": \"https://example.com/fixtures\"}\n@tool type_secret {\"name\": \"bank\"}",
		"@tool done {\"summary\": \"The match is on Friday at 8. (key "+leakyKey+")\"}")
	g := guarded(t, s)
	alone(s, "a1")
	id := exposedChat(t, s, "ana")
	turns, err := s.Send(context.Background(), id, "when is the match?")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || !turns[0].Working {
		t.Fatalf("ana did not start on her computer: %+v", turns)
	}
	sess := waitDone(t, s, id)
	system := sa.seen[0][0].Content
	if !strings.Contains(system, "THIS CHAT IS WITH SOMEONE ELSE") || !strings.Contains(system, "you keep your own computer") || strings.Contains(system, "type_secret (you are never shown") {
		t.Errorf("exposed prompt = %s", system)
	}
	browsed := false
	for _, e := range d.execs {
		browsed = browsed || strings.Contains(e, "example.com/fixtures")
		if strings.Contains(e, "bank") {
			t.Errorf("a secret was typed: %s", e)
		}
	}
	if !browsed {
		t.Errorf("ana did not browse: %v", d.execs)
	}
	last := sess.Turns[len(sess.Turns)-1]
	if !strings.Contains(last.Text, "Friday at 8") || strings.Contains(last.Text, leakyKey) {
		t.Errorf("answer = %q", last.Text)
	}
	if g.Status().Counts[guard.KindBlocked] == 0 {
		t.Errorf("type_secret was not recorded as refused: %v", g.Status().Counts)
	}
}

func TestLinkProblem(t *testing.T) {
	for raw, bad := range map[string]bool{
		"https://example.com/fixtures":        false,
		"http://news.example.org/a?b=1":       false,
		"https://8.8.8.8/":                    false,
		"file:///etc/passwd":                  true,
		"javascript:alert(1)":                 true,
		"https://bank.com@evil.example/login": true,
		"http://localhost:8080/":              true,
		"http://127.0.0.1/":                   true,
		"http://192.168.1.1/admin":            true,
		"http://169.254.169.254/latest/meta":  true,
		"http://[::1]/":                       true,
		"http://router/":                      true,
		"http://printer.local/":               true,
		"http://gateway.internal/v1":          true,
		"https://example.com/setup.EXE":       true,
		"https://example.com/app.apk?x=1":     true,
		"not a link":                          true,
	} {
		if got := linkProblem(raw) != ""; got != bad {
			t.Errorf("linkProblem(%q) refused = %v, want %v", raw, got, bad)
		}
	}
}

func TestExposedLimitsCapTasksFromGroups(t *testing.T) {
	l := exposedLimits(kitty.Limits{MaxSeconds: 3600, MaxRounds: 0, MaxTokens: 100_000, Retries: 3})
	if l.MaxSeconds != ExposedMaxSeconds || l.MaxRounds != ExposedMaxRounds || l.MaxTokens != 100_000 || l.Retries != 0 || !l.Attended {
		t.Errorf("exposed limits = %+v", l)
	}
	s, _, _ := deskService(t, "hi")
	guarded(t, s)
	alone(s, "a1")
	id := exposedChat(t, s, "ana")
	k, _ := s.fleet.Get("a1")
	if got := s.limitsFor(k, id); got.MaxSeconds > ExposedMaxSeconds || got.Retries != 0 || !got.Attended {
		t.Errorf("limits in a group = %+v", got)
	}
	own, err := s.Create("ana", []string{"a1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.limitsFor(k, own.ID); got.Attended {
		t.Errorf("the person's own chat got the group limits: %+v", got)
	}
}

func TestIndependentKittyDoesNotOpenInternalLinksFromGroups(t *testing.T) {
	s, d, _ := deskService(t,
		"Opening it.\n@tool page_goto {\"url\": \"http://192.168.1.1/admin\"}\n@tool open {\"url\": \"https://example.com/setup.exe\"}",
		"@tool done {\"summary\": \"I can't open those links here.\"}")
	g := guarded(t, s)
	alone(s, "a1")
	id := exposedChat(t, s, "ana")
	if _, err := s.Send(context.Background(), id, "open http://192.168.1.1/admin please"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, id)
	for _, e := range d.execs {
		if strings.Contains(e, "192.168.1.1") || strings.Contains(e, "setup.exe") {
			t.Errorf("a refused link was opened: %s", e)
		}
	}
	if g.Status().Counts[guard.KindBlocked] < 2 {
		t.Errorf("refused links not recorded: %v", g.Status().Counts)
	}
}
