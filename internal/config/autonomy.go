package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Autonomy tunes how long a kitty keeps at a task on its own before it needs the person.
//
//	autonomy:
//	  follow_ups: 10m 30m 2h   # waits before a goal that did not finish runs again; "off": never
//	  brain_wait: 30m          # how long a task waits for a model that is down; "off": not at all
//	  stand_ins: 3             # tasks a day a kitty without a backup hands to a free one; "off": none
//
// Empty takes the defaults.
type Autonomy struct {
	FollowUps string `yaml:"follow_ups" json:"follow_ups"`
	BrainWait string `yaml:"brain_wait" json:"brain_wait"`
	StandIns  string `yaml:"stand_ins" json:"stand_ins"`
}

// maxWait is the longest single wait either setting takes.
const maxWait = 7 * 24 * time.Hour

// off reports whether a setting turns its feature off.
func off(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "off" || s == "0" || s == "none"
}

// Validate checks both settings.
func (a Autonomy) Validate() error {
	if _, err := a.followUps(nil); err != nil {
		return err
	}
	if _, err := a.brainWait(0); err != nil {
		return err
	}
	_, err := a.standIns(0)
	return err
}

// StandInsOr is how many tasks a day a kitty without a backup hands to a free kitty: def when
// unset, zero when off.
func (a Autonomy) StandInsOr(def int) int {
	n, _ := a.standIns(def)
	return n
}

func (a Autonomy) standIns(def int) (int, error) {
	switch s := strings.TrimSpace(a.StandIns); {
	case s == "":
		return def, nil
	case off(s):
		return 0, nil
	default:
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return def, fmt.Errorf("autonomy.stand_ins: %q is not a number between 1 and 100 (or off)", s)
		}
		return n, nil
	}
}

// FollowUpWaits are the waits before the follow-ups of a goal that did not finish: def when
// none are set, nothing when they are off.
func (a Autonomy) FollowUpWaits(def []time.Duration) []time.Duration {
	w, _ := a.followUps(def)
	return w
}

// BrainWaitOr is how long a task waits for a model that is down: def when unset, zero when off.
func (a Autonomy) BrainWaitOr(def time.Duration) time.Duration {
	w, _ := a.brainWait(def)
	return w
}

func (a Autonomy) followUps(def []time.Duration) ([]time.Duration, error) {
	switch s := strings.TrimSpace(a.FollowUps); {
	case s == "":
		return def, nil
	case off(s):
		return []time.Duration{}, nil
	}
	var out []time.Duration
	for _, f := range strings.FieldsFunc(a.FollowUps, func(r rune) bool { return r == ' ' || r == ',' }) {
		d, err := time.ParseDuration(f)
		if err != nil || d < time.Minute || d > maxWait {
			return def, fmt.Errorf("autonomy.follow_ups: %q is not a wait between 1m and 168h (say e.g. \"10m 30m 2h\", or off)", f)
		}
		out = append(out, d)
	}
	if len(out) > 10 {
		return def, fmt.Errorf("autonomy.follow_ups: %d follow-ups, 10 at most", len(out))
	}
	return out, nil
}

func (a Autonomy) brainWait(def time.Duration) (time.Duration, error) {
	switch s := strings.TrimSpace(a.BrainWait); {
	case s == "":
		return def, nil
	case off(s):
		return 0, nil
	default:
		d, err := time.ParseDuration(s)
		if err != nil || d < time.Minute || d > maxWait {
			return def, fmt.Errorf("autonomy.brain_wait: %q is not a wait between 1m and 168h (say e.g. 30m, or off)", s)
		}
		return d, nil
	}
}
