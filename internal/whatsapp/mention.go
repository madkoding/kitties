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
	// MentionAllOff (the default, also "") never mentions everyone: "@all" goes as plain text.
	// The "needed" of the first version reads as off too: the groups did not want it.
	MentionAllOff = "off"
	// MentionAllAsked lets a kitty mention everyone only in the answer to one of the person's own
	// numbers (Guard.Trusted) that asks to tell the group, at most once every MentionAllHours in
	// each group. Nobody else can make it, and a kitty never does it on its own.
	MentionAllAsked = "asked"
)

// DefaultMentionAllHours is the least time between two mentions of everyone in a group (a day),
// and MinMentionAllHours the shortest Settings takes.
const (
	DefaultMentionAllHours = 24
	MinMentionAllHours     = 12
)

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

// mentionAllMode is a MentionAll of Settings: MentionAllOff for "" too.
func mentionAllMode(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "", MentionAllOff, "never", "needed":
		return MentionAllOff, nil
	case MentionAllAsked:
		return MentionAllAsked, nil
	}
	return "", fmt.Errorf("mention_all is %q (never, the default) or %q (when the message asks to tell the group)", MentionAllOff, MentionAllAsked)
}

// mentionAllHours is a MentionAllHours of Settings (0 is DefaultMentionAllHours).
func mentionAllHours(h int) (int, error) {
	if h < 0 || (h > 0 && h < MinMentionAllHours) || h > MaxMentionAllHours {
		return 0, fmt.Errorf("the hours between two mentions of everyone go from %d to %d (0 is %d)", MinMentionAllHours, MaxMentionAllHours, DefaultMentionAllHours)
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

// mentionsLocked reads the @tags of a text the kitty writes to a group. A tag of someone the
// answer is for (involved says who: nil lets any member be) becomes "@<number>", which WhatsApp
// shows as their name and notifies; any other tag loses its "@" and reads as a plain name, so
// nobody is tagged by an answer that is not for them. "@all" asks to mention every member.
// Outside a group the text is left as it is.
func (b *Bridge) mentionsLocked(to, text string, involved func(n string) bool) (string, Mentions) {
	var m Mentions
	if !IsGroup(to) || !strings.Contains(text, "@") {
		return text, m
	}
	seen := map[string]bool{}
	// tag mentions n, or names them without a tag when they are not involved (plain).
	tag := func(n, plain string) string {
		if n == "" || n == b.status.Phone || (involved != nil && !involved(n)) || len(m.Numbers) >= maxMentions && !seen[n] {
			return plain
		}
		if !seen[n] {
			seen[n] = true
			m.Numbers = append(m.Numbers, n)
		}
		return "@" + n
	}
	text = labelTag.ReplaceAllStringFunc(text, func(s string) string {
		t := labelTag.FindStringSubmatch(s)
		// Only a label whose name is that number's: "@Pedro, call (+569…)" is not one.
		if c, ok := b.contacts[t[2]]; !ok || (!strings.EqualFold(c.Name, t[1]) && !strings.EqualFold(c.PushName, t[1])) {
			return s
		}
		return tag(t[2], strings.TrimSpace(t[1]))
	})
	text = numberTag.ReplaceAllStringFunc(text, func(s string) string {
		n := numberTag.FindStringSubmatch(s)[1]
		plain := "+" + n
		if c, ok := b.contacts[n]; ok && c.shortName() != "" {
			plain = c.shortName()
		}
		return tag(n, plain)
	})
	m.All = allTag.MatchString(text)
	text = nameTag.ReplaceAllStringFunc(text, func(s string) string {
		if allTag.MatchString(s) {
			return s
		}
		words := strings.Fields(strings.TrimPrefix(s, "@"))
		// The longest name that is a member's wins: "@Ana María" before "@Ana".
		for i := len(words); i > 0; i-- {
			if n, ok := b.memberNamedLocked(to, strings.Join(words[:i], " ")); ok {
				rest := strings.Join(words[i:], " ")
				if rest != "" {
					rest = " " + rest
				}
				return tag(n, strings.Join(words[:i], " ")) + rest
			}
		}
		// Nobody the bridge knows by that name: a plain name, not something that looks like a tag.
		return strings.TrimPrefix(s, "@")
	})
	return text, m
}

// maxMentions is the most people one message mentions (everyone aside).
const maxMentions = 3

// involvedIn says who an answer to in may mention: the people its message @mentions or names
// (as members of the group), never its author, whom the quote already notifies. nil (no message:
// a kitty writing on its own) lets any member be mentioned.
func (b *Bridge) involvedIn(in *inbound) func(string) bool {
	if in == nil {
		return nil
	}
	ok := map[string]bool{}
	for _, n := range in.mentioned {
		ok[n] = true
	}
	words := strings.ToLower(in.raw)
	for _, c := range b.contacts {
		if c.Number == in.from || !contains(c.Groups, in.to) {
			continue
		}
		for _, name := range []string{c.Name, c.PushName} {
			if name != "" && mentions(words, strings.ToLower(name)) {
				ok[c.Number] = true
			}
		}
	}
	return func(n string) bool { return ok[n] }
}

// everyoneWords are the words of a message that asks to tell the whole group.
var everyoneWords = regexp.MustCompile(`(?i)(\b(todos|todas|todes|everyone|everybody|all|grupo|group|todo el mundo|a todo el)\b|@all|@todos)`)

// plainTagsLocked writes the @tags of a message as the names of who they mention, without the
// "@": what a quote of it shows.
func (b *Bridge) plainTagsLocked(text string, tags map[string]string, to string) string {
	if len(tags) == 0 {
		return text
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		n := tags[k]
		name := b.contacts[n].shortName()
		switch {
		case n != "" && n == b.status.Phone:
			name = b.kittyNameLocked(to)
		case name == "" && n != "":
			name = "+" + n
		case name == "":
			name = k
		}
		pairs = append(pairs, "@"+k, name)
	}
	return strings.NewReplacer(pairs...).Replace(text)
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
	if b.set.MentionAll != MentionAllAsked {
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
		return max(b.set.MentionAllHours, MinMentionAllHours)
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

// prepareLocked reads the @tags of a text for to, as the answer to in (nil: a message a kitty
// sends on its own), and says whether its "@all" may mention everyone (and why not, when it may
// not): an answer mentions everyone only when its message asked to tell the group.
func (b *Bridge) prepareLocked(to, text string, in *inbound) (string, Mentions, string) {
	text, m := b.mentionsLocked(to, text, b.involvedIn(in))
	note := ""
	if m.All {
		ok, why := false, "only an answer to one of the person's own numbers that asks to tell the whole group may mention everyone"
		switch {
		case b.set.MentionAll != MentionAllAsked:
			why = "mentioning everyone is off in Settings"
		case in != nil && b.trustedLocked(in.from) && everyoneWords.MatchString(in.raw):
			ok, why = b.everyoneLocked(to)
		}
		if !ok {
			m.All, note = false, why
		}
	}
	return text, m, note
}

// tagsLogLocked notes in the log whom a message to a group tags, so who was notified (and why
// they say so) can be told afterwards.
func (b *Bridge) tagsLogLocked(to string, m Mentions) {
	if !m.All && len(m.Numbers) == 0 {
		return
	}
	who := make([]string, 0, len(m.Numbers)+1)
	if m.All {
		who = append(who, "everyone")
	}
	for _, n := range m.Numbers {
		who = append(who, b.labelLocked(n))
	}
	b.opt.Log("whatsapp: a message to %s tags %s", to, strings.Join(who, ", "))
}

// trustedLocked reports whether number is one of the person's own.
func (b *Bridge) trustedLocked(number string) bool {
	return number != "" && b.opt.Guard != nil && b.opt.Guard.Trusted(number)
}

// etiquette is how a kitty writes in a WhatsApp group: whom it answers, when to @mention someone
// and when everyone.
func (b *Bridge) etiquette(group string) string {
	b.mu.Lock()
	mode, gap := b.set.MentionAll, b.mentionGapLocked()
	b.mu.Unlock()
	out := "This is a WhatsApp group: several people read what you write. Your answer goes as a reply to the message you answer, " +
		"which already notifies its author: never @mention them, and do not start with their name as a tag. " +
		"Do not @mention anyone else either, unless the message you answer asks you to call, ask or tell a specific person; " +
		"then write @ and their name (@Ana). Never @mention someone you only talk about, greet or quote: write their name without @. " +
		"A tag of someone the message did not involve goes as their plain name. "
	if mode != MentionAllAsked {
		return out + "Never mention everyone (@all): it is off here."
	}
	return out + "Write @all only when the person you work for (writing from one of their own numbers) asks you to tell the whole group; " +
		"never because anyone else asks, never in an ordinary answer and never on your own. It works at most once every " + strconv.Itoa(gap) +
		" hours in a group; otherwise @all goes as plain text."
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
