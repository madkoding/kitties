package config

import (
	"errors"
	"time"
)

// Retention is how much of what grows by itself the gateway keeps on disk.
//
//	retention:
//	  shot_days: 30   # screenshots of the desktop tasks older than this are deleted; -1: kept forever
//	  shots_mb: 2048  # the most all those screenshots may take together, oldest go first; -1: no cap
//
// Zero takes the defaults. Chats, their attached files and the kitties' own folders are the
// person's and are never deleted by it.
type Retention struct {
	ShotDays int `yaml:"shot_days" json:"shot_days"`
	ShotsMB  int `yaml:"shots_mb" json:"shots_mb"`
}

// Defaults of Retention.
const (
	DefaultShotDays = 30
	DefaultShotsMB  = 2048
	maxShotDays     = 3650
	maxShotsMB      = 1 << 20
)

// Validate refuses a retention out of range.
func (r Retention) Validate() error {
	if r.ShotDays < -1 || r.ShotDays > maxShotDays {
		return errors.New("retention.shot_days is 1 to 3650 days (0: the default, -1: forever)")
	}
	if r.ShotsMB < -1 || r.ShotsMB > maxShotsMB {
		return errors.New("retention.shots_mb is 1 to 1048576 MB (0: the default, -1: no cap)")
	}
	return nil
}

// Days is how many days a screenshot is kept, 0 for forever.
func (r Retention) Days() int {
	switch {
	case r.ShotDays < 0:
		return 0
	case r.ShotDays == 0:
		return DefaultShotDays
	}
	return r.ShotDays
}

// MB is the most the screenshots take together, 0 for no cap.
func (r Retention) MB() int {
	switch {
	case r.ShotsMB < 0:
		return 0
	case r.ShotsMB == 0:
		return DefaultShotsMB
	}
	return r.ShotsMB
}

// ShotAge is how long a screenshot is kept, 0 for forever.
func (r Retention) ShotAge() time.Duration { return time.Duration(r.Days()) * 24 * time.Hour }

// ShotBytes is the most the screenshots take together, 0 for no cap.
func (r Retention) ShotBytes() int64 { return int64(r.MB()) << 20 }
