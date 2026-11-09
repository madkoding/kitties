package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/artifacts"
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
