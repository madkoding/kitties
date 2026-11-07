package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A chat's gist is the short text (two lines at most) the sidebar shows under its title, so the person sees what each
// chat is about without opening it. It is made from the chat's last words, without asking any
// brain: it costs nothing, it is kept on the session and it changes when the chat does (save).
// A gist leaves the chat (a list, a phone, a screenshot), so whatever looks like a secret or a
// person's details is masked before it is cut.

// GistLen is the most a gist holds, in characters.
const GistLen = 140

// gistScan is how much of a turn a gist reads: the start says what the turn is about.
const gistScan = 600

// mask stands for what a gist hides.
const mask = "•••"

var (
	gistFence  = regexp.MustCompile("(?s)```.*?(```|$)")
	gistKeyPEM = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|$)`)
	gistImage  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	gistLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	gistURL    = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftp)://(?:[^/\s?#@]*@)?([^/\s?#@]+)[^\s)]*`)
	gistEmail  = regexp.MustCompile(`[\p{L}\p{N}._%+-]+@[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+`)
	gistBearer = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	// A secret said with its name: "token: …", "password is …".
	gistNamed  = regexp.MustCompile(`(?i)\b(passwords?|passwd|pwd|passphrase|token|secret|api[ _-]?key|apikey|key|pin|otp|cvv|contraseña|clave|senha|chave|código|code)(\s*[:=]\s*)(\S+)`)
	gistSaid   = regexp.MustCompile(`(?i)\b(passwords?|passphrase|pin|otp|contraseña|clave|senha)(\s+(?:is|es|é)\s+)(\S+)`)
	gistPrefix = regexp.MustCompile(`\b(?:(?:sk|pk|rk)-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{10,}|github_pat_[A-Za-z0-9_]{10,}|xox[abprs]-[A-Za-z0-9-]{8,}|AKIA[0-9A-Z]{12,}|AIza[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{10,})`)
	gistJWT    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	gistToken  = regexp.MustCompile(`[A-Za-z0-9_+/=-]{24,}`)
	gistDigits = regexp.MustCompile(`\+?\d(?:[ .-]?\d)+`)
	gistDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	gistMarks  = regexp.MustCompile("(?m)^\\s*(?:#{1,6}\\s+|>\\s*|[-*+]\\s+|\\d+[.)]\\s+)")
	gistInline = regexp.MustCompile("\\*\\*|__|~~|`")
	gistSpace  = regexp.MustCompile(`\s+`)
)

// Gist is the text on a single line (the sidebar wraps it to two), without markdown and with whatever looks like a secret, a link's
// path, an address or a long number masked, at most GistLen characters.
func Gist(text string) string {
	if len(text) > gistScan {
		cut := gistScan
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		// Ending on a space keeps a secret that the cut halves from slipping out unmasked.
		if sp := strings.LastIndexFunc(text[:cut], unicode.IsSpace); sp > 0 {
			cut = sp
		}
		text = text[:cut]
	}
	text = gistKeyPEM.ReplaceAllString(text, mask)
	text = gistFence.ReplaceAllString(text, " ")
	text = gistImage.ReplaceAllString(text, " ")
	text = gistLink.ReplaceAllString(text, "$1")
	text = Redact(text)
	text = gistMarks.ReplaceAllString(text, "")
	text = gistInline.ReplaceAllString(text, "")
	text = strings.TrimSpace(gistSpace.ReplaceAllString(text, " "))
	return clipLine(text, GistLen)
}

// Redact masks what looks like a secret or a person's details in a text: keys and tokens,
// passwords and codes said with their name, e-mail addresses, long numbers (cards, phones,
// accounts) and the path and query of links, which keep only their host.
func Redact(text string) string {
	text = gistURL.ReplaceAllString(text, "$1")
	text = gistEmail.ReplaceAllString(text, mask)
	text = gistBearer.ReplaceAllString(text, "$1 "+mask)
	text = gistPrefix.ReplaceAllString(text, mask)
	text = gistJWT.ReplaceAllString(text, mask)
	text = gistNamed.ReplaceAllString(text, "$1$2"+mask)
	text = gistSaid.ReplaceAllString(text, "$1$2"+mask)
	text = gistToken.ReplaceAllStringFunc(text, func(s string) string {
		if looksLikeToken(s) {
			return mask
		}
		return s
	})
	return gistDigits.ReplaceAllStringFunc(text, func(s string) string {
		digits := 0
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits < 6 || gistDate.MatchString(s) {
			return s
		}
		return mask
	})
}

// looksLikeToken tells a key or an id (letters and digits, long) from a word or a path: a path
// is lower case with slashes, a key mixes cases or has none.
func looksLikeToken(s string) bool {
	var digit, upper, lower bool
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		}
	}
	if !digit || !(upper || lower) {
		return false
	}
	return upper && lower || !strings.Contains(s, "/")
}

// clipLine cuts a line to n characters, on a word when there is one near, with an ellipsis.
func clipLine(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n-1]
	cut := len(r)
	for i := len(r) - 1; i > n*2/3; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), func(c rune) bool {
		return unicode.IsSpace(c) || unicode.IsPunct(c)
	}) + "…"
}

// gistOf is the gist of a session and who said it: the last turn with words, read backwards so
// a kitty still working shows the ask it works on. The connectors' results ("system") are not
// what the chat is about, and the condensed summary stands in for a thread with no turns left.
func gistOf(sess *Session) (text, by string) {
	for i := len(sess.Turns) - 1; i >= 0; i-- {
		t := &sess.Turns[i]
		if t.From == "system" {
			continue
		}
		if g := Gist(t.Text); g != "" {
			return g, t.From
		}
	}
	if sess.Summary != "" {
		return Gist(sess.Summary), ""
	}
	return "", ""
}

// refreshGist keeps a session's gist up to date; save calls it, so every change refreshes it.
func refreshGist(sess *Session) {
	sess.Gist, sess.GistBy = gistOf(sess)
}
