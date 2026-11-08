// Package tidy keeps what grows by itself from filling the disk, and tells what the gateway's
// data takes.
//
// WHAT GOES BY ITSELF, AND WHAT NEVER DOES.
//
// The screenshots of the desktop tasks are swept by the retention of Settings › Preferences (by
// age, then the oldest until they fit their cap), and the desktop images left behind by an update
// are pruned, every few hours and when the person asks. The chats, their attached files, the
// kitties' homes, workspaces and Shared folders and the backups of their sessions are the person's:
// they are only counted, so Settings can show where the disk goes.
package tidy

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
)

// Chats is what the janitor needs of the chats.
type Chats interface {
	DiskUsage() chat.DiskUsage
	SweepShots(before time.Time, maxBytes int64) (files int, freed int64)
}

// Settings keeps the retention.
type Settings interface {
	Retention() config.Retention
	SetRetention(r config.Retention) error
}

// Images deletes the desktop images nothing uses any more, and says what that freed.
type Images interface {
	PruneImages(ctx context.Context) (string, error)
}

// Janitor sweeps on a schedule and on request.
type Janitor struct {
	// Dir is the gateway's data folder.
	Dir      string
	Chats    Chats
	Settings Settings
	// Images is nil when there is no container runtime.
	Images Images
	Now    func() time.Time

	mu   sync.Mutex
	last *Result
}

// Area is what one kind of data takes.
type Area struct {
	Key   string `json:"key"`
	Bytes int64  `json:"bytes"`
	// Count is how many items there are, when it is told (the screenshots).
	Count int `json:"count,omitempty"`
}

// Usage is what the data takes, the retention, and the last sweep.
type Usage struct {
	Dir   string `json:"dir"`
	Areas []Area `json:"areas"`
	Total int64  `json:"total"`
	// Free is what is left on the disk of Dir, 0 when it cannot be told.
	Free      int64            `json:"free,omitempty"`
	Retention config.Retention `json:"retention"`
	// Defaults are what a zero retention means.
	Defaults config.Retention `json:"defaults"`
	Last     *Result          `json:"last,omitempty"`
}

// Result is what a sweep did.
type Result struct {
	At    time.Time `json:"at"`
	Shots int       `json:"shots"`
	Freed int64     `json:"freed"`
	// Images is what pruning the old desktop images freed, as the runtime says it.
	Images string `json:"images,omitempty"`
	Error  string `json:"error,omitempty"`
}

// The areas of the data folder, by their folder or file.
const (
	AreaShots       = "screenshots"
	AreaAttachments = "attachments"
	AreaChats       = "chats"
	AreaHomes       = "homes"
	AreaWork        = "work"
	AreaShared      = "shared"
	AreaBackups     = "backups"
	AreaJournal     = "journal"
	AreaWhatsApp    = "whatsapp"
	AreaLogs        = "logs"
	AreaOther       = "other"
)

// folders maps a folder of the data to its area; the chats are told by kind, apart.
var folders = map[string]string{
	"homes": AreaHomes, "work": AreaWork, "shared": AreaShared, "backups": AreaBackups,
	"journal": AreaJournal, "whatsapp": AreaWhatsApp,
}

func (j *Janitor) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Usage tells what the data takes on disk. It walks the folders, so it is asked for, not polled.
func (j *Janitor) Usage() Usage {
	sizes := map[string]int64{}
	u := Usage{Dir: j.Dir, Defaults: config.Retention{ShotDays: config.DefaultShotDays, ShotsMB: config.DefaultShotsMB}}
	entries, _ := os.ReadDir(j.Dir)
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == "chats" && e.IsDir():
			continue
		case !e.IsDir():
			info, err := e.Info()
			if err != nil {
				continue
			}
			if strings.HasSuffix(name, ".log") || strings.Contains(name, ".log.") {
				sizes[AreaLogs] += info.Size()
			} else {
				sizes[AreaOther] += info.Size()
			}
		case folders[name] != "":
			sizes[folders[name]] += dirSize(filepath.Join(j.Dir, name))
		default:
			sizes[AreaOther] += dirSize(filepath.Join(j.Dir, name))
		}
	}
	var shots int
	if j.Chats != nil {
		c := j.Chats.DiskUsage()
		sizes[AreaShots], sizes[AreaAttachments], sizes[AreaChats] = c.Shots, c.Files, c.History
		shots = c.ShotCount
	}
	for key, n := range sizes {
		a := Area{Key: key, Bytes: n}
		if key == AreaShots {
			a.Count = shots
		}
		u.Areas = append(u.Areas, a)
		u.Total += n
	}
	sort.Slice(u.Areas, func(a, b int) bool {
		if u.Areas[a].Bytes != u.Areas[b].Bytes {
			return u.Areas[a].Bytes > u.Areas[b].Bytes
		}
		return u.Areas[a].Key < u.Areas[b].Key
	})
	u.Free = freeBytes(j.Dir)
	if j.Settings != nil {
		u.Retention = j.Settings.Retention()
	}
	j.mu.Lock()
	if j.last != nil {
		last := *j.last
		u.Last = &last
	}
	j.mu.Unlock()
	return u
}

// SetRetention changes how much of the screenshots is kept.
func (j *Janitor) SetRetention(r config.Retention) error {
	if j.Settings == nil {
		return errors.New("this gateway cannot keep a retention")
	}
	return j.Settings.SetRetention(r)
}

// Clean sweeps the screenshots by the retention and prunes the old desktop images now.
func (j *Janitor) Clean(ctx context.Context) Result {
	res := Result{At: j.now()}
	if j.Chats != nil {
		var r config.Retention
		if j.Settings != nil {
			r = j.Settings.Retention()
		}
		var before time.Time
		if age := r.ShotAge(); age > 0 {
			before = res.At.Add(-age)
		}
		res.Shots, res.Freed = j.Chats.SweepShots(before, r.ShotBytes())
	}
	if j.Images != nil {
		freed, err := j.Images.PruneImages(ctx)
		res.Images = freed
		if err != nil {
			res.Error = err.Error()
		}
	}
	j.mu.Lock()
	j.last = &res
	j.mu.Unlock()
	return res
}

// Run cleans shortly after the start and then every `every`, until ctx ends.
func (j *Janitor) Run(ctx context.Context, first, every time.Duration, logf func(format string, args ...any)) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		res := j.Clean(ctx)
		if logf == nil {
			continue
		}
		if res.Shots > 0 {
			logf("tidy: deleted %d old screenshots (%s)", res.Shots, Bytes(res.Freed))
		}
		if res.Images != "" && res.Images != "0B" {
			logf("tidy: deleted old desktop images (%s)", res.Images)
		}
		if res.Error != "" {
			logf("tidy: %s", res.Error)
		}
	}
}

// sizeCap bounds how many entries a size counts: a huge folder is told as at least its size.
var sizeCap = 500000

// dirSize adds up the sizes of the files under dir, without following links.
func dirSize(dir string) int64 {
	var total int64
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > sizeCap {
			return filepath.SkipAll
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Bytes writes a size for a log line: 1.5 GB, 320 MB, 12 KB.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB"}[exp]
	if v >= 10 {
		return strconv.FormatInt(int64(v+0.5), 10) + " " + suffix
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + suffix
}
