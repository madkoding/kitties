package reply

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

// Channel says which topic to listen to: the ntfy connector's settings, and ok when replies are
// on and the connector can take them.
type Channel func() (connector.Settings, bool)

// message is one line of ntfy's JSON stream.
type message struct {
	ID      string   `json:"id"`
	Event   string   `json:"event"`
	Title   string   `json:"title"`
	Message string   `json:"message"`
	Tags    []string `json:"tags"`
}

// How often the listener looks again at the settings, and waits after the stream broke.
var (
	recheck = 30 * time.Second
	backoff = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}
)

// Listen follows the topic while replies are on, until ctx ends. A change of topic, server or
// token, or replies turned off, closes the stream; it opens again on the new settings.
func (s *Service) Listen(ctx context.Context, ch Channel, client *http.Client) {
	if client == nil {
		client = &http.Client{}
	}
	every := recheck
	last := "" // the id of the last message read, to go on from it after a break
	fails := 0
	for ctx.Err() == nil {
		set, ok := ch()
		if !ok {
			if !sleep(ctx, every) {
				return
			}
			continue
		}
		key := settingsKey(set)
		sctx, cancel := context.WithCancel(ctx)
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-sctx.Done():
					return
				case <-t.C:
					if now, ok := ch(); !ok || settingsKey(now) != key {
						cancel()
						return
					}
				}
			}
		}()
		read, err := s.stream(sctx, ctx, client, set, &last)
		changed := sctx.Err() != nil && ctx.Err() == nil
		cancel()
		if ctx.Err() != nil {
			return
		}
		if changed {
			last, fails = "", 0
			continue
		}
		if read {
			fails = 0
		}
		if err != nil {
			s.logf("reply: %v", err)
		}
		wait := backoff[min(fails, len(backoff)-1)]
		fails++
		if !sleep(ctx, wait) {
			return
		}
	}
}

func settingsKey(s map[string]string) string {
	return strings.Join([]string{connector.NtfyServer(s), s["topic"], s["token"]}, "\x00")
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// stream reads the topic's messages until it breaks; read says it got at least one line. The
// messages are handled under work, so a stream closed for new settings lets them finish.
func (s *Service) stream(ctx, work context.Context, client *http.Client, set map[string]string, last *string) (read bool, err error) {
	u := strings.TrimRight(connector.NtfyServer(set), "/") + "/" + url.PathEscape(strings.TrimSpace(set["topic"])) + "/json"
	if *last != "" {
		u += "?since=" + url.QueryEscape(*last)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	if t := strings.TrimSpace(set["token"]); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, fmt.Errorf("listen to the topic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("listen to the topic: the server said %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	for sc.Scan() {
		read = true
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Event != "message" {
			continue
		}
		if m.ID != "" {
			*last = m.ID
		}
		if marked(m.Tags) {
			continue
		}
		s.wg.Add(1)
		go func(m message) {
			defer s.wg.Done()
			if err := s.Handle(work, m.Title, m.Message); err != nil {
				s.logf("reply: %v", err)
			}
		}(m)
	}
	if ctx.Err() != nil {
		return read, nil
	}
	return read, sc.Err()
}

// marked says a message is one the gateway sent.
func marked(tags []string) bool {
	for _, t := range tags {
		if t == connector.NtfyMark {
			return true
		}
	}
	return false
}
