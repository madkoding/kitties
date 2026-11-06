package container

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// hostFiles points the zone lookups at a made-up host and gives back the old ones after the test.
func hostFiles(t *testing.T) (etc, zoneinfo string) {
	t.Helper()
	root := t.TempDir()
	etc, zoneinfo = filepath.Join(root, "etc"), filepath.Join(root, "zoneinfo")
	for _, d := range []string{etc, filepath.Join(zoneinfo, "America"), filepath.Join(zoneinfo, "Chile"), filepath.Join(zoneinfo, "posix", "America"), filepath.Join(zoneinfo, "Europe")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(zoneinfo, "UTC"), "TZif-utc")
	write(filepath.Join(zoneinfo, "America", "Santiago"), "TZif-santiago")
	write(filepath.Join(zoneinfo, "Chile", "Continental"), "TZif-santiago") // an alias, sorted first
	write(filepath.Join(zoneinfo, "posix", "America", "Lima"), "TZif-lima")
	write(filepath.Join(zoneinfo, "Europe", "Madrid"), "TZif-madrid")
	write(filepath.Join(zoneinfo, "zone1970.tab"), "# comment\nCL\t-3327-07040\tAmerica/Santiago\tmost of Chile\nbad line\n")
	oldTZ, oldLocal, oldDirs, oldNow, oldEnv, oldWin := timezoneFile, localtimeFile, zoneinfoDirs, clockNow, getEnv, windowsZoneID
	windowsZoneID = func() string { return "" }
	timezoneFile, localtimeFile = filepath.Join(etc, "timezone"), filepath.Join(etc, "localtime")
	zoneinfoDirs = []string{filepath.Join(root, "missing"), zoneinfo}
	getEnv = func(string) string { return "" }
	t.Cleanup(func() {
		timezoneFile, localtimeFile, zoneinfoDirs, clockNow, getEnv, windowsZoneID = oldTZ, oldLocal, oldDirs, oldNow, oldEnv, oldWin
	})
	return etc, zoneinfo
}

func TestHostTimezoneLooksEverywhere(t *testing.T) {
	etc, zoneinfo := hostFiles(t)
	clockNow = func() time.Time { return time.Date(2026, 11, 6, 10, 0, 0, 0, time.FixedZone("x", -3*3600)) }

	// Nothing tells it: the offset of the host right now.
	if z := HostTimezone(); z != "Etc/GMT+3" {
		t.Errorf("offset only: %q", z)
	}
	// /etc/localtime a copy (or a file mounted into the gateway's container): the canonical name.
	local := filepath.Join(etc, "localtime")
	_ = os.WriteFile(local, []byte("TZif-santiago"), 0o644)
	if z := HostTimezone(); z != "America/Santiago" {
		t.Errorf("a copy of Santiago: %q", z)
	}
	// One no table names is found by walking the folder, leaving posix/ aside.
	_ = os.WriteFile(local, []byte("TZif-madrid"), 0o644)
	if z := HostTimezone(); z != "Europe/Madrid" {
		t.Errorf("a copy of Madrid: %q", z)
	}
	_ = os.WriteFile(local, []byte("TZif-lima"), 0o644)
	if z := HostTimezone(); z != "Etc/GMT+3" {
		t.Errorf("only under posix/: %q", z)
	}
	// A link into the zone folder.
	_ = os.Remove(local)
	if err := os.Symlink(filepath.Join(zoneinfo, "Europe", "Madrid"), local); err != nil {
		t.Skipf("no links here: %v", err)
	}
	if z := HostTimezone(); z != "Europe/Madrid" {
		t.Errorf("a link: %q", z)
	}
	// /etc/timezone wins over the link, and $TZ over both; what is not a zone is passed over.
	_ = os.WriteFile(filepath.Join(etc, "timezone"), []byte("America/Santiago\n"), 0o644)
	if z := HostTimezone(); z != "America/Santiago" {
		t.Errorf("/etc/timezone: %q", z)
	}
	getEnv = func(string) string { return ":Asia/Tokyo" }
	if z := HostTimezone(); z != "Asia/Tokyo" {
		t.Errorf("$TZ: %q", z)
	}
	getEnv = func(string) string { return "not a zone; rm -rf /" }
	_ = os.WriteFile(filepath.Join(etc, "timezone"), []byte("../../etc/passwd\n"), 0o644)
	if z := HostTimezone(); z != "Europe/Madrid" {
		t.Errorf("bad $TZ and /etc/timezone: %q", z)
	}
}

func TestFixedOffset(t *testing.T) {
	for off, want := range map[int]string{0: "UTC", -3 * 3600: "Etc/GMT+3", 2 * 3600: "Etc/GMT-2", 14 * 3600: "Etc/GMT-14", 5*3600 + 1800: "<+0530>-5:30", -(9*3600 + 1800): "<-0930>9:30"} {
		got := fixedOffset(time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", off)))
		if got != want || !(fixedZone.MatchString(got) || zoneName.MatchString(got)) {
			t.Errorf("offset %d: %q, want %q", off, got, want)
		}
	}
	// A fixed zone in $TZ is taken as it is.
	hostFiles(t)
	getEnv = func(string) string { return "<+0530>-5:30" }
	if z := HostTimezone(); z != "<+0530>-5:30" {
		t.Errorf("fixed $TZ: %q", z)
	}
}

func TestSpecZone(t *testing.T) {
	hostFiles(t)
	getEnv = func(string) string { return "America/Santiago" }
	if z := (Spec{}).Zone(); z != "America/Santiago" {
		t.Errorf("host zone: %q", z)
	}
	if z := (Spec{Env: map[string]string{"TZ": "Asia/Tokyo"}}).Zone(); z != "Asia/Tokyo" {
		t.Errorf("own zone: %q", z)
	}
}

func TestZoneStale(t *testing.T) {
	m := New(Runtime{Bin: "docker"})
	ctx := context.Background()
	f := &fake{answer: func([]string) ([]byte, error) {
		return []byte("PATH=/usr/bin\nTZ=America/Santiago\nKITTY_NAME=mimi\n\n"), nil
	}}
	f.install(t, "docker")
	if stale, err := m.ZoneStale(ctx, "kitty-mimi", "America/Santiago"); err != nil || stale {
		t.Errorf("same zone: %v %v", stale, err)
	}
	if got := strings.Join(f.calls[0], " "); !strings.Contains(got, "inspect") || !strings.HasSuffix(got, "kitty-mimi") {
		t.Errorf("asked %q", got)
	}
	if stale, err := m.ZoneStale(ctx, "kitty-mimi", "Europe/Madrid"); err != nil || !stale {
		t.Errorf("moved: %v %v", stale, err)
	}
	// Unknown host zone: never stale, nothing asked.
	f.calls = nil
	if stale, err := m.ZoneStale(ctx, "kitty-mimi", ""); err != nil || stale || len(f.calls) != 0 {
		t.Errorf("no zone: %v %v %v", stale, err, f.calls)
	}
	if _, err := m.ZoneStale(ctx, "bad name;", "UTC"); err == nil {
		t.Error("a bad name was inspected")
	}
	// A container made without a zone runs in UTC, which is stale for any other.
	f.answer = func([]string) ([]byte, error) { return []byte("PATH=/usr/bin\n"), nil }
	if stale, _ := m.ZoneStale(ctx, "kitty-mimi", "America/Santiago"); !stale {
		t.Error("a container without TZ")
	}
	f.answer = func([]string) ([]byte, error) { return []byte("no such container"), errors.New("exit 1") }
	if _, err := m.ZoneStale(ctx, "kitty-mimi", "UTC"); err == nil || !strings.Contains(err.Error(), "no such container") {
		t.Errorf("failed inspect: %v", err)
	}
}

func TestHostTimezoneOnWindows(t *testing.T) {
	hostFiles(t)
	clockNow = func() time.Time { return time.Date(2026, 11, 6, 10, 0, 0, 0, time.FixedZone("x", -3*3600)) }
	for id, want := range map[string]string{
		"Pacific SA Standard Time\r\n":        "America/Santiago",
		"India Standard Time":                 "Asia/Kolkata", // CLDR's Asia/Calcutta, by its current name
		"Pacific SA Standard Time_dstoff\r\n": "Etc/GMT+3",    // summer time off: the offset
		"Mars Standard Time":                  "Etc/GMT+3",    // unknown id
		"":                                    "Etc/GMT+3",
	} {
		windowsZoneID = func() string { return id }
		if z := HostTimezone(); z != want {
			t.Errorf("%q: %q, want %q", id, z, want)
		}
	}
	// Every name in the table is one the desktop can take.
	for id, z := range windowsZones {
		if !zoneName.MatchString(z) {
			t.Errorf("%s: %q", id, z)
		}
	}
	// Off Windows, nothing is asked.
	if runtime.GOOS != "windows" && realWindowsZoneID() != "" {
		t.Error("tzutil asked off Windows")
	}
}

// realWindowsZoneID is the lookup before any test replaces it.
var realWindowsZoneID = windowsZoneID
