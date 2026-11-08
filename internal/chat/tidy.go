package chat

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// WHAT THE CHATS KEEP ON DISK, AND WHAT GOES BY ITSELF.
//
// A chat is its JSON (<dir>/<id>.json), the files the person attached (<dir>/<id>/files) and the
// screenshots its desktop tasks took, one after each step (<dir>/<id>/<turn>-<step>.png). The
// first two are the person's and stay until the chat is deleted; the screenshots are the film of
// work already done and pile up fastest, so SweepShots deletes the old ones (the retention in
// Settings › Preferences): their steps stay in the chat, without the picture.

// DiskUsage is what the chats take on disk, by kind, in bytes.
type DiskUsage struct {
	History int64 `json:"history"`
	Files   int64 `json:"files"`
	Shots   int64 `json:"shots"`
	// ShotCount is how many screenshots there are.
	ShotCount int `json:"shot_count"`
}

// shotFile is a screenshot on disk.
type shotFile struct {
	chat, name, path string
	mod              time.Time
	size             int64
}

// scan walks the chats' folder once: what each kind takes, and every screenshot.
func (s *Service) scan() (DiskUsage, []shotFile) {
	var u DiskUsage
	var shots []shotFile
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return u, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			if info, err := e.Info(); err == nil {
				u.History += info.Size()
			}
			continue
		}
		id := e.Name()
		_ = filepath.WalkDir(filepath.Join(s.dir, id), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if filepath.Dir(path) == filepath.Join(s.dir, id) && shotName.MatchString(d.Name()) {
				u.Shots += info.Size()
				u.ShotCount++
				shots = append(shots, shotFile{chat: id, name: d.Name(), path: path, mod: info.ModTime(), size: info.Size()})
				return nil
			}
			u.Files += info.Size()
			return nil
		})
	}
	return u, shots
}

// DiskUsage tells what the chats take on disk.
func (s *Service) DiskUsage() DiskUsage {
	u, _ := s.scan()
	return u
}

// SweepShots deletes the screenshots taken before `before` (zero: none for their age) and then
// the oldest ones until all of them take at most maxBytes (0: no cap). The screenshots of a task
// still at work are never touched. The steps keep their place in the chat, without a picture. It
// returns how many files went and the bytes they took.
func (s *Service) SweepShots(before time.Time, maxBytes int64) (files int, freed int64) {
	_, shots := s.scan()
	if len(shots) == 0 {
		return 0, 0
	}
	busy := s.workingShots()
	sort.Slice(shots, func(i, j int) bool { return shots[i].mod.Before(shots[j].mod) })
	var total int64
	for _, f := range shots {
		total += f.size
	}
	gone := map[string]map[string]bool{}
	for _, f := range shots {
		old := !before.IsZero() && f.mod.Before(before)
		over := maxBytes > 0 && total > maxBytes
		if !old && !over {
			// Sorted oldest first: none after this one is older, and the cap is met.
			break
		}
		if busy[f.chat+"/"+f.name] {
			continue
		}
		if err := os.Remove(f.path); err != nil {
			continue
		}
		total -= f.size
		files++
		freed += f.size
		if gone[f.chat] == nil {
			gone[f.chat] = map[string]bool{}
		}
		gone[f.chat][f.name] = true
	}
	s.forgetShots(gone)
	return files, freed
}

// workingShots are the screenshots of the turns still at work, as "<chat>/<name>".
func (s *Service) workingShots() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	for id, sess := range s.sessions {
		for _, t := range sess.Turns {
			if !t.Working {
				continue
			}
			for _, st := range t.Steps {
				if st.Shot != "" {
					out[id+"/"+st.Shot] = true
				}
			}
		}
	}
	return out
}

// forgetShots takes the deleted screenshots out of their steps, so the page does not ask for them.
func (s *Service) forgetShots(gone map[string]map[string]bool) {
	if len(gone) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, names := range gone {
		sess, ok := s.sessions[id]
		if !ok {
			continue
		}
		changed := false
		for i := range sess.Turns {
			t := &sess.Turns[i]
			var steps []Step
			for j, st := range t.Steps {
				if st.Shot == "" || !names[st.Shot] {
					continue
				}
				if steps == nil {
					// A copy, as markShot does: a reader holding the steps never sees them change.
					steps = append([]Step(nil), t.Steps...)
				}
				steps[j].Shot = ""
			}
			if steps != nil {
				t.Steps = steps
				changed = true
			}
		}
		if changed {
			_ = s.save(sess)
		}
	}
}
