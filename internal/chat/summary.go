package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/notify"
)

// SummaryAsk is what the lead is asked when it sums its Reports thread up: by the button in
// its drawer (the page writes it in the person's language) or every day (autonomy.lead_summary).
const SummaryAsk = "Summarize the reports in this thread since your last summary: what each kitty did, " +
	"what is stuck or failed, what waits for me, and what you suggest next. Short, in bullets."

// SummaryTitle heads the notification that carries the lead's summary.
const SummaryTitle = "Summary of the reports"

// ErrNoLead says the fleet has no lead kitty to ask.
var ErrNoLead = errors.New("the fleet has no lead kitty")

// LeadSummary asks the lead to sum its Reports thread up, in that thread, and returns the
// notification that carries its answer, in its name. A desktop lead may still be at it when
// this returns: then the notification points to the thread.
func (s *Service) LeadSummary(ctx context.Context) (notify.Notification, error) {
	lead := s.Lead()
	if lead == nil {
		return notify.Notification{}, ErrNoLead
	}
	id, err := s.ReportsChat(lead)
	if err != nil {
		return notify.Notification{}, err
	}
	turns, err := s.Send(ctx, id, SummaryAsk)
	if err != nil {
		return notify.Notification{}, err
	}
	body := ""
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Kitty == lead.ID && strings.TrimSpace(turns[i].Text) != "" {
			body = strings.TrimSpace(turns[i].Text)
			break
		}
	}
	if body == "" {
		body = lead.Name + " is summing the reports up in its Reports thread."
	}
	return notify.Notification{Event: notify.Summary, Kitty: lead.Name, Title: SummaryTitle, Body: body, Chat: id}, nil
}

// SummaryWindow is how late a day's summary may still be asked for (the gateway was off at
// the time, or the lead was busy).
const SummaryWindow = 2 * time.Hour

// LeadSummaries asks the lead every day, at the time At gives, to sum its reports up, and
// hands the summary to Deliver. It remembers the last day in Path, so a restart does not ask twice.
type LeadSummaries struct {
	Chats   *Service
	At      func() (hour, minute int, ok bool)
	Deliver func(context.Context, notify.Notification) error
	Path    string
	// Now is the clock, time.Now when nil.
	Now func() time.Time

	mu   sync.Mutex
	last string
	read bool
}

func (l *LeadSummaries) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Tick asks for today's summary when its time has come and it was not asked yet, and reports
// whether it did. A busy lead is asked again on the next tick; a summary that cannot be
// delivered stays in the Reports thread and is not asked again.
func (l *LeadSummaries) Tick(ctx context.Context) (bool, error) {
	if l.At == nil || l.Chats == nil {
		return false, nil
	}
	h, m, ok := l.At()
	if !ok || l.Chats.Lead() == nil {
		return false, nil
	}
	now := l.now()
	due := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	day := now.Format("2006-01-02")
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	if now.Before(due) || now.Sub(due) > SummaryWindow || l.last == day {
		return false, nil
	}
	n, err := l.Chats.LeadSummary(ctx)
	if err != nil {
		return false, err
	}
	l.last = day
	if l.Path != "" {
		raw, _ := json.Marshal(map[string]string{"last": day})
		if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err == nil {
			_ = os.WriteFile(l.Path, raw, 0o600)
		}
	}
	if l.Deliver != nil {
		if err := l.Deliver(ctx, n); err != nil {
			return true, err
		}
	}
	return true, nil
}

// load reads the last day once.
func (l *LeadSummaries) load() {
	if l.read || l.Path == "" {
		return
	}
	l.read = true
	var v struct {
		Last string `json:"last"`
	}
	if raw, err := os.ReadFile(l.Path); err == nil && json.Unmarshal(raw, &v) == nil {
		l.last = v.Last
	}
}

// Run ticks every half minute until ctx ends; a failure goes to logf.
func (l *LeadSummaries) Run(ctx context.Context, logf func(string, ...any)) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if _, err := l.Tick(ctx); err != nil && logf != nil {
			logf("lead summary: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
