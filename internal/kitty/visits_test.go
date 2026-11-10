package kitty

import "testing"

func TestParseBrowserVisits(t *testing.T) {
	v, ok := ParseBrowserVisits(`{"at": 1794000000.5, "tabs": 3, "reloaded": 1, "quiet": 2, "kept": 0, "skipped": 0}`)
	if !ok || v.At.Unix() != 1794000000 || v.Tabs != 3 || v.Reloaded != 1 || v.Quiet != 2 {
		t.Errorf("parsed = %+v %v", v, ok)
	}
	for _, bad := range []string{"", "{}", "not json", `{"at": 0}`} {
		if _, ok := ParseBrowserVisits(bad); ok {
			t.Errorf("%q taken", bad)
		}
	}
}
