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

func TestLeadLimitsSetting(t *testing.T) {
	for v, ask := range map[string]bool{"": false, "auto": false, "ask": true, " ASK ": true} {
		a := Autonomy{LeadLimits: v}
		if err := a.Validate(); err != nil || a.LeadLimitsAsk() != ask {
			t.Errorf("%q: %v %v", v, err, a.LeadLimitsAsk())
		}
	}
	if err := (Autonomy{LeadLimits: "maybe"}).Validate(); err == nil {
		t.Error("maybe is not a setting")
	}
}

func TestParallelAndSummarySettings(t *testing.T) {
	for v, want := range map[string]int{"": 4, "off": 1, "0": 1, "1": 1, " 8 ": 8, "16": 16} {
		a := Autonomy{ParallelHandOvers: v}
		if err := a.Validate(); err != nil || a.ParallelOr(4) != want {
			t.Errorf("%q: %v %d", v, err, a.ParallelOr(4))
		}
	}
	for _, bad := range []string{"17", "-2", "many"} {
		if err := (Autonomy{ParallelHandOvers: bad}).Validate(); err == nil {
			t.Errorf("parallel %q was accepted", bad)
		}
	}
	if h, m, ok := (Autonomy{LeadSummary: "18:30"}).LeadSummaryAt(); !ok || h != 18 || m != 30 {
		t.Errorf("summary at %d:%d %v", h, m, ok)
	}
	for _, none := range []string{"", "off"} {
		a := Autonomy{LeadSummary: none}
		if _, _, ok := a.LeadSummaryAt(); ok || a.Validate() != nil {
			t.Errorf("%q: a summary", none)
		}
	}
	if err := (Autonomy{LeadSummary: "6pm"}).Validate(); err == nil {
		t.Error("6pm was accepted")
	}
	if _, _, ok := (Autonomy{LeadSummary: "6pm"}).LeadSummaryAt(); ok {
		t.Error("6pm is a time")
	}
}

func TestHandOverKeepSetting(t *testing.T) {
	for v, want := range map[string]time.Duration{"": 72 * time.Hour, "off": 0, "none": 0, "24h": 24 * time.Hour, " 2160h ": 2160 * time.Hour} {
		a := Autonomy{HandOverKeep: v}
		if err := a.Validate(); err != nil || a.HandOverKeepOr(72*time.Hour) != want {
			t.Errorf("%q: %v %v", v, err, a.HandOverKeepOr(72*time.Hour))
		}
	}
	for _, bad := range []string{"30m", "2161h", "a week"} {
		if err := (Autonomy{HandOverKeep: bad}).Validate(); err == nil {
			t.Errorf("keep %q was accepted", bad)
		}
	}
}
