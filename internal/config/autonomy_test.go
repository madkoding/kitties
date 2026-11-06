package config

import (
	"strings"
	"testing"
	"time"
)

func TestAutonomy(t *testing.T) {
	def := []time.Duration{time.Minute}
	if w := (Autonomy{}).FollowUpWaits(def); len(w) != 1 || (Autonomy{}).BrainWaitOr(time.Hour) != time.Hour {
		t.Error("empty takes the defaults")
	}
	a := Autonomy{FollowUps: " 5m, 1h 2h30m", BrainWait: "45m"}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if w := a.FollowUpWaits(def); len(w) != 3 || w[0] != 5*time.Minute || w[2] != 150*time.Minute || a.BrainWaitOr(time.Hour) != 45*time.Minute {
		t.Errorf("waits = %v %v", w, a.BrainWaitOr(time.Hour))
	}
	if (Autonomy{}).StandInsOr(3) != 3 || (Autonomy{StandIns: "5"}).StandInsOr(3) != 5 || (Autonomy{StandIns: "off"}).StandInsOr(3) != 0 || (Autonomy{StandIns: "x"}).StandInsOr(3) != 3 {
		t.Error("stand-ins")
	}
	for _, o := range []string{"off", "OFF", "0", "none"} {
		a := Autonomy{FollowUps: o, BrainWait: o}
		if a.Validate() != nil || a.FollowUpWaits(def) == nil || len(a.FollowUpWaits(def)) != 0 || a.BrainWaitOr(time.Hour) != 0 {
			t.Errorf("%q turns them off", o)
		}
	}
	for _, c := range []struct {
		a    Autonomy
		want string
	}{
		{Autonomy{FollowUps: "soon"}, "autonomy.follow_ups"},
		{Autonomy{FollowUps: "10s"}, "between 1m and 168h"},
		{Autonomy{FollowUps: "1m 1m 1m 1m 1m 1m 1m 1m 1m 1m 1m"}, "10 at most"},
		{Autonomy{BrainWait: "200h"}, "autonomy.brain_wait"},
		{Autonomy{StandIns: "many"}, "autonomy.stand_ins"},
		{Autonomy{StandIns: "500"}, "between 1 and 100"},
	} {
		if err := c.a.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.a, err)
		}
		if c.a.FollowUpWaits(def)[0] != time.Minute && c.a.FollowUps != "" {
			t.Errorf("%+v: a bad value keeps the defaults", c.a)
		}
	}
	if _, err := Parse([]byte("providers:\n  p:\n    kind: ollama\nautonomy:\n  brain_wait: never\n")); err == nil || !strings.Contains(err.Error(), "autonomy.brain_wait") {
		t.Errorf("the file is checked: %v", err)
	}
	c, err := Parse([]byte("providers:\n  p:\n    kind: ollama\nautonomy:\n  follow_ups: 15m 1h\n  brain_wait: off\n"))
	if err != nil || c.Autonomy.FollowUps != "15m 1h" || c.Autonomy.BrainWait != "off" {
		t.Errorf("parse: %+v %v", c.Autonomy, err)
	}
}
