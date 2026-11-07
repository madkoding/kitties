package kitty

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// THE MEMORY OF A KITTY'S MACHINE.
//
// A kitty's machine has so much RAM and swap (the container's limits, set per kitty in the
// settings or for all of them by default). The kitty is told how much is used and how much is
// free, so it does not open browser tabs or programs that do not fit: before `open` and `launch`
// the gateway looks, and refuses when what is left is too little.

// The limits a machine may be given, in MB.
const (
	MinMemoryMB = 512
	MaxMemoryMB = 128 * 1024
	MaxSwapMB   = 64 * 1024
	// DefaultSwapMB is the swap a machine has besides its RAM when nothing says otherwise.
	DefaultSwapMB = 1024
)

// The least free memory, in MB, a tool needs to start: a browser tab takes some 150 to 300 MB,
// a program with windows more.
const (
	OpenNeedsMB   = 250
	LaunchNeedsMB = 400
)

// ValidMemory checks the RAM and swap of a machine, in MB.
func ValidMemory(memoryMB, swapMB int) error {
	if memoryMB < MinMemoryMB || memoryMB > MaxMemoryMB {
		return fmt.Errorf("memory_mb is between %d and %d", MinMemoryMB, MaxMemoryMB)
	}
	if swapMB < 0 || swapMB > MaxSwapMB {
		return fmt.Errorf("swap_mb is between 0 (no swap) and %d", MaxSwapMB)
	}
	return nil
}

// Process is a program by the memory it holds: all its processes together (a browser has one
// per tab).
type Process struct {
	Name  string `json:"name"`
	MB    int    `json:"mb"`
	Count int    `json:"count"`
}

// Memory is what a machine has and uses now, in MB.
type Memory struct {
	TotalMB     int `json:"total_mb"`
	UsedMB      int `json:"used_mb"`
	AvailableMB int `json:"available_mb"`
	SwapTotalMB int `json:"swap_total_mb"`
	SwapUsedMB  int `json:"swap_used_mb"`
	// Top are the programs that hold the most, biggest first.
	Top []Process `json:"top,omitempty"`
}

// MemoryScript prints what ParseMemory reads: the container's limits and use (cgroup v2, else
// v1), what the kernel says (/proc/meminfo, the host's when nothing limits the container) and
// the memory of each process. It needs only sh, cat, grep and ps.
const MemoryScript = `cd /sys/fs/cgroup 2>/dev/null || exit 0
for f in memory.max memory.current memory.swap.max memory.swap.current memory/memory.limit_in_bytes memory/memory.usage_in_bytes memory/memory.memsw.limit_in_bytes memory/memory.memsw.usage_in_bytes; do
  [ -r "$f" ] && echo "$f=$(cat "$f")"
done
grep -h -E '^(total_)?inactive_file ' memory.stat memory/memory.stat 2>/dev/null
grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo
echo '--- ps'
ps -eo rss=,comm= 2>/dev/null`

// MemoryArgv is the command that prints what ParseMemory reads.
var MemoryArgv = []string{"sh", "-c", MemoryScript}

// noLimit is what a cgroup says when nothing limits it (v1 writes a huge number instead of max).
const noLimit = int64(1) << 60

// ParseMemory reads the output of MemoryScript. ok is false when it says nothing of use.
func ParseMemory(out string) (Memory, bool) {
	vals := map[string]int64{}
	procs := map[string]*Process{}
	inPS := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "--- ps" {
			inPS = true
			continue
		}
		if inPS {
			rss, name, found := strings.Cut(line, " ")
			kb, err := strconv.ParseInt(rss, 10, 64)
			if !found || err != nil {
				continue
			}
			name = strings.TrimSpace(name)
			p := procs[name]
			if p == nil {
				p = &Process{Name: name}
				procs[name] = p
			}
			p.MB += int(kb / 1024)
			p.Count++
			continue
		}
		var key, val string
		if k, v, found := strings.Cut(line, "="); found {
			key, val = k, v
		} else if k, v, found := strings.Cut(line, ":"); found {
			key, val = k, strings.TrimSuffix(strings.TrimSpace(v), " kB")
			if n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
				vals[key] = n * 1024
			}
			continue
		} else if k, v, found := strings.Cut(line, " "); found {
			key, val = strings.TrimPrefix(k, "total_"), v
		}
		val = strings.TrimSpace(val)
		if val == "max" {
			vals[key] = noLimit
			continue
		}
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			vals[key] = n
		}
	}
	const mb = 1024 * 1024
	hostTotal, hostAvail := vals["MemTotal"], vals["MemAvailable"]
	limit, used, v2 := vals["memory.max"], vals["memory.current"], true
	if _, ok := vals["memory.current"]; !ok {
		limit, used, v2 = vals["memory/memory.limit_in_bytes"], vals["memory/memory.usage_in_bytes"], false
	}
	if used == 0 && hostTotal == 0 {
		return Memory{}, false
	}
	// The page cache of files not read lately is given back when it is needed: it is not used.
	used = max(used-vals["inactive_file"], 0)
	m := Memory{}
	switch {
	case limit > 0 && limit < noLimit && (hostTotal == 0 || limit < hostTotal):
		m.TotalMB, m.UsedMB = int(limit/mb), int(used/mb)
		m.AvailableMB = max(m.TotalMB-m.UsedMB, 0)
		if hostAvail > 0 {
			m.AvailableMB = min(m.AvailableMB, int(hostAvail/mb))
		}
	default:
		m.TotalMB, m.AvailableMB = int(hostTotal/mb), int(hostAvail/mb)
		m.UsedMB = max(m.TotalMB-m.AvailableMB, 0)
	}
	hostSwap, hostSwapUsed := vals["SwapTotal"], max(vals["SwapTotal"]-vals["SwapFree"], 0)
	swapLimit, swapUsed := hostSwap, hostSwapUsed
	if both := vals["memory/memory.memsw.limit_in_bytes"]; !v2 && both > 0 && both < noLimit && limit > 0 {
		swapLimit = max(both-limit, 0)
		swapUsed = max(vals["memory/memory.memsw.usage_in_bytes"]-vals["memory/memory.usage_in_bytes"], 0)
	} else if sm, ok := vals["memory.swap.max"]; v2 && ok && sm < noLimit {
		swapLimit, swapUsed = sm, vals["memory.swap.current"]
	} else if _, ok := vals["memory.swap.current"]; v2 && ok {
		swapUsed = vals["memory.swap.current"]
	}
	if hostSwap > 0 && swapLimit > hostSwap {
		swapLimit = hostSwap
	}
	m.SwapTotalMB, m.SwapUsedMB = int(swapLimit/mb), int(min(swapUsed, swapLimit)/mb)
	for _, p := range procs {
		if p.MB > 0 {
			m.Top = append(m.Top, *p)
		}
	}
	sort.Slice(m.Top, func(i, j int) bool {
		if m.Top[i].MB != m.Top[j].MB {
			return m.Top[i].MB > m.Top[j].MB
		}
		return m.Top[i].Name < m.Top[j].Name
	})
	if len(m.Top) > 5 {
		m.Top = m.Top[:5]
	}
	return m, true
}

// MBText is an amount of memory for a person or a kitty: MB under a gigabyte, GB above.
func MBText(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d MB", n)
	}
	return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " GB"
}

// Describe says the memory in a sentence, for the kitty.
func (m Memory) Describe() string {
	out := fmt.Sprintf("%s of %s RAM used, %s free", MBText(m.UsedMB), MBText(m.TotalMB), MBText(m.AvailableMB))
	if m.SwapTotalMB > 0 {
		out += fmt.Sprintf("; swap %s of %s used", MBText(m.SwapUsedMB), MBText(m.SwapTotalMB))
	} else {
		out += "; no swap"
	}
	return out
}

// TopText names the programs that hold the most memory, for the kitty.
func (m Memory) TopText() string {
	parts := make([]string, 0, len(m.Top))
	for _, p := range m.Top {
		s := p.Name + " " + MBText(p.MB)
		if p.Count > 1 {
			s += fmt.Sprintf(" (%d processes)", p.Count)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// Low reports whether the free memory is under what a tool needs.
func (m Memory) Low(needMB int) bool { return m.TotalMB > 0 && m.AvailableMB < needMB }

// NotEnough is the error a tool gets when the machine has too little memory left for it.
func (m Memory) NotEnough(what string, needMB int) error {
	return fmt.Errorf("not enough memory to %s: %s free, about %s needed (%s). Close browser tabs (key ctrl+w) "+
		"or programs you no longer need and try again; if the task truly needs more, tell the person they can give "+
		"your machine more RAM or swap in Settings › Machines", what, MBText(m.AvailableMB), MBText(needMB), m.Describe())
}
