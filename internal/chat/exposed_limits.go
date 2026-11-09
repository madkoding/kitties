package chat

import (
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/madkoding/kitties/internal/kitty"
)

// Anyone in a WhatsApp group (or a contact) can put the kitty to work, so a task asked there gets
// a short leash: these caps apply over the kitty's own limits and the defaults, never above them.
// The default is 5 minutes; Settings › WhatsApp may give a chat more or less (SetExposedMinutes),
// and the rounds and tokens follow the minutes.
const (
	ExposedMaxSeconds      = 300
	ExposedMaxRounds       = 30
	ExposedMaxTokens       = 200_000
	exposedRoundsPerMinute = ExposedMaxRounds * 60 / ExposedMaxSeconds
	exposedTokensPerMinute = ExposedMaxTokens * 60 / ExposedMaxSeconds
)

// SetExposedMinutes tells how long a task asked in a chat with people who are not the kitty's
// person may run, in minutes (whatsapp.Bridge.TaskMinutes); 0 takes the default.
func (s *Service) SetExposedMinutes(f func(chatID string) int) { s.exposedMinutes = f }

// exposedLimits caps l for a task asked in a chat with people who are not the kitty's person:
// minutes (0: the default 5) with rounds and tokens to match, no retries, and it stops at the end
// of its rounds instead of going on by itself through more windows.
func exposedLimits(l kitty.Limits, minutes int) kitty.Limits {
	capAt := func(v, most int) int {
		if v <= 0 || v > most {
			return most
		}
		return v
	}
	seconds, rounds, tokens := ExposedMaxSeconds, ExposedMaxRounds, ExposedMaxTokens
	if minutes > 0 {
		seconds, rounds, tokens = minutes*60, min(minutes*exposedRoundsPerMinute, kitty.MaxRoundsCap), min(minutes*exposedTokensPerMinute, kitty.MaxTokensCap)
	}
	l.MaxSeconds = capAt(l.MaxSeconds, seconds)
	l.MaxRounds = capAt(l.MaxRounds, rounds)
	l.MaxTokens = capAt(l.MaxTokens, tokens)
	l.Retries = 0
	l.Attended = true
	return l
}

// linkTools are the desktop tools that open an address in the kitty's browser.
var linkTools = map[string]bool{"open": true, "page_goto": true, "page_flow": true}

// actionLinks are the addresses a then opens: the url of open/page_goto, the goto of each step of
// a page_flow.
func actionLinks(a action) []string {
	if a.kind != "tool" || !linkTools[a.name] {
		return nil
	}
	var out []string
	if a.name == "page_flow" {
		steps, _ := a.args["steps"].([]any)
		for _, st := range steps {
			if m, ok := st.(map[string]any); ok {
				if u, ok := m["goto"].(string); ok {
					out = append(out, strings.TrimSpace(u))
				}
			}
		}
		return out
	}
	for _, key := range []string{"url", "text"} {
		if u, ok := a.args[key].(string); ok && strings.TrimSpace(u) != "" {
			return []string{strings.TrimSpace(u)}
		}
	}
	return nil
}

// runnable are the endings of files that run when opened: a link to one is a download, not a page.
var runnable = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true, ".ps1": true,
	".vbs": true, ".js": true, ".jar": true, ".apk": true, ".dmg": true, ".pkg": true, ".app": true,
	".deb": true, ".rpm": true, ".appimage": true, ".sh": true, ".run": true, ".bin": true,
	".lnk": true, ".hta": true, ".iso": true, ".img": true,
}

// linkProblem says why the kitty should not open raw for someone who is not its person, empty when
// it looks like an ordinary public web page: only http(s), no name:password@ in it (it hides the
// real site), nothing on the kitty's own machine or network, and no program to download.
func linkProblem(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "it is not a web address"
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return "only http:// and https:// pages open here"
	}
	if u.User != nil {
		return "it has a name before @ that hides the real site"
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home.arpa") ||
		!strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return "it points inside this machine or its network"
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return "it points inside this machine or its network"
	}
	if runnable[strings.ToLower(path.Ext(u.Path))] {
		return "it downloads a program"
	}
	return ""
}

// publicIP reports whether ip is an address on the internet, not a private, loopback, link-local
// or otherwise special one.
func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.Equal(net.IPv4bcast) || (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64)) // 100.64/10
}

// linkRefused checks the addresses a would open in a chat with people who are not the kitty's
// person: the first that should not open is recorded with the guard and returned as what the
// kitty is told; empty lets the action run.
func (s *Service) linkRefused(id string, a action) string {
	if !s.guardedID(id) {
		return ""
	}
	for _, link := range actionLinks(a) {
		why := linkProblem(link)
		if why == "" {
			continue
		}
		who := ""
		if sess, err := s.Get(id); err == nil {
			who = sess.Audience
		}
		s.guard.Blocked(id, who, "link "+clip(link, 120))
		return "not opened: " + why + ". Say so in words and do not try another way to open it"
	}
	return ""
}
