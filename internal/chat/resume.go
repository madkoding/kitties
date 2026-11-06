package chat

import (
	"context"
	"time"
)

// GOING ON AFTER A DAY'S BUDGET.
//
// A kitty that spends its day's tokens stops in the middle of what it was doing: the turn is
// parked. When its tokens are back (the next day, or the person raised or removed the limit)
// it goes on by itself from where it was, as if the person had pressed Continue.

// resumeText is what the parked kitty is told when its tokens are back.
const resumeText = "Your tokens for the day are back. Continue where you left off, without repeating what is done."

// ResumeParked wakes up the kitties parked on their day's tokens that have tokens again, and
// returns the chats it woke up. Each one goes on in the background.
func (s *Service) ResumeParked(ctx context.Context) []string {
	var ids []string
	kittyOf := map[string]string{}
	s.mu.RLock()
	for id, sess := range s.sessions {
		if len(sess.Turns) == 0 || sess.Working() {
			continue
		}
		last := sess.Turns[len(sess.Turns)-1]
		if !last.Parked || s.resuming[id] {
			continue
		}
		k, err := s.fleet.Get(last.Kitty)
		if err != nil || s.overDay(k) {
			continue
		}
		ids = append(ids, id)
		kittyOf[id] = k.ID
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.mu.Lock()
		if s.resuming == nil {
			s.resuming = map[string]bool{}
		}
		s.resuming[id] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func(id string) {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.resuming, id)
				s.mu.Unlock()
			}()
			text := resumeText
			if k, err := s.fleet.Get(kittyOf[id]); err == nil {
				text += s.todoNote(ctx, k)
			}
			_, _ = s.send(ctx, id, "scheduler", "", text)
		}(id)
	}
	return ids
}

// RunResumer looks for parked kitties to wake up every so often, until ctx ends.
func (s *Service) RunResumer(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ResumeParked(ctx)
		}
	}
}
