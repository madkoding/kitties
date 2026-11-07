package kitty

import "testing"

func TestTaskLimits(t *testing.T) {
	r, z := 120, 0
	tl := TaskLimits{MaxRounds: &r, MaxTokens: &z}
	if tl.Empty() || !(TaskLimits{}).Empty() {
		t.Error("empty")
	}
	if err := tl.Validate(); err != nil {
		t.Fatal(err)
	}
	got := tl.Apply(Limits{MaxRounds: 5, MaxTokens: 100, MaxSeconds: 60})
	if got.MaxRounds != 120 || got.MaxTokens != 0 || got.MaxSeconds != 60 {
		t.Errorf("apply = %+v", got)
	}
	if d := tl.Describe(Limits{MaxRounds: 5, MaxTokens: 100}); d != "rounds 5 → 120, tokens 100 → default" {
		t.Errorf("describe = %q", d)
	}
	neg := -1
	if err := (TaskLimits{MaxSeconds: &neg}).Validate(); err == nil {
		t.Error("negative")
	}
}
