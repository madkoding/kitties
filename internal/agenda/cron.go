// Package agenda is what makes a kitty work on its own: goals it pursues on a schedule or when
// something happens, a journal of what it did and where it got stuck, and the scheduler that
// wakes it up. A chat is where the work happens; the agenda decides when.
package agenda

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed cron expression: five fields (minute, hour, day of month, month, day of
// week), each a set of allowed values.
type Schedule struct {
	minute, hour, dom, month, dow [60]bool
	// domAny and dowAny say the field was *, which matters when both days are given: cron
	// runs when either matches.
	domAny, dowAny bool
	expr           string
}

// aliases are the words a schedule may be written as.
var aliases = map[string]string{
	"@hourly":  "0 * * * *",
	"@daily":   "0 9 * * *",
	"@weekly":  "0 9 * * 1",
	"@monthly": "0 9 1 * *",
}

// Parse reads a cron expression ("*/15 * * * *", "0 9 * * 1-5", "@daily").
func Parse(expr string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if a, ok := aliases[strings.ToLower(expr)]; ok {
		expr = a
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Schedule{}, errors.New("a schedule has five fields: minute hour day month weekday (or @hourly, @daily, @weekly, @monthly)")
	}
	s := Schedule{expr: expr}
	specs := []struct {
		set      *[60]bool
		min, max int
		name     string
	}{{&s.minute, 0, 59, "minute"}, {&s.hour, 0, 23, "hour"}, {&s.dom, 1, 31, "day"}, {&s.month, 1, 12, "month"}, {&s.dow, 0, 7, "weekday"}}
	for i, sp := range specs {
		any, err := fill(sp.set, fields[i], sp.min, sp.max)
		if err != nil {
			return Schedule{}, fmt.Errorf("%s %q: %v", sp.name, fields[i], err)
		}
		switch i {
		case 2:
			s.domAny = any
		case 4:
			s.dowAny = any
			if s.dow[7] {
				s.dow[0] = true // Sunday is 0 or 7
			}
		}
	}
	return s, nil
}

// fill marks the values a field allows and reports whether it was a bare *.
func fill(set *[60]bool, field string, min, max int) (bool, error) {
	if field == "*" {
		for v := min; v <= max; v++ {
			set[v] = true
		}
		return true, nil
	}
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return false, errors.New("the step after / is a positive number")
			}
			step, part = n, part[:i]
		}
		lo, hi := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			var err error
			if lo, err = strconv.Atoi(a); err != nil {
				return false, errors.New("not a number")
			}
			if hi, err = strconv.Atoi(b); err != nil {
				return false, errors.New("not a number")
			}
		default:
			v, err := strconv.Atoi(part)
			if err != nil {
				return false, errors.New("not a number")
			}
			lo, hi = v, v
			if step > 1 {
				hi = max // "5/10" means from 5 on, every 10
			}
		}
		if lo < min || hi > max || lo > hi {
			return false, fmt.Errorf("values go from %d to %d", min, max)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return false, nil
}

// String returns the expression.
func (s Schedule) String() string { return s.expr }

// matches reports whether t is a time the schedule names.
func (s Schedule) matches(t time.Time) bool {
	if !s.minute[t.Minute()] || !s.hour[t.Hour()] || !s.month[int(t.Month())] {
		return false
	}
	dom, dow := s.dom[t.Day()], s.dow[int(t.Weekday())]
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	default:
		return dom || dow
	}
}

// Next is the first time after t the schedule names, or the zero time when none comes within
// a year (a February 30th, say).
func (s Schedule) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(1, 0, 1)
	for t.Before(end) {
		if !s.month[int(t.Month())] {
			// Skip to the first of the next month.
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !s.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if s.minute[t.Minute()] {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s Schedule) dayMatches(t time.Time) bool {
	dom, dow := s.dom[t.Day()], s.dow[int(t.Weekday())]
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	default:
		return dom || dow
	}
}
