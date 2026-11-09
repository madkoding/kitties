package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The ways the kitties may @mention every member of a group (Settings.MentionAll).
const (
	// MentionAllNeeded (the default, also "") lets a kitty mention everyone only when all of the
	// group must see a message (a change of plans, an urgent notice, an announcement someone asked
	// for), at most once every MentionAllHours in each group.
	MentionAllNeeded = "needed"
	// MentionAllOff never mentions everyone: "@all" goes as plain text.
	MentionAllOff = "off"
)

// DefaultMentionAllHours is the least time between two mentions of everyone in a group.
const DefaultMentionAllHours = 4

// MaxMentionAllHours is the longest gap Settings takes (a week).
const MaxMentionAllHours = 168

// Mentions are who a message @mentions in a group, so WhatsApp notifies them even when they
// muted it: Numbers (digits) its text writes as "@<number>", and All for every member (the
// text keeps its own "@all"; the members go unseen, as WhatsApp does).
type Mentions struct {
	Numbers []string
	All     bool
}

// Mentioner is a Client that @mentions people in a group.
type Mentioner interface {
	// SendMentions writes text to a group mentioning m, as an answer to ref (quoting quoted)
	// when ref.ID is set. A number that is not a member of the group is not mentioned.
	SendMentions(ctx context.Context, to, text string, m Mentions, ref Ref, quoted string) error
}

// etiquetter is the part of the chat service that tells a kitty how to write in one chat.
type etiquetter interface {
	SetEtiquette(id, text string) error
}

// mentionAllMode is a MentionAll of Settings: MentionAllNeeded for "" too.
func mentionAllMode(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "", MentionAllNeeded:
		return MentionAllNeeded, nil
	case MentionAllOff, "never":
		return MentionAllOff, nil
	}
	return "", fmt.Errorf("mention_all is %q (only when all of the group must see it) or %q (never)", MentionAllNeeded, MentionAllOff)
}

// mentionAllHours is a MentionAllHours of Settings (0 is DefaultMentionAllHours).
func mentionAllHours(h int) (int, error) {
	if h < 0 || h > MaxMentionAllHours {
		return 0, fmt.Errorf("the hours between two mentions of everyone go from 1 to %d (0 is %d)", MaxMentionAllHours, DefaultMentionAllHours)
	}
	return h, nil
}

// maxContactGroups is how many groups a contact is remembered in.
const maxContactGroups = 20

// inGroupLocked notes that number wrote in group, so the kitty may @mention them there by name.
func (b *Bridge) inGroupLocked(number, group string) {
	c, ok := b.contacts[number]
	if !ok || contains(c.Groups, group) {
		return
	}
	c.Groups = append([]string{group}, c.Groups...)
	if len(c.Groups) > maxContactGroups {
		c.Groups = c.Groups[:maxContactGroups]
	}
	b.contacts[number] = c
	if err := b.saveContactsLocked(); err != nil {
		b.opt.Log("whatsapp: contacts: %v", err)
	}
}

var (
	// allTag is "@all" and its other words, as a kitty writes it.
	allTag = regexp.MustCompile(`(?i)@(all|everyone|everybody|todos|todas|todes|todo el grupo|todos e todas)\b`)
	// labelTag is a label copied whole: "@Ana (+56912345678)".
	labelTag = regexp.MustCompile(`@([^@\n()]{1,40}?) \(\+(\d{6,15})\)`)
	// numberTag is "@56912345678" or "@+56912345678".
	numberTag = regexp.MustCompile(`@\+?(\d{6,15})\b`)
	// nameTag is "@" and up to three words of a name.
	nameTag = regexp.MustCompile(`@(\p{L}[\p{L}\p{M}\p{N}'._-]*(?:[ ]\p{L}[\p{L}\p{M}\p{N}'._-]*){0,2})`)
)

// mentionsLocked reads the @tags of a text the kitty writes to a group: a member's name (as it
// knows them in this group) or number becomes "@<number>", which WhatsApp shows as their name and
// notifies; "@all" asks to mention every member. Outside a group the text is left as it is.
func (b *Bridge) mentionsLocked(to, text string) (string, Mentions) {
	var m Mentions
	if !IsGroup(to) || !strings.Contains(text, "@") {
		return text, m
	}
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && n != b.status.Phone && !seen[n] {
			seen[n] = true
			m.Numbers = append(m.Numbers, n)
		}
	}
	text = labelTag.ReplaceAllStringFunc(text, func(s string) string {
		t := labelTag.FindStringSubmatch(s)
		// Only a label whose name is that number's: "@Pedro, call (+569…)" is not one.
		if c, ok := b.contacts[t[2]]; !ok || (!strings.EqualFold(c.Name, t[1]) && !strings.EqualFold(c.PushName, t[1])) {
			return s
		}
		add(t[2])
		return "@" + t[2]
	})
	text = numberTag.ReplaceAllStringFunc(text, func(s string) string {
		n := numberTag.FindStringSubmatch(s)[1]
		add(n)
		return "@" + n
	})
	m.All = allTag.MatchString(text)
	text = nameTag.ReplaceAllStringFunc(text, func(s string) string {
		words := strings.Fields(strings.TrimPrefix(s, "@"))
		// The longest name that is a member's wins: "@Ana María" before "@Ana".
		for i := len(words); i > 0; i-- {
			if n, ok := b.memberNamedLocked(to, strings.Join(words[:i], " ")); ok {
				add(n)
				rest := strings.Join(words[i:], " ")
				if rest != "" {
					rest = " " + rest
				}
				return "@" + n + rest
			}
		}
		return s
	})
	return text, m
}

// memberNamedLocked is the number of the one member of group called name (its Name or PushName,
// any case, trailing punctuation aside).
func (b *Bridge) memberNamedLocked(group, name string) (string, bool) {
	name = strings.ToLower(strings.TrimRight(strings.TrimSpace(name), ".,;:!?'"))
	if name == "" {
		return "", false
	}
	found := ""
	for _, c := range b.contacts {
		if !contains(c.Groups, group) {
			continue
		}
		if strings.ToLower(c.Name) == name || strings.ToLower(c.PushName) == name {
			if found != "" && found != c.Number {
				return "", false // two members with that name: the kitty must use the number
			}
			found = c.Number
		}
	}
	return found, found != ""
}

// everyoneLocked reports whether a kitty may mention every member of group now, and takes the
// turn when it may: never with MentionAll off, and once every MentionAllHours in a group.
func (b *Bridge) everyoneLocked(group string) (bool, string) {
	if b.set.MentionAll == MentionAllOff {
		return false, "mentioning everyone is off in Settings"
	}
	gap := time.Duration(b.mentionGapLocked()) * time.Hour
	now := b.now()
	if last, ok := b.everyone[group]; ok && now.Sub(last) < gap {
		return false, "everyone was mentioned in this group less than " + strconv.Itoa(b.mentionGapLocked()) + " hours ago"
	}
	b.everyone[group] = now
	return true, ""
}

func (b *Bridge) mentionGapLocked() int {
	if b.set.MentionAllHours > 0 {
		return b.set.MentionAllHours
	}
	return DefaultMentionAllHours
}

// mentionPart is who part of a text mentions: the numbers its own "@<number>" name, and all
// when it holds the "@all" of a text whose mention of everyone was allowed.
func mentionPart(part string, m Mentions, all bool) Mentions {
	out := Mentions{All: all && allTag.MatchString(part)}
	in := map[string]bool{}
	for _, t := range numberTag.FindAllStringSubmatch(part, -1) {
		in[t[1]] = true
	}
	for _, n := range m.Numbers {
		if in[n] {
			out.Numbers = append(out.Numbers, n)
		}
	}
	return out
}

// sendText writes one part of a text to to: mentioning who it names in a group (when the Client
// can), quoting ref when quote is set.
func sendText(ctx context.Context, c Client, to, part string, m Mentions, quote bool, ref Ref, quoted string) error {
	if mc, ok := c.(Mentioner); ok && (m.All || len(m.Numbers) > 0) {
		if !quote {
			ref = Ref{}
		}
		return mc.SendMentions(ctx, to, part, m, ref, quoted)
	}
	if quote {
		return c.SendReply(ctx, to, part, ref, quoted)
	}
	return c.Send(ctx, to, part)
}

// prepareLocked reads the @tags of a text for to and says whether its "@all" may mention
// everyone (and why not, when it may not).
func (b *Bridge) prepareLocked(to, text string) (string, Mentions, string) {
	text, m := b.mentionsLocked(to, text)
	note := ""
	if m.All {
		ok, why := b.everyoneLocked(to)
		if !ok {
			m.All, note = false, why
		}
	}
	return text, m, note
}

// etiquette is how a kitty writes in a WhatsApp group: whom it answers, how to @mention someone
// and when to mention everyone.
func (b *Bridge) etiquette(group string) string {
	b.mu.Lock()
	mode, gap := b.set.MentionAll, b.mentionGapLocked()
	var names []string
	for _, c := range b.contactListLocked() {
		if contains(c.Groups, group) && len(names) < 15 {
			names = append(names, c.Label())
		}
	}
	b.mu.Unlock()
	sort.Strings(names)
	out := "This is a WhatsApp group: several people read what you write. Your answer goes as a reply to the message you answer, " +
		"which already notifies its author, so you do not need to @mention them. " +
		"To call someone else, write @ and their name as you know them here, or their number (@Ana or @+56912345678); " +
		"only people who wrote in this group can be mentioned by name"
	if len(names) > 0 {
		out += " (" + strings.Join(names, "; ") + ")"
	}
	out += ". "
	if mode == MentionAllOff {
		return out + "Never mention everyone (@all): it is off here."
	}
	return out + "Write @all only when every member must see the message now: a change of plans, an urgent notice, " +
		"or an announcement someone asked you to make to all. Never in an ordinary answer, a greeting or a question to one person. " +
		"It works at most once every " + strconv.Itoa(gap) + " hours in a group; past that, @all goes as plain text."
}

// teach tells the chat of a group how the kitty writes there.
func (b *Bridge) teach(id string, in inbound) {
	e, ok := b.opt.Chats.(etiquetter)
	if !ok || !in.group {
		return
	}
	if err := e.SetEtiquette(id, b.etiquette(in.to)); err != nil {
		b.opt.Log("whatsapp: the manners of %s: %v", in.to, err)
	}
}
