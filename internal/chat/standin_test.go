package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/soul"
)

func TestAFreeKittyStandsInWithoutABackup(t *testing.T) {
	s, d, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	cy := &kitty.Kitty{ID: "c1", Name: "cy", Soul: soul.Default("cy"), Desktop: true}
	d.kitties["c1"] = cy
	d.brains["c1"] = &scripted{replies: []string{"Done: cy finished it."}}
	ana, _ := d.Get("ana")
	ana.Limits = kitty.Limits{MaxRounds: 1}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "count the things")
	turn := waitDone(t, s, sess.ID).Turns[1]
	if turn.NeedsHelp || !strings.Contains(turn.Text, "Handed over to cy, who answered:\nDone: cy finished it.") {
		t.Fatalf("turn = %+v", turn)
	}
}

func TestStandIn(t *testing.T) {
	s, d, _ := deskService(t)
	ana, _ := d.Get("ana")
	// bo has no desktop: it cannot do a desktop task.
	if s.standIn(ana) != nil {
		t.Error("no kitty with a desktop is free")
	}
	cy := &kitty.Kitty{ID: "c1", Name: "cy", Desktop: true, Status: kitty.StatusError}
	dee := &kitty.Kitty{ID: "d1", Name: "dee", Desktop: true}
	d.kitties["c1"], d.kitties["d1"] = cy, dee
	if got := s.standIn(ana); got == nil || got.Name != "dee" {
		t.Errorf("a kitty in error is skipped: %v", got)
	}
	// One at work on something else is skipped too.
	sess, _ := s.Create("", []string{"dee"})
	s.mu.Lock()
	s.sessions[sess.ID].Turns = []Turn{{Kitty: "d1", Working: true}}
	s.mu.Unlock()
	if got := s.standIn(ana); got != nil {
		t.Errorf("busy kitty picked: %v", got)
	}
	// Attended, the person is asked as before: no stand-in.
	s2, d2, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	d2.kitties["c1"] = &kitty.Kitty{ID: "c1", Name: "cy", Desktop: true}
	d2.brains["c1"] = &scripted{replies: []string{"never asked"}}
	a2, _ := d2.Get("ana")
	a2.Limits = kitty.Limits{MaxRounds: 1, Attended: true}
	c, _ := s2.Create("", []string{"ana"})
	_, _ = s2.Send(context.Background(), c.ID, "go")
	if turn := waitDone(t, s2, c.ID).Turns[1]; !turn.NeedsHelp {
		t.Errorf("attended: %+v", turn)
	}
}

func TestStandInIsTheMostAlikeAndCappedADay(t *testing.T) {
	s, d, _ := deskService(t)
	ana, _ := d.Get("ana") // connectors: echo
	ana.Soul.Skills = []string{"Research", "writing"}
	// cy has nothing in common; dee shares a skill; eve may use every connector and is on.
	d.kitties["c1"] = &kitty.Kitty{ID: "c1", Name: "cy", Desktop: true, Connectors: []string{"other"}}
	d.kitties["d1"] = &kitty.Kitty{ID: "d1", Name: "dee", Desktop: true, Connectors: []string{"other"}, Soul: soul.Soul{Skills: []string{" research "}}}
	d.kitties["e1"] = &kitty.Kitty{ID: "e1", Name: "eve", Desktop: true, Status: kitty.StatusRunning}
	if got := s.standIn(ana); got == nil || got.Name != "eve" {
		t.Fatalf("most alike = %v", got)
	}
	if likeness(ana, d.kitties["d1"]) != 1 || likeness(ana, d.kitties["e1"]) != 3 || likeness(ana, d.kitties["c1"]) != 0 {
		t.Error("likeness")
	}
	// Three a day by default (one already taken above), then nobody; a new day starts again.
	now := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.standIns = nil
	for i := 0; i < DefaultStandIns; i++ {
		if s.standIn(ana) == nil {
			t.Fatalf("stand-in %d refused", i+1)
		}
	}
	if s.standIn(ana) != nil {
		t.Error("past the day's stand-ins")
	}
	now = now.Add(24 * time.Hour)
	if s.standIn(ana) == nil {
		t.Error("a new day")
	}
	// From the settings: off means none.
	s.SetStandIns(func() int { return 0 })
	now = now.Add(24 * time.Hour)
	if s.standIn(ana) != nil {
		t.Error("off in the settings")
	}
}
