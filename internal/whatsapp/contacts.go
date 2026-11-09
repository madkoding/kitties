package whatsapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Contact is a person who wrote to the linked number, as the bridge knows them: the kitty reads
// every message with its author's name, so the people of a chat are not taken for one.
type Contact struct {
	// Number is their phone number, digits with the country code: it says who is who, whatever
	// name they give themselves.
	Number string `json:"number"`
	// Name is what the person calls them (set in Settings), and wins over PushName.
	Name string `json:"name,omitempty"`
	// PushName is the name they gave themselves on WhatsApp, the last one seen.
	PushName string `json:"push_name,omitempty"`
	// Seen is when they last wrote (in a chat with the kitty or in a group it is in).
	Seen time.Time `json:"seen,omitzero"`
	// Groups are the groups (JIDs) they wrote in, the last first: the kitty may @mention them
	// there by name.
	Groups []string `json:"groups,omitempty"`
}

// Label is how the kitty reads a contact: "Ana (+56912345678)". The name is Name, then PushName;
// a contact with neither is only "+56912345678". The number is always there, so two people with
// the same name (or a name made to look like someone else's) stay apart.
func (c Contact) Label() string {
	name := c.Name
	if name == "" {
		name = c.PushName
	}
	if name == "" {
		return "+" + c.Number
	}
	return name + " (+" + c.Number + ")"
}

// maxContacts is how many contacts are kept; past it the ones not seen for longest, without a
// name of the person's, are forgotten.
const maxContacts = 1000

// maxContactName is the longest name kept, in characters.
const maxContactName = 40

// contactsAAD binds the sealed contacts to their use.
var contactsAAD = []byte("whatsapp:contacts")

// ErrNoContact is a contact that is not known.
var ErrNoContact = errors.New("there is no such contact")

// CleanName is a name as the kitty may read it: one line, no brackets, quotes or tags (a name is
// written by whoever owns the number, so it must not look like part of the conversation), and
// at most maxContactName characters.
func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return ' '
		case strings.ContainsRune("<>[]{}()\"`", r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxContactName {
		s = strings.TrimSpace(string(r[:maxContactName]))
	}
	return s
}

func (b *Bridge) contactsPath() string { return filepath.Join(b.opt.Dir, "contacts.bin") }

// loadContacts opens the sealed contacts (they are personal data: the vault seals them).
func (b *Bridge) loadContacts() {
	if b.opt.Box == nil {
		return
	}
	data, err := os.ReadFile(b.contactsPath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			b.opt.Log("whatsapp: contacts: %v", err)
		}
		return
	}
	plain, err := b.opt.Box.Open(data, contactsAAD)
	if err != nil {
		b.opt.Log("whatsapp: the contacts could not be opened: %v", err)
		return
	}
	var list []Contact
	if err := json.Unmarshal(plain, &list); err != nil {
		b.opt.Log("whatsapp: contacts: %v", err)
		return
	}
	for _, c := range list {
		if c.Number != "" {
			b.contacts[c.Number] = c
		}
	}
}

// saveContactsLocked seals the contacts in contacts.bin.
func (b *Bridge) saveContactsLocked() error {
	if b.opt.Box == nil {
		return nil
	}
	plain, err := json.Marshal(b.contactListLocked())
	if err != nil {
		return err
	}
	sealed, err := b.opt.Box.Seal(plain, contactsAAD)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
		return err
	}
	tmp := b.contactsPath() + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.contactsPath())
}

// contactListLocked are the contacts, the ones seen last first.
func (b *Bridge) contactListLocked() []Contact {
	out := make([]Contact, 0, len(b.contacts))
	for _, c := range b.contacts {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Seen.Equal(out[j].Seen) {
			return out[i].Seen.After(out[j].Seen)
		}
		return out[i].Number < out[j].Number
	})
	return out
}

// metLocked notes that number wrote, under the name it gives itself. It is kept sealed when it
// is new or its name changed (the time it was seen alone waits for the next change).
func (b *Bridge) metLocked(number, pushName string) Contact {
	pushName = CleanName(pushName)
	c, ok := b.contacts[number]
	c.Number, c.Seen = number, b.now()
	changed := !ok || (pushName != "" && pushName != c.PushName)
	if pushName != "" {
		c.PushName = pushName
	}
	b.contacts[number] = c
	if !changed {
		return c
	}
	b.trimContactsLocked()
	if err := b.saveContactsLocked(); err != nil {
		b.opt.Log("whatsapp: contacts: %v", err)
	}
	return c
}

// trimContactsLocked forgets the contacts past maxContacts: the ones seen longest ago first,
// never one the person named.
func (b *Bridge) trimContactsLocked() {
	if len(b.contacts) <= maxContacts {
		return
	}
	list := b.contactListLocked()
	for i := len(list) - 1; i >= 0 && len(b.contacts) > maxContacts; i-- {
		if list[i].Name == "" {
			delete(b.contacts, list[i].Number)
		}
	}
}

// labelLocked is how the kitty reads number: its contact's label, or the number alone.
func (b *Bridge) labelLocked(number string) string {
	if c, ok := b.contacts[number]; ok {
		return c.Label()
	}
	return Contact{Number: number}.Label()
}

// Contacts are the people who wrote, the ones seen last first.
func (b *Bridge) Contacts() []Contact {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.contactListLocked()
}

// NameContact sets what the person calls a number ("" goes back to the name it gives itself).
// A number that never wrote is added, so it can be named before it does.
func (b *Bridge) NameContact(number, name string) (Contact, error) {
	n, err := Normalize(number)
	if err != nil {
		return Contact{}, err
	}
	name = CleanName(name)
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.contacts[n]
	if !ok && name == "" {
		return Contact{}, ErrNoContact
	}
	c.Number, c.Name = n, name
	b.contacts[n] = c
	b.trimContactsLocked()
	if err := b.saveContactsLocked(); err != nil {
		return Contact{}, fmt.Errorf("the contacts could not be kept: %w", err)
	}
	return c, nil
}

// ForgetContact removes a contact (it comes back, with the name it gives itself, when it writes).
func (b *Bridge) ForgetContact(number string) error {
	n, err := Normalize(number)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.contacts[n]; !ok {
		return ErrNoContact
	}
	delete(b.contacts, n)
	return b.saveContactsLocked()
}

// contactNamedLocked is the number of the contact called name (its Name or PushName, any case),
// when exactly one is.
func (b *Bridge) contactNamedLocked(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	found := ""
	for _, c := range b.contacts {
		if strings.ToLower(c.Name) == name || (c.Name == "" && strings.ToLower(c.PushName) == name) {
			if found != "" {
				return "", false
			}
			found = c.Number
		}
	}
	return found, found != ""
}

// shortName is the contact's name without its number ("" when it has none).
func (c Contact) shortName() string {
	if c.Name != "" {
		return c.Name
	}
	return c.PushName
}

// maxQuote is how much of a quoted message the kitty reads.
const maxQuote = 300

// quoteLineLocked is the line that tells the kitty which message a reply answers, and who wrote
// it: "(replying to Pedro (+56922222222): "¿vamos?")", or to its own message.
func (b *Bridge) quoteLineLocked(q *Quote, to string) string {
	if q == nil {
		return ""
	}
	text := strings.Join(strings.Fields(q.Text), " ")
	if r := []rune(text); len(r) > maxQuote {
		text = string(r[:maxQuote]) + "…"
	}
	var who string
	switch {
	case q.From != "" && q.From == b.status.Phone:
		who = "your own message (" + b.kittyNameLocked(to) + ")"
	case q.From != "":
		who = b.labelLocked(q.From)
	default:
		who = "an earlier message"
	}
	if text == "" {
		return "(replying to " + who + ")"
	}
	return "(replying to " + who + ": \"" + text + "\")"
}

// withMentionsLocked writes the @tags of a text with the names of who they mention: "@56922222222"
// (or WhatsApp's hidden id) becomes "@Pedro (+56922222222)", and the kitty's own number its name.
func (b *Bridge) withMentionsLocked(text string, mentions map[string]string, to string) string {
	if len(mentions) == 0 {
		return text
	}
	tags := make([]string, 0, len(mentions))
	for tag := range mentions {
		tags = append(tags, tag)
	}
	// Longer tags first, so one that starts another is not replaced inside it.
	sort.Slice(tags, func(i, j int) bool { return len(tags[i]) > len(tags[j]) })
	pairs := make([]string, 0, 2*len(tags))
	for _, tag := range tags {
		n := mentions[tag]
		label := b.labelLocked(n)
		if n == b.status.Phone && n != "" {
			label = b.kittyNameLocked(to)
		}
		pairs = append(pairs, "@"+tag, "@"+label)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// signed is a message with who wrote it before it, for a chat service that cannot post it as
// theirs (the kitty still reads who wrote).
func (b *Bridge) signed(in inbound) string {
	if in.sender == "" {
		return in.text
	}
	return in.sender + ": " + in.text
}
