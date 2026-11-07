package guard

import (
	"regexp"
	"sort"
	"strings"
)

// WHAT MUST NOT LEAVE.
//
// Scrub takes a text that is about to reach someone who is not the person (a WhatsApp contact, a
// group, a channel a connector posts to) and puts a mark in place of what must not reach them:
//
//   - the secrets this gateway knows by value (the vault's, the models' keys, the connections'
//     tokens, the gateway's own token): whatever their shape;
//   - what looks like a credential: API keys of the usual services, private keys, JWTs, bearer
//     tokens, "password: …" and the like;
//   - where things are inside: the kitties' data folder, home folders, private network addresses;
//   - with personal on, evident personal data: e-mail addresses, phone numbers, card numbers and
//     Chilean RUTs, unless the other side wrote them in the conversation itself (their own data).
//
// Mark is what stands in for each one, and the labels say what was taken out, for the record.

// Mark stands in for what is taken out.
const Mark = "[redacted]"

// minSecret is the shortest known secret matched by value: shorter ones would hit ordinary words.
const minSecret = 6

// A pattern is a shape that is taken out, with the label the record shows.
type pattern struct {
	label string
	re    *regexp.Regexp
	// group, when set, is the submatch that is taken out (the value after "password:").
	group int
	// check, when set, must accept the match for it to be taken out.
	check func(string) bool
}

var credentials = []pattern{
	{label: "private key", re: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`)},
	{label: "API key", re: regexp.MustCompile(`\b(?:sk-ant-[A-Za-z0-9_\-]{20,}|sk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{20,}|gsk_[A-Za-z0-9]{20,}|xai-[A-Za-z0-9]{20,}|AIza[0-9A-Za-z_\-]{30,}|hf_[A-Za-z0-9]{30,}|r8_[A-Za-z0-9]{30,}|pplx-[A-Za-z0-9]{30,})`)},
	{label: "GitHub token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})`)},
	{label: "cloud key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{label: "Slack token", re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`)},
	{label: "Stripe key", re: regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{label: "bot token", re: regexp.MustCompile(`\b\d{8,10}:AA[A-Za-z0-9_\-]{30,}`)},
	{label: "ntfy token", re: regexp.MustCompile(`\btk_[A-Za-z0-9]{24,}`)},
	{label: "JWT", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)},
	{label: "bearer token", re: regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9_\-\.=+/]{16,})`), group: 1},
	{label: "password", re: regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|passphrase|contraseña|contrasena|clave|senha|secret|token|api[_\- ]?key|access[_\- ]?key)\s*[:=]\s*["'` + "`" + `]?([^\s"'` + "`" + `,;]{4,})`), group: 1},
	{label: "credentials in a link", re: regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s/:@]+:([^\s/@]+)@`), group: 1},
}

var internals = []pattern{
	{label: "internal path", re: regexp.MustCompile(`(?:~|\$HOME)/\.kitties[^\s"'` + "`" + `)]*|/(?:home|Users|root)/[A-Za-z0-9._\-]+(?:/[^\s"'` + "`" + `)]*)?|[A-Za-z]:\\Users\\[^\s"'` + "`" + `)]+|/var/lib/kitties[^\s"'` + "`" + `)]*|/run/secrets[^\s"'` + "`" + `)]*`)},
	{label: "private address", re: regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|127\.\d{1,3}\.\d{1,3}\.\d{1,3})(?::\d{2,5})?\b|\blocalhost:\d{2,5}\b`)},
}

var personalData = []pattern{
	{label: "e-mail", re: regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)},
	{label: "card number", re: regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`), check: luhn},
	{label: "RUT", re: regexp.MustCompile(`\b\d{1,2}\.\d{3}\.\d{3}-[\dkK]\b|\b\d{7,8}-[\dkK]\b`)},
	{label: "phone number", re: regexp.MustCompile(`\+\d[\d \-()]{7,18}\d|\b\d{9,15}\b`), check: phone},
}

// phone accepts 8 to 15 digits: fewer is a code or a number, more a long id.
func phone(s string) bool {
	n := len(digits(s))
	return n >= 8 && n <= 15
}

// luhn reports whether the digits pass the check every card number passes.
func luhn(s string) bool {
	d := digits(s)
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	sum, double := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0
}

func digits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// Scrub returns text without what must not leave, and the labels of what was taken out (one per
// kind, sorted). secrets are the values known to be secret; keep is what the other side wrote
// (personal data found there are theirs, and stay); personal takes evident personal data out too.
func Scrub(text string, secrets []string, keep string, personal bool) (string, []string) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	found := map[string]bool{}
	// The longest secrets first, so one that holds another is taken whole.
	known := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s = strings.TrimSpace(s); len(s) >= minSecret {
			known = append(known, s)
		}
	}
	sort.Slice(known, func(i, j int) bool { return len(known[i]) > len(known[j]) })
	for _, s := range known {
		if strings.Contains(text, s) {
			text = strings.ReplaceAll(text, s, Mark)
			found["saved secret"] = true
		}
	}
	text = apply(text, credentials, found, nil)
	text = apply(text, internals, found, nil)
	if personal {
		text = apply(text, personalData, found, ownData(keep))
	}
	labels := make([]string, 0, len(found))
	for l := range found {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	return text, labels
}

// numberish finds the numbers someone wrote, however they spaced them.
var numberish = regexp.MustCompile(`\+?\d[\d \-().]{6,}\d`)

// ownData tells whether a piece of personal data is one the other side wrote themselves.
func ownData(keep string) func(string) bool {
	lower := strings.ToLower(keep)
	nums := map[string]bool{}
	for _, m := range numberish.FindAllString(keep, -1) {
		if d := digits(m); len(d) >= 7 {
			nums[d] = true
		}
	}
	return func(s string) bool {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
		d := digits(s)
		if len(d) < 7 {
			return false
		}
		for n := range nums {
			if strings.HasSuffix(n, d) || strings.HasSuffix(d, n) {
				return true
			}
		}
		return false
	}
}

func apply(text string, list []pattern, found map[string]bool, own func(string) bool) string {
	for _, p := range list {
		text = p.re.ReplaceAllStringFunc(text, func(m string) string {
			if p.check != nil && !p.check(m) {
				return m
			}
			if own != nil && own(m) {
				return m
			}
			if p.group > 0 {
				sub := p.re.FindStringSubmatch(m)
				if len(sub) <= p.group || sub[p.group] == "" || sub[p.group] == Mark || strings.Contains(sub[p.group], Mark) {
					return m
				}
				found[p.label] = true
				return strings.Replace(m, sub[p.group], Mark, 1)
			}
			if m == Mark {
				return m
			}
			found[p.label] = true
			return Mark
		})
	}
	return text
}
