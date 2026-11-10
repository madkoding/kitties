package kitty

import (
	"encoding/json"
	"time"
)

// BrowserVisitsFile is where kitty-refresh writes what its last pass over the browser's tabs did:
// counts only, never an address or what a page says.
const BrowserVisitsFile = "/dev/shm/kitty-refresh.json"

// BrowserVisitsArgv prints it (nothing when no pass has run yet).
var BrowserVisitsArgv = []string{"cat", BrowserVisitsFile}

// BrowserVisits is the last pass of kitty-refresh over the open tabs: when, and how many it
// reloaded, visited quietly (someone was using them), kept (they asked before being left) or
// skipped (they did not answer).
type BrowserVisits struct {
	At       time.Time `json:"at"`
	Tabs     int       `json:"tabs"`
	Reloaded int       `json:"reloaded"`
	Quiet    int       `json:"quiet"`
	Kept     int       `json:"kept"`
	Skipped  int       `json:"skipped"`
}

// ParseBrowserVisits reads what kitty-refresh wrote; ok is false when nothing is there yet.
func ParseBrowserVisits(out string) (BrowserVisits, bool) {
	var raw struct {
		At       float64 `json:"at"`
		Tabs     int     `json:"tabs"`
		Reloaded int     `json:"reloaded"`
		Quiet    int     `json:"quiet"`
		Kept     int     `json:"kept"`
		Skipped  int     `json:"skipped"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil || raw.At <= 0 {
		return BrowserVisits{}, false
	}
	sec := int64(raw.At)
	return BrowserVisits{At: time.Unix(sec, 0).UTC(), Tabs: raw.Tabs, Reloaded: raw.Reloaded,
		Quiet: raw.Quiet, Kept: raw.Kept, Skipped: raw.Skipped}, true
}
