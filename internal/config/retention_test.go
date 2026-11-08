package config

import (
	"testing"
	"time"
)

func TestRetention(t *testing.T) {
	var r Retention
	if r.Days() != DefaultShotDays || r.MB() != DefaultShotsMB || r.Validate() != nil {
		t.Errorf("zero = %d days %d MB", r.Days(), r.MB())
	}
	if r.ShotAge() != 30*24*time.Hour || r.ShotBytes() != 2048<<20 {
		t.Errorf("zero = %v %d", r.ShotAge(), r.ShotBytes())
	}
	r = Retention{ShotDays: -1, ShotsMB: -1}
	if r.Days() != 0 || r.MB() != 0 || r.ShotAge() != 0 || r.ShotBytes() != 0 || r.Validate() != nil {
		t.Errorf("forever = %d %d", r.Days(), r.MB())
	}
	r = Retention{ShotDays: 7, ShotsMB: 100}
	if r.Days() != 7 || r.MB() != 100 {
		t.Errorf("set = %d %d", r.Days(), r.MB())
	}
	for _, bad := range []Retention{{ShotDays: -2}, {ShotDays: 4000}, {ShotsMB: -5}, {ShotsMB: 2 << 20}} {
		if bad.Validate() == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	c := Defaults()
	c.Retention.ShotDays = -9
	if c.Validate() == nil {
		t.Error("the configuration took a bad retention")
	}
}
