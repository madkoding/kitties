package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { now = now.Add(time.Minute); return now }
	n := 0
	s.id = func() string { n++; return "a" + string(rune('0'+n)) }
	return s
}

func req(s *Store, text string) *Approval {
	a, err := s.Request(Approval{Kitty: "k1", KittyName: "mimi", Connector: "slack", Kind: "slack", Action: "send", Args: map[string]any{"text": text}, Risk: "ask", Chat: "c1"})
	if err != nil {
		panic(err)
	}
	return a
}

func TestThreshold(t *testing.T) {
	want := map[int]int{0: 2, 1: 2, 2: 4, 3: 6, 4: 12, 5: 0, 9: 0}
	for demand, n := range want {
		if got := Threshold(demand); got != n {
			t.Errorf("Threshold(%d) = %d, want %d", demand, got, n)
		}
	}
}

func TestRequestDecideAndTrust(t *testing.T) {
	s := open(t)
	a := req(s, "hello")
	if a.ID != "a1" || a.Status != Pending || a.At.IsZero() {
		t.Fatalf("request = %+v", a)
	}
	// The same request again is the same request; another text is another.
	if again := req(s, "hello"); again.ID != a.ID || len(s.List(Pending, 0)) != 1 {
		t.Error("an identical pending request is not duplicated")
	}
	other := req(s, "bye")
	if other.ID == a.ID || len(s.List(Pending, 0)) != 2 {
		t.Error("a different one is new")
	}
	// Nothing is trusted yet: it waits.
	if v := s.Check("k1", "slack", "send", "ask", "ask", 3); v.Run || v.Why != "waiting for the person" {
		t.Errorf("first check = %+v", v)
	}
	// Approving builds the streak; at the threshold for a person of demand 3 (6) it runs alone.
	for i := 0; i < 6; i++ {
		x := req(s, "msg "+string(rune('a'+i)))
		if v := s.Check("k1", "slack", "send", "ask", "ask", 3); v.Run && i < 5 {
			t.Fatalf("trusted too early at %d", i)
		}
		if _, err := s.Decide(x.ID, true, "", false); err != nil {
			t.Fatal(err)
		}
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 3); !v.Run || !strings.Contains(v.Why, "6 approvals in a row") {
		t.Errorf("trusted = %+v", v)
	}
	// The same history means less to a stricter person, and nothing to the strictest.
	if v := s.Check("k1", "slack", "send", "ask", "ask", 4); v.Run {
		t.Error("a demanding person wants twelve")
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 5); v.Run {
		t.Error("the strictest person never trusts")
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 1); !v.Run {
		t.Error("a relaxed person trusts sooner")
	}
	// Another kitty or another action has its own history.
	if v := s.Check("k2", "slack", "send", "ask", "ask", 3); v.Run {
		t.Error("trust is per kitty")
	}
	if v := s.Check("k1", "slack", "history", "ask", "ask", 3); v.Run {
		t.Error("trust is per action")
	}
	// A no starts the streak over, keeps the reason and counts.
	n := req(s, "risky")
	rej, err := s.Decide(n.ID, false, "  too informal ", false)
	if err != nil || rej.Status != Rejected || rej.Reason != "too informal" || rej.DecidedAt.IsZero() {
		t.Fatalf("reject = %+v %v", rej, err)
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 3); v.Run {
		t.Error("a no resets the trust")
	}
	if st := s.Stats("k1")["slack.send"]; st.Approved != 6 || st.Rejected != 1 || st.Streak != 0 {
		t.Errorf("stats = %+v", st)
	}
	// Decisions are final, and unknown ids and nonsense are refused.
	if _, err := s.Decide(n.ID, true, "", false); err == nil || !strings.Contains(err.Error(), "already rejected") {
		t.Errorf("deciding twice: %v", err)
	}
	if _, err := s.Decide("zz", true, "", false); err == nil {
		t.Error("unknown id")
	}
	m := req(s, "x")
	if _, err := s.Decide(m.ID, false, "", true); err == nil {
		t.Error("a no cannot be for good")
	}
	// Nothing is limited unless someone set it: outside "ask" mode, and without a rule, even a
	// refund goes ahead; what the account may do is the service's to limit.
	if v := s.Check("k9", "slack", "send", "ask", "auto", 3); !v.Run {
		t.Error("auto mode")
	}
	if v := s.Check("k1", "pay", "refund", "always", "", 5); !v.Run || v.Why != WhyFree {
		t.Errorf("no limit set = %+v", v)
	}
	if v := s.Check("k1", "slack", "history", "", "ask", 5); !v.Run {
		t.Error("ask mode holds only what reaches people")
	}
}

func TestSetRule(t *testing.T) {
	s := open(t)
	// A rule for every action of a connection, and one for an action, which wins.
	if _, err := s.SetRule(Rule{Kitty: "k1", KittyName: "mimi", Connector: "x", Mode: ModeAsk}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetRule(Rule{Kitty: "k1", Connector: "x", Action: "Like", Mode: ModeNever}); err != nil {
		t.Fatal(err)
	}
	if v := s.Check("k1", "x", "post", "ask", "", 3); v.Run || v.Blocked {
		t.Errorf("x.* ask = %+v", v)
	}
	if v := s.Check("k1", "x", "like", "ask", "", 3); !v.Blocked || v.Run {
		t.Errorf("x.like never = %+v", v)
	}
	if v := s.Check("k2", "x", "post", "ask", "", 3); !v.Run {
		t.Error("rules are per kitty")
	}
	if s.ModeOf("k1", "x", "") != ModeAsk || s.ModeOf("k1", "x", "LIKE") != ModeNever || s.ModeOf("k1", "x", "post") != "" {
		t.Error("ModeOf is the rule for exactly that action")
	}
	// Setting again changes the mode; an empty mode takes the limit away.
	if r, err := s.SetRule(Rule{Kitty: "k1", Connector: "x", Action: "like", Mode: ModeAuto}); err != nil || r.Mode != ModeAuto || len(s.Rules()) != 2 {
		t.Errorf("change = %+v %v %d", r, err, len(s.Rules()))
	}
	if r, err := s.SetRule(Rule{Kitty: "k1", Connector: "x", Action: "*", Mode: ""}); err != nil || r != nil || len(s.Rules()) != 1 {
		t.Errorf("remove = %+v %v", r, err)
	}
	if r, err := s.SetRule(Rule{Kitty: "k1", Connector: "y", Mode: ""}); err != nil || r != nil {
		t.Error("removing a limit that is not there is no error")
	}
	for _, bad := range []Rule{{Connector: "x", Mode: ModeAsk}, {Kitty: "k1", Mode: ModeAsk}, {Kitty: "k1", Connector: "x", Mode: "maybe"}} {
		if _, err := s.SetRule(bad); err == nil {
			t.Errorf("%+v is refused", bad)
		}
	}
}

func TestAlwaysRulesAndForgetting(t *testing.T) {
	s := open(t)
	a := req(s, "hi")
	got, err := s.Decide(a.ID, true, "", true)
	if err != nil || !got.Always {
		t.Fatalf("approve for good = %+v %v", got, err)
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 5); !v.Run || v.Why != "allowed for good by the person" {
		t.Errorf("a rule beats the demand: %+v", v)
	}
	rules := s.Rules()
	if len(rules) != 1 || rules[0].Mode != "auto" || rules[0].KittyName != "mimi" || rules[0].Created.IsZero() {
		t.Fatalf("rules = %+v", rules)
	}
	// Approving again for good does not duplicate; taking the rule back asks again.
	b := req(s, "again")
	_, _ = s.Decide(b.ID, true, "", true)
	if len(s.Rules()) != 1 {
		t.Error("one rule per kind of action")
	}
	if err := s.RemoveRule(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	if v := s.Check("k1", "slack", "send", "ask", "ask", 1); v.Run {
		t.Error("the streak starts over with the rule gone")
	}
	if err := s.RemoveRule("zz"); err == nil {
		t.Error("unknown rule")
	}
	// A "ask me" rule (set by hand in the file) beats trust.
	s.d.Rules = append(s.d.Rules, &Rule{ID: "r9", Kitty: "k1", Connector: "slack", Action: "send", Mode: "ask"})
	s.d.Stats[statKey("k1", "slack", "send")].Streak = 99
	if v := s.Check("k1", "slack", "send", "ask", "ask", 1); v.Run || !strings.Contains(v.Why, "asked to be asked") {
		t.Errorf("ask rule = %+v", v)
	}
	// Done records the result; Get and List find it; Forget drops a removed kitty's things.
	s.Done(a.ID, "posted", "")
	s.Done("zz", "ignored", "")
	if got, _ := s.Get(a.ID); got.Result != "posted" {
		t.Errorf("result = %+v", got)
	}
	if _, err := s.Get("zz"); err == nil {
		t.Error("Get unknown")
	}
	if l := s.List(Approved, 1); len(l) != 1 || l[0].ID != b.ID {
		t.Errorf("newest first, limited: %+v", l)
	}
	if err := s.Forget("k1"); err != nil || len(s.List("", 0)) != 0 || len(s.Rules()) != 0 || len(s.Stats("k1")) != 0 {
		t.Error("Forget")
	}
}

func TestPersistenceAndTrim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "approvals.json")
	s, _ := Open(path)
	a, _ := s.Request(Approval{Kitty: "k", Connector: "c", Action: "send", Args: map[string]any{"text": "x"}, Risk: "ask"})
	_, _ = s.Decide(a.ID, true, "", true)
	again, err := Open(path)
	if err != nil || len(again.List("", 0)) != 1 || len(again.Rules()) != 1 || again.Stats("k")["c.send"].Streak != 1 {
		t.Fatalf("reopen: %v", err)
	}
	// Damaged and unreadable files are errors; a missing one is an empty store.
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{nope"), 0o600)
	if _, err := Open(bad); err == nil {
		t.Error("damaged file")
	}
	if _, err := Open(dir); err == nil {
		t.Error("a directory")
	}
	empty := filepath.Join(dir, "empty.json")
	_ = os.WriteFile(empty, []byte("{}"), 0o600)
	if e, err := Open(empty); err != nil || e.d.Stats == nil {
		t.Error("an empty object")
	}
	// Only decided requests are forgotten past the limit; pending ones stay.
	old := maxKept
	maxKept = 40
	defer func() { maxKept = old }()
	s2 := open(t)
	for i := 0; i < maxKept+20; i++ {
		x, _ := s2.Request(Approval{Kitty: "k", Connector: "c", Action: "send", Args: map[string]any{"n": float64(i)}, Risk: "ask"})
		if i%2 == 0 {
			_, _ = s2.Decide(x.ID, true, "", false)
		}
	}
	if got := len(s2.List("", 0)); got > maxKept+10 || len(s2.List(Pending, 0)) != 30 {
		t.Errorf("kept %d, pending %d", got, len(s2.List(Pending, 0)))
	}
	// A store whose file cannot be written refuses the request and keeps nothing.
	if os.Getuid() != 0 {
		ro := filepath.Join(dir, "ro")
		_ = os.Mkdir(ro, 0o500)
		bad, _ := Open(filepath.Join(ro, "x", "a.json"))
		if _, err := bad.Request(Approval{Kitty: "k", Connector: "c", Action: "send", Risk: "ask"}); err == nil || len(bad.List("", 0)) != 0 {
			t.Error("a failed save keeps nothing")
		}
	}
	if randomID() == randomID() {
		t.Error("ids are random")
	}
}

func TestSummarise(t *testing.T) {
	got := Summarise("team-slack", "send", map[string]any{"text": "We ship Friday.", "channel": "#launch", "extra": "ignored"})
	if got != "team-slack.send — channel: #launch; text: We ship Friday." {
		t.Errorf("summary = %q", got)
	}
	if Summarise("x", "like", nil) != "x.like" {
		t.Error("no arguments")
	}
	long := Summarise("a", "b", map[string]any{"text": strings.Repeat("x", 900), "to": 1.0, "id": "", "title": "t", "subject": "s", "message": "m"})
	if len(long) > 705 || !strings.HasPrefix(long, "a.b — to: 1; title: t; subject: s; text: xxx") {
		t.Errorf("long = %q", long)
	}
	if RiskText(connector.RiskAsk) != "ask" {
		t.Error("RiskText")
	}
}
