package agenda

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseAndNext(t *testing.T) {
	cases := []struct {
		expr, from, want string
	}{
		{"* * * * *", "2026-03-10 10:00", "2026-03-10 10:01"},
		{"*/15 * * * *", "2026-03-10 10:00", "2026-03-10 10:15"},
		{"*/15 * * * *", "2026-03-10 10:50", "2026-03-10 11:00"},
		{"0 9 * * 1-5", "2026-03-13 10:00", "2026-03-16 09:00"}, // Friday after 9 → Monday
		{"0 9 * * 1-5", "2026-03-13 08:30", "2026-03-13 09:00"},
		{"30 18 * * 0", "2026-03-10 10:00", "2026-03-15 18:30"}, // Sunday
		{"30 18 * * 7", "2026-03-10 10:00", "2026-03-15 18:30"}, // Sunday as 7
		{"0 0 1 * *", "2026-03-10 10:00", "2026-04-01 00:00"},
		{"0 12 15 6 *", "2026-03-10 10:00", "2026-06-15 12:00"},
		{"0 12 15 * 1", "2026-03-10 10:00", "2026-03-15 12:00"}, // day OR weekday: the 15th comes first
		{"0 12 15 * 1", "2026-03-15 13:00", "2026-03-16 12:00"}, // then Monday
		{"5,35 8-10 * * *", "2026-03-10 09:35", "2026-03-10 10:05"},
		{"5/20 * * * *", "2026-03-10 09:00", "2026-03-10 09:05"},
		{"5/20 * * * *", "2026-03-10 09:06", "2026-03-10 09:25"},
		{"0-10/5 9 * * *", "2026-03-10 09:03", "2026-03-10 09:05"},
		{"@hourly", "2026-03-10 09:03", "2026-03-10 10:00"},
		{"@daily", "2026-03-10 09:03", "2026-03-11 09:00"},
		{"@weekly", "2026-03-10 09:03", "2026-03-16 09:00"},
		{"@monthly", "2026-03-10 09:03", "2026-04-01 09:00"},
		{"0 9 29 2 *", "2026-03-10 09:03", ""}, // no Feb 29 within a year
	}
	for _, tc := range cases {
		s, err := Parse(tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		got := s.Next(at(tc.from))
		if tc.want == "" {
			if !got.IsZero() {
				t.Errorf("%s from %s: want none, got %s", tc.expr, tc.from, got)
			}
			continue
		}
		if got.IsZero() || got.Format("2006-01-02 15:04") != tc.want {
			t.Errorf("%s from %s: got %s, want %s", tc.expr, tc.from, got.Format("2006-01-02 15:04"), tc.want)
		}
		if !s.matches(got) {
			t.Errorf("%s: Next returned a time it does not match", tc.expr)
		}
	}
	if s, _ := Parse("  @DAILY "); s.String() != "0 9 * * *" {
		t.Errorf("alias: %q", s.String())
	}
}

func TestParseRefuses(t *testing.T) {
	for _, bad := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "a * * * *", "*/0 * * * *", "*/x * * * *", "5-3 * * * *", "1-x * * * *", "x-1 * * * *", "@yearly"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestMatches(t *testing.T) {
	s, _ := Parse("0 9 * * 1-5")
	if !s.matches(at("2026-03-16 09:00")) || s.matches(at("2026-03-15 09:00")) || s.matches(at("2026-03-16 09:01")) {
		t.Error("weekdays at nine")
	}
	both, _ := Parse("0 9 15 * 1")
	if !both.matches(at("2026-03-15 09:00")) || !both.matches(at("2026-03-16 09:00")) || both.matches(at("2026-03-17 09:00")) {
		t.Error("day or weekday")
	}
	dom, _ := Parse("0 9 15 * *")
	if !dom.matches(at("2026-03-15 09:00")) || dom.matches(at("2026-03-16 09:00")) {
		t.Error("day only")
	}
	dow, _ := Parse("0 9 * * 1")
	if !dow.matches(at("2026-03-16 09:00")) || dow.matches(at("2026-03-15 09:00")) {
		t.Error("weekday only")
	}
	june, _ := Parse("0 9 * 6 *")
	if june.matches(at("2026-03-16 09:00")) {
		t.Error("month")
	}
}
