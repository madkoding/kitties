package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/soul"
)

type fakeFleet struct {
	kitties map[string]*kitty.Kitty
	brains  map[string]provider.Client
	conns   *connector.Registry
}

func (f *fakeFleet) Get(ref string) (*kitty.Kitty, error) {
	for _, k := range f.kitties {
		if k.ID == ref || k.Name == ref {
			return k, nil
		}
	}
	return nil, errors.New("no such kitty " + ref)
}
func (f *fakeFleet) List() []*kitty.Kitty {
	var out []*kitty.Kitty
	for _, k := range f.kitties {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (f *fakeFleet) Brain(k *kitty.Kitty) (provider.Client, error) {
	b, ok := f.brains[k.ID]
	if !ok {
		return nil, errors.New("no brain")
	}
	return b, nil
}
func (f *fakeFleet) Connectors() *connector.Registry { return f.conns }

type echoConn struct{ calls []map[string]any }

func (e *echoConn) Describe() string { return "echoes" }
func (e *echoConn) Call(_ context.Context, a map[string]any) (string, error) {
	e.calls = append(e.calls, a)
	return "echoed", nil
}

// scripted answers with the given replies in order, remembering what it was asked.
type scripted struct {
	replies []string
	seen    [][]provider.Message
	err     error
}

func (s *scripted) Complete(_ context.Context, m []provider.Message) (provider.Reply, error) {
	s.seen = append(s.seen, m)
	if s.err != nil {
		return provider.Reply{}, s.err
	}
	r := s.replies[0]
	if len(s.replies) > 1 {
		s.replies = s.replies[1:]
	}
	return provider.Reply{Content: r, Usage: provider.Usage{Input: 1, Output: 2}}, nil
}
func (s *scripted) Describe() string { return "scripted" }

func fleet(t *testing.T) (*fakeFleet, *scripted, *scripted, *echoConn) {
	t.Helper()
	ana := &kitty.Kitty{ID: "a1", Name: "ana", Soul: soul.Default("ana"), Desktop: true, Connectors: []string{"echo"}}
	bo := &kitty.Kitty{ID: "b1", Name: "bo", Soul: soul.Default("bo")}
	sa, sb := &scripted{replies: []string{"ana says hi"}}, &scripted{replies: []string{"bo agrees"}}
	e := &echoConn{}
	reg := connector.NewRegistry()
	reg.Add("echo", e)
	return &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": ana, "b1": bo}, brains: map[string]provider.Client{"a1": sa, "b1": sb}, conns: reg}, sa, sb, e
}

func service(t *testing.T, f Fleet) *Service {
	t.Helper()
	s, err := New(f, filepath.Join(t.TempDir(), "chats"))
	if err != nil {
		t.Fatal(err)
	}
	// The older tests count rounds and steps exactly: no second attempt, and quick polls.
	s.DefaultLimits.Retries = 0
	s.pollEvery = time.Millisecond
	return s
}

func TestCreateListGetDelete(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	if _, err := s.Create("", nil); err == nil {
		t.Error("empty members")
	}
	if _, err := s.Create("", []string{"zz"}); err == nil {
		t.Error("unknown member")
	}
	sess, err := s.Create("", []string{"ana", "b1", "ana"})
	if err != nil || sess.Title != "ana, bo" || len(sess.Members) != 2 || !sess.Group() {
		t.Fatalf("Create = %+v, %v", sess, err)
	}
	single, _ := s.Create("solo", []string{"bo"})
	if single.Group() || single.Title != "solo" {
		t.Error("single chat")
	}
	got, err := s.Get(sess.ID)
	if err != nil || got.ID != sess.ID {
		t.Error("Get")
	}
	if _, err := s.Get("zz"); err == nil {
		t.Error("Get unknown")
	}
	if list := s.List(); len(list) != 2 || list[0].Turns != nil {
		t.Errorf("List = %v", list)
	}
	again, err := New(f, s.dir)
	if err != nil || len(again.List()) != 2 {
		t.Error("sessions survive a restart")
	}
	if _, err := s.AddMembers(sess.ID, []string{"zz"}); err == nil {
		t.Error("AddMembers unknown kitty")
	}
	if _, err := s.AddMembers("zz", nil); err == nil {
		t.Error("AddMembers unknown chat")
	}
	if got, err := s.AddMembers(single.ID, []string{"ana", "bo"}); err != nil || len(got.Members) != 2 {
		t.Error("AddMembers")
	}
	if got, err := s.RemoveMember(single.ID, "ana"); err != nil || len(got.Members) != 1 || got.Members[0] != "b1" {
		t.Error("RemoveMember")
	}
	if _, err := s.RemoveMember(single.ID, "zz"); err == nil {
		t.Error("RemoveMember unknown kitty")
	}
	if _, err := s.RemoveMember("zz", "ana"); err == nil {
		t.Error("RemoveMember unknown chat")
	}
	if err := s.Delete(single.ID); err != nil || len(s.List()) != 1 {
		t.Error("Delete")
	}
	if err := s.Delete("zz"); err == nil {
		t.Error("Delete unknown")
	}
	s.sessions["ghost"] = &Session{ID: "ghost"}
	if err := s.Delete("ghost"); err != nil {
		t.Error("a session without a file deletes fine")
	}
}

func TestLoadFailures(t *testing.T) {
	f, _, _, _ := fleet(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600)
	_ = os.Mkdir(filepath.Join(dir, "sub.json"), 0o700)
	if _, err := New(f, dir); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged chat: %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := New(f, file); err == nil {
		t.Error("a file where the folder should be")
	}
	s := service(t, f)
	s.dir = filepath.Join(file, "x")
	if _, err := s.Create("", []string{"ana"}); err == nil {
		t.Error("a save failure is reported")
	}
}

func TestSendSingle(t *testing.T) {
	f, sa, _, e := fleet(t)
	s := service(t, f)
	s.SetUser("Pat")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "  "); err == nil {
		t.Error("empty message")
	}
	if _, err := s.Send(context.Background(), "zz", "hi"); err == nil {
		t.Error("unknown chat")
	}
	sa.replies = []string{"@connector echo {\"text\":\"ping\"}\nworking on it", "all done"}
	replies, err := s.Send(context.Background(), sess.ID, "hello ana")
	if err != nil || len(replies) != 1 {
		t.Fatalf("Send = %+v, %v", replies, err)
	}
	r := replies[0]
	if r.From != "ana" || r.Text != "working on it\n\nall done" || len(r.Calls) != 1 || r.Calls[0].Output != "echoed" || r.Usage != (provider.Usage{Input: 2, Output: 4}) {
		t.Errorf("reply = %+v", r)
	}
	if len(e.calls) != 1 || e.calls[0]["text"] != "ping" {
		t.Errorf("connector calls = %v", e.calls)
	}
	first := sa.seen[0]
	if first[0].Role != "system" || !strings.Contains(first[0].Content, "You are ana") || !strings.Contains(first[0].Content, "echo: echoes") || !strings.Contains(first[0].Content, "kind to Pat") || !strings.Contains(first[0].Content, "/home/kitty/work") {
		t.Errorf("system prompt = %s", first[0].Content)
	}
	if first[1].Role != "user" || first[1].Content != "[Pat]: hello ana" {
		t.Errorf("user turn = %+v", first[1])
	}
	second := sa.seen[1]
	if second[len(second)-1].Role != "user" || !strings.Contains(second[len(second)-1].Content, "@connector echo") || !strings.Contains(second[len(second)-1].Content, "echoed") {
		t.Errorf("connector results go back: %+v", second[len(second)-1])
	}
	got, _ := s.Get(sess.ID)
	if len(got.Turns) != 2 || got.Turns[0].From != "user" || got.Turns[1].Kitty != "a1" {
		t.Errorf("turns = %+v", got.Turns)
	}
	// The kitty sees its own turn as the assistant on the next message.
	sa.replies = []string{""}
	replies, _ = s.Send(context.Background(), sess.ID, "and now?")
	if replies[0].Text != "(ana had nothing to add)" {
		t.Errorf("empty answer = %q", replies[0].Text)
	}
	seen := sa.seen[2]
	if seen[2].Role != "assistant" || seen[2].Content != "working on it\n\nall done" || seen[3].Content != "[Pat]: and now?" {
		t.Errorf("transcript = %+v", seen)
	}
	sa.err = errors.New("brain down")
	replies, err = s.Send(context.Background(), sess.ID, "?")
	if err != nil || !strings.Contains(replies[0].Text, "could not answer: brain down") {
		t.Errorf("a failing brain is reported in the thread: %+v %v", replies, err)
	}
	delete(f.brains, "a1")
	if replies, _ := s.Send(context.Background(), sess.ID, "?"); !strings.Contains(replies[0].Text, "no brain") {
		t.Error("no brain")
	}
}

func TestSendGroup(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("team", []string{"ana", "bo"})
	replies, err := s.Send(context.Background(), sess.ID, "plan the launch")
	if err != nil || len(replies) != 2 || replies[0].From != "ana" || replies[1].From != "bo" {
		t.Fatalf("group replies = %+v, %v", replies, err)
	}
	if sys := sa.seen[0][0].Content; !strings.Contains(sys, "group chat with other kitties: bo") {
		t.Errorf("ana knows about bo: %s", sys)
	}
	last := sb.seen[0][len(sb.seen[0])-1]
	if !strings.Contains(last.Content, "[User]: plan the launch\n\n[ana]: ana says hi") {
		t.Errorf("bo sees ana's answer: %+v", last)
	}
	replies, _ = s.Send(context.Background(), sess.ID, "@bo what do you think? @Bo @nobody")
	if len(replies) != 1 || replies[0].From != "bo" {
		t.Errorf("mentions pick the responders: %+v", replies)
	}
	bo := sb.seen[1]
	if bo[len(bo)-2].Role != "assistant" || bo[len(bo)-2].Content != "bo agrees" {
		t.Errorf("bo sees its own turn as the assistant: %+v", bo)
	}
	// A turn that ends on the kitty's own words gets a nudge.
	msgs := s.transcript("sys", []Turn{{From: "bo", Kitty: "b1", Text: "x"}}, f.kitties["b1"])
	if msgs[len(msgs)-1].Content != "(continue)" {
		t.Errorf("nudge: %+v", msgs)
	}
	s.MaxTurns = 1
	msgs = s.transcript("sys", []Turn{{From: "user", Text: "old"}, {From: "user", Text: "new"}}, f.kitties["b1"])
	if len(msgs) != 2 || msgs[1].Content != "[User]: new" {
		t.Errorf("MaxTurns keeps the tail: %+v", msgs)
	}
	delete(f.kitties, "a1")
	delete(f.kitties, "b1")
	if _, err := s.Send(context.Background(), sess.ID, "anyone?"); err == nil {
		t.Error("a chat whose kitties are gone fails")
	}
}

func TestConnectorRoundsAreBounded(t *testing.T) {
	f, sa, _, e := fleet(t)
	s := service(t, f)
	s.MaxRounds = 2
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@connector echo go"}
	replies, _ := s.Send(context.Background(), sess.ID, "loop")
	if len(e.calls) != 2 || len(replies[0].Calls) != 2 || replies[0].Text != "(ana had nothing to add)" {
		t.Errorf("rounds = %d, reply = %+v", len(e.calls), replies[0])
	}
	// With no connectors configured, the calls are ignored.
	f.conns = connector.NewRegistry()
	bo, _ := s.Create("", []string{"bo"})
	f.brains["b1"].(*scripted).replies = []string{"@connector echo go\nfine"}
	replies, _ = s.Send(context.Background(), bo.ID, "x")
	if replies[0].Text != "fine" || len(replies[0].Calls) != 0 {
		t.Errorf("no connectors: %+v", replies[0])
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	replies, _ = s.Send(context.Background(), bo.ID, "y")
	if !replies[0].At.Equal(now) {
		t.Error("turns are stamped")
	}
	s.dir = filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(s.dir, nil, 0o600)
	if _, err := s.Send(context.Background(), bo.ID, "z"); err == nil {
		t.Error("a save failure is reported")
	}
}
