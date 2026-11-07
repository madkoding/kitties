package kitty

import (
	"strings"
	"testing"
)

const meminfo = "MemTotal:       16384000 kB\nMemAvailable:    8192000 kB\nSwapTotal:       4096000 kB\nSwapFree:        3072000 kB\n"

func TestParseMemoryCgroupV2(t *testing.T) {
	out := "memory.max=2147483648\nmemory.current=1610612736\nmemory.swap.max=1073741824\nmemory.swap.current=104857600\n" +
		"inactive_file 536870912\n" + meminfo +
		"--- ps\n 409600 chromium\n 204800 chromium\n 102400 Xvfb\n bad line\n x chromium\n 512 tiny\n 100 sh\n 2048 a\n 2048 b\n 2048 c\n"
	m, ok := ParseMemory(out)
	if !ok || m.TotalMB != 2048 || m.UsedMB != 1024 || m.AvailableMB != 1024 || m.SwapTotalMB != 1024 || m.SwapUsedMB != 100 {
		t.Fatalf("v2 = %+v %v", m, ok)
	}
	if len(m.Top) != 5 || m.Top[0].Name != "chromium" || m.Top[0].MB != 600 || m.Top[0].Count != 2 || m.Top[1].Name != "Xvfb" {
		t.Errorf("top = %+v", m.Top)
	}
	if !strings.Contains(m.TopText(), "chromium 600 MB (2 processes)") || !strings.Contains(m.TopText(), "Xvfb 100 MB") {
		t.Errorf("top text = %q", m.TopText())
	}
	if d := m.Describe(); d != "1.0 GB of 2.0 GB RAM used, 1.0 GB free; swap 100 MB of 1.0 GB used" {
		t.Errorf("describe = %q", d)
	}
}

func TestParseMemoryUnlimitedAndV1(t *testing.T) {
	// No limit: the host's memory and swap.
	m, ok := ParseMemory("memory.max=max\nmemory.current=1073741824\nmemory.swap.max=max\nmemory.swap.current=0\n" + meminfo)
	if !ok || m.TotalMB != 16000 || m.AvailableMB != 8000 || m.UsedMB != 8000 || m.SwapTotalMB != 4000 || m.SwapUsedMB != 0 {
		t.Errorf("unlimited = %+v %v", m, ok)
	}
	// No swap accounting: the host's swap.
	m, _ = ParseMemory("memory.max=1073741824\nmemory.current=0\n" + meminfo)
	if m.TotalMB != 1024 || m.SwapTotalMB != 4000 || m.SwapUsedMB != 1000 {
		t.Errorf("no swap accounting = %+v", m)
	}
	// v1 with swap accounting: memsw is RAM and swap together.
	v1 := "memory/memory.limit_in_bytes=1073741824\nmemory/memory.usage_in_bytes=536870912\n" +
		"memory/memory.memsw.limit_in_bytes=1610612736\nmemory/memory.memsw.usage_in_bytes=641728512\ntotal_inactive_file 0\n" + meminfo
	m, ok = ParseMemory(v1)
	if !ok || m.TotalMB != 1024 || m.UsedMB != 512 || m.AvailableMB != 512 || m.SwapTotalMB != 512 || m.SwapUsedMB != 100 {
		t.Errorf("v1 = %+v %v", m, ok)
	}
	// A limit bigger than the swap the host has is the host's.
	m, _ = ParseMemory("memory.max=1073741824\nmemory.current=0\nmemory.swap.max=99999999999\n" + meminfo)
	if m.SwapTotalMB != 4000 {
		t.Errorf("swap over the host's = %+v", m)
	}
	// No swap at all.
	m, _ = ParseMemory("memory.max=1073741824\nmemory.current=0\nmemory.swap.max=0\nMemTotal: 2048000 kB\nMemAvailable: 100 kB\n")
	if m.SwapTotalMB != 0 || !strings.HasSuffix(m.Describe(), "no swap") || m.AvailableMB != 0 {
		t.Errorf("no swap = %+v %q", m, m.Describe())
	}
	if _, ok := ParseMemory("nothing here\n--- ps\n"); ok {
		t.Error("an empty answer says nothing")
	}
}

func TestMemoryLowAndLimits(t *testing.T) {
	m := Memory{TotalMB: 2048, UsedMB: 1900, AvailableMB: 148}
	if !m.Low(OpenNeedsMB) || (Memory{}).Low(OpenNeedsMB) || (Memory{TotalMB: 2048, AvailableMB: 1000}).Low(LaunchNeedsMB) {
		t.Error("low")
	}
	err := m.NotEnough("open another page in the browser", OpenNeedsMB)
	if err == nil || !strings.Contains(err.Error(), "148 MB free") || !strings.Contains(err.Error(), "Settings › Machines") {
		t.Errorf("not enough = %v", err)
	}
	if MBText(512) != "512 MB" || MBText(1536) != "1.5 GB" {
		t.Error("MBText")
	}
	for _, c := range []struct {
		mem, swap int
		ok        bool
	}{{2048, 1024, true}, {512, 0, true}, {100, 0, false}, {MaxMemoryMB + 1, 0, false}, {2048, -1, false}, {2048, MaxSwapMB + 1, false}} {
		if err := ValidMemory(c.mem, c.swap); (err == nil) != c.ok {
			t.Errorf("ValidMemory(%d, %d) = %v", c.mem, c.swap, err)
		}
	}
}
