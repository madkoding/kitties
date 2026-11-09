// Package wameow is the WhatsApp connection of the bridge (internal/whatsapp): whatsmeow, the
// WhatsApp Web protocol in Go, as a linked device of the person's phone. Its store (store.go)
// keeps everything in memory and exports it as one blob that the bridge seals with the vault's
// key, so no session is ever written in the clear.
package wameow

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"

	"github.com/madkoding/kitties/internal/whatsapp"
)

// conn is the part of whatsmeow.Client used here (a fake in the tests).
type conn interface {
	AddEventHandler(handler whatsmeow.EventHandler) uint32
	GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error)
	Connect() error
	Disconnect()
	SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	Logout(ctx context.Context) error
	SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error
	Download(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error)
	Upload(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error)
	MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
	BuildReaction(chat, sender types.JID, id types.MessageID, reaction string) *waE2E.Message
}

// Client is a whatsapp.Client over whatsmeow.
type Client struct {
	newConn func(*store.Device) conn
	// flushEvery is how often a changed session is handed out to be kept.
	flushEvery time.Duration

	mu     sync.Mutex
	conn   conn
	dev    *store.Device
	groups *groupCache
}

var (
	_ whatsapp.Client    = (*Client)(nil)
	_ whatsapp.Mentioner = (*Client)(nil)
)

// New is a connection to WhatsApp, for whatsapp.Options.NewClient.
func New() whatsapp.Client {
	return &Client{newConn: realConn, flushEvery: 2 * time.Second}
}

func realConn(dev *store.Device) conn { return whatsmeow.NewClient(dev, waLog.Noop) }

// ErrNotConnected is a send before the connection is up.
var ErrNotConnected = errors.New("WhatsApp is not connected")

// Run implements whatsapp.Client.
func (c *Client) Run(ctx context.Context, session []byte, emit func(whatsapp.Event)) error {
	st, dev, err := Open(session, nil)
	if err != nil {
		return err
	}
	cn := c.newConn(dev)
	groups := &groupCache{groups: map[types.JID]*groupInfo{}}
	c.mu.Lock()
	c.conn, c.dev, c.groups = cn, dev, groups
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()
	over := make(chan struct{}, 1)
	cn.AddEventHandler(func(evt any) {
		evs := translate(st, dev, evt)
		if m, ok := evt.(*events.Message); ok && len(evs) == 1 {
			evs[0].Files = media(ctx, cn, m.Message)
			if m.Info.IsGroup {
				chat := m.Info.Chat.ToNonAD()
				evs[0].ChatName = groups.of(ctx, cn, chat, forever).name
				// Who wrote, whose message it answers and whom it mentions come as WhatsApp's
				// hidden ids in a group that hides numbers: the members list says their numbers
				// when the store does not know them yet.
				if unresolved(&evs[0], m) {
					fill(&evs[0], m, groups.of(ctx, cn, chat, membersAge))
				}
			}
		}
		for _, ev := range evs {
			if ev.Kind == whatsapp.EvConnected {
				flush(st, emit)
			}
			emit(ev)
			if ev.Kind == whatsapp.EvLoggedOut {
				select {
				case over <- struct{}{}:
				default:
				}
			}
		}
	})
	var codes <-chan whatsmeow.QRChannelItem
	if dev.ID == nil {
		if codes, err = cn.GetQRChannel(ctx); err != nil {
			return err
		}
	}
	if err := cn.Connect(); err != nil {
		return err
	}
	defer cn.Disconnect()
	tick := time.NewTicker(c.flushEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			flush(st, emit)
			return nil
		case <-over:
			return nil
		case <-tick.C:
			flush(st, emit)
		case item, ok := <-codes:
			if !ok {
				codes = nil
				continue
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				emit(whatsapp.Event{Kind: whatsapp.EvQR, QR: item.Code})
			case whatsmeow.QRChannelSuccess.Event:
				codes = nil
				flush(st, emit)
			case whatsmeow.QRChannelTimeout.Event:
				emit(whatsapp.Event{Kind: whatsapp.EvQRExpired})
				return nil
			default:
				msg := item.Event
				if item.Error != nil {
					msg = item.Error.Error()
				}
				return errors.New("the link failed: " + msg)
			}
		}
	}
}

// forever and membersAge are how old a group's info may be: its name is asked once, its members
// again after a few minutes (people come and go).
const (
	forever    = time.Duration(1<<63 - 1)
	membersAge = 10 * time.Minute
)

// groupInfo is what a group says of itself: its name, and the number and JID of each member.
type groupInfo struct {
	name string
	// members maps a member's phone number to the JID to mention them by (their LID in a group
	// that hides numbers); lids maps a member's hidden id to their number.
	members map[string]types.JID
	lids    map[string]string
	at      time.Time
}

// groupCache keeps the info of the groups that wrote or that a kitty writes to.
type groupCache struct {
	mu     sync.Mutex
	groups map[types.JID]*groupInfo
}

// of is the info of a group, asked again when it is older than maxAge (an empty one when it cannot
// be asked).
func (g *groupCache) of(ctx context.Context, cn conn, jid types.JID, maxAge time.Duration) groupInfo {
	g.mu.Lock()
	info, ok := g.groups[jid]
	g.mu.Unlock()
	if ok && time.Since(info.at) < maxAge {
		return *info
	}
	gctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fresh := &groupInfo{members: map[string]types.JID{}, lids: map[string]string{}, at: time.Now()}
	if gi, err := cn.GetGroupInfo(gctx, jid); err == nil && gi != nil {
		fresh.name = gi.Name
		for _, p := range gi.Participants {
			phone := p.PhoneNumber.User
			if phone == "" && p.JID.Server == types.DefaultUserServer {
				phone = p.JID.User
			}
			if phone == "" {
				continue
			}
			fresh.members[phone] = p.JID
			if lid := p.LID.User; lid != "" {
				fresh.lids[lid] = phone
			} else if p.JID.Server == types.HiddenUserServer {
				fresh.lids[p.JID.User] = phone
			}
		}
	} else if ok {
		// It could not be asked: the old info stays, asked again next time.
		return *info
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.groups[jid] = fresh
	return *fresh
}

// unresolved reports whether a group message has a sender, a quoted author or a mention whose
// number is not known.
func unresolved(ev *whatsapp.Event, m *events.Message) bool {
	if ev.From == "" {
		return true
	}
	c := contextOf(m.Message)
	if c == nil {
		return false
	}
	if c.GetParticipant() != "" && ev.Quote != nil && ev.Quote.From == "" {
		return true
	}
	return len(c.GetMentionedJID()) > len(ev.Mentions)
}

// fill gives a group message the numbers of its sender, quoted author and mentions from the
// group's members, where the store did not know them.
func fill(ev *whatsapp.Event, m *events.Message, info groupInfo) {
	phone := func(j types.JID) string {
		if j.Server == types.DefaultUserServer {
			return j.User
		}
		return info.lids[j.User]
	}
	if ev.From == "" {
		for _, j := range []types.JID{m.Info.Sender, m.Info.SenderAlt} {
			if n := phone(j); n != "" {
				ev.From = n
				break
			}
		}
	}
	c := contextOf(m.Message)
	if c == nil {
		return
	}
	if ev.Quote != nil && ev.Quote.From == "" {
		if j, err := types.ParseJID(c.GetParticipant()); err == nil {
			ev.Quote.From = phone(j)
		}
	}
	for _, s := range c.GetMentionedJID() {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" || ev.Mentions[j.User] != "" {
			continue
		}
		if n := phone(j); n != "" {
			if ev.Mentions == nil {
				ev.Mentions = map[string]string{}
			}
			ev.Mentions[j.User] = n
		}
	}
}

// flush hands out the session when it changed.
func flush(st *Store, emit func(whatsapp.Event)) {
	if !st.Dirty() {
		return
	}
	blob, err := st.Export()
	if err != nil {
		return
	}
	emit(whatsapp.Event{Kind: whatsapp.EvSession, Session: blob})
}

// translate turns a whatsmeow event into the bridge's.
func translate(st *Store, dev *store.Device, evt any) []whatsapp.Event {
	switch e := evt.(type) {
	case *events.PairSuccess:
		return []whatsapp.Event{{Kind: whatsapp.EvPaired, Phone: e.ID.User}}
	case *events.Connected:
		return []whatsapp.Event{{Kind: whatsapp.EvConnected, Phone: dev.GetJID().User}}
	case *events.Disconnected, *events.StreamReplaced:
		return []whatsapp.Event{{Kind: whatsapp.EvDisconnected}}
	case *events.LoggedOut:
		return []whatsapp.Event{{Kind: whatsapp.EvLoggedOut}}
	case *events.TemporaryBan:
		return []whatsapp.Event{{Kind: whatsapp.EvError, Err: "WhatsApp banned this number for a while: " + e.String()}}
	case *events.ConnectFailure:
		return []whatsapp.Event{{Kind: whatsapp.EvError, Err: "WhatsApp refused the connection: " + e.Message}}
	case *events.ClientOutdated:
		return []whatsapp.Event{{Kind: whatsapp.EvError, Err: "WhatsApp says this client is outdated: update Kitties"}}
	case *events.Message:
		if e.Info.IsFromMe || e.Message == nil {
			return nil
		}
		m := e.Message
		text := firstOf(m.GetConversation(), m.GetExtendedTextMessage().GetText(), m.GetImageMessage().GetCaption(),
			m.GetVideoMessage().GetCaption(), m.GetDocumentMessage().GetCaption(), described(m))
		ev := whatsapp.Event{
			Kind: whatsapp.EvMessage, From: phoneOf(st, e.Info.Sender, e.Info.SenderAlt),
			Name: e.Info.PushName, Text: text, ID: e.Info.ID, Group: e.Info.IsGroup,
			Ref: whatsapp.Ref{Chat: e.Info.Chat.String(), Sender: e.Info.Sender.String(), ID: e.Info.ID},
		}
		if ev.Group {
			ev.Chat = e.Info.Chat.ToNonAD().String()
			ev.Mentioned = mentioned(dev, m)
		}
		ev.Quote = quoteOf(st, dev, m)
		ev.Mentions = mentionsOf(st, dev, m)
		return []whatsapp.Event{ev}
	}
	return nil
}

// described is the text of a sticker, a location or a contact, which carry none of their own.
func described(m *waE2E.Message) string {
	if x := m.GetStickerMessage(); x != nil {
		return strings.TrimSpace("(sticker) " + x.GetEmojis())
	}
	if x := m.GetLocationMessage(); x != nil {
		return place("(location)", x.GetDegreesLatitude(), x.GetDegreesLongitude(), x.GetName(), x.GetAddress(), x.GetComment())
	}
	if x := m.GetLiveLocationMessage(); x != nil {
		return place("(live location)", x.GetDegreesLatitude(), x.GetDegreesLongitude(), x.GetCaption())
	}
	if x := m.GetContactMessage(); x != nil {
		return "(contact) " + contact(x)
	}
	if x := m.GetContactsArrayMessage(); x != nil {
		var out []string
		for _, c := range x.GetContacts() {
			out = append(out, contact(c))
		}
		return "(contacts) " + strings.Join(out, "; ")
	}
	return ""
}

// place is a location as a line with a map link.
func place(kind string, lat, lng float64, words ...string) string {
	parts := []string{kind}
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			parts = append(parts, w)
		}
	}
	return strings.Join(parts, " ") + fmt.Sprintf(" https://maps.google.com/?q=%.6f,%.6f", lat, lng)
}

// contact is a shared contact as "Name: +number, +number", its numbers read from its vCard.
func contact(c *waE2E.ContactMessage) string {
	name := strings.TrimSpace(c.GetDisplayName())
	var tels []string
	for _, l := range strings.Split(c.GetVcard(), "\n") {
		l = strings.TrimSpace(l)
		key, _, ok := strings.Cut(l, ":")
		key, _, _ = strings.Cut(key, ";")
		if i := strings.LastIndex(key, "."); i >= 0 {
			key = key[i+1:] // item1.TEL
		}
		if !ok || !strings.EqualFold(key, "TEL") {
			continue
		}
		// TEL;waid=569…:+56 9 … keeps the number after the last colon.
		if val := strings.TrimSpace(l[strings.LastIndex(l, ":")+1:]); val != "" {
			tels = append(tels, val)
		}
	}
	if len(tels) == 0 {
		return name
	}
	if name == "" {
		return strings.Join(tels, ", ")
	}
	return name + ": " + strings.Join(tels, ", ")
}

func firstOf(texts ...string) string {
	for _, t := range texts {
		if t != "" {
			return t
		}
	}
	return ""
}

// contextOf is what a message says about the ones it answers or mentions.
func contextOf(m *waE2E.Message) *waE2E.ContextInfo {
	for _, c := range []*waE2E.ContextInfo{
		m.GetExtendedTextMessage().GetContextInfo(), m.GetImageMessage().GetContextInfo(),
		m.GetVideoMessage().GetContextInfo(), m.GetDocumentMessage().GetContextInfo(), m.GetAudioMessage().GetContextInfo(),
	} {
		if c != nil {
			return c
		}
	}
	return nil
}

// quoteOf is the message m answers, with the number of who wrote it (this device's own number when
// it answers one of the kitty's), or nil.
func quoteOf(st *Store, dev *store.Device, m *waE2E.Message) *whatsapp.Quote {
	c := contextOf(m)
	if c == nil || (c.GetStanzaID() == "" && c.GetQuotedMessage() == nil) {
		return nil
	}
	q := &whatsapp.Quote{}
	if qm := c.GetQuotedMessage(); qm != nil {
		q.Text = firstOf(qm.GetConversation(), qm.GetExtendedTextMessage().GetText(), qm.GetImageMessage().GetCaption(),
			qm.GetVideoMessage().GetCaption(), qm.GetDocumentMessage().GetCaption(), described(qm))
		if q.Text == "" {
			switch {
			case qm.GetImageMessage() != nil:
				q.Text = "(picture)"
			case qm.GetVideoMessage() != nil:
				q.Text = "(video)"
			case qm.GetAudioMessage() != nil:
				q.Text = "(audio)"
			case qm.GetDocumentMessage() != nil:
				q.Text = "(document) " + qm.GetDocumentMessage().GetFileName()
			}
		}
	}
	if j, err := types.ParseJID(c.GetParticipant()); err == nil && c.GetParticipant() != "" {
		q.From = numberOf(st, dev, j)
	}
	return q
}

// mentionsOf maps the @tags of m (the user part of each mentioned JID, as the text writes it) to
// their phone numbers.
func mentionsOf(st *Store, dev *store.Device, m *waE2E.Message) map[string]string {
	c := contextOf(m)
	if c == nil {
		return nil
	}
	var out map[string]string
	for _, s := range c.GetMentionedJID() {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" {
			continue
		}
		if n := numberOf(st, dev, j); n != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[j.User] = n
		}
	}
	return out
}

// numberOf is the phone number of a JID (a LID is looked up; this device's LID is its number).
func numberOf(st *Store, dev *store.Device, j types.JID) string {
	if own := dev.GetLID(); own.User != "" && j.User == own.User && j.Server == types.HiddenUserServer {
		return dev.GetJID().User
	}
	return phoneOf(st, j, types.JID{})
}

// mentioned reports whether a message @mentions this number or answers one of its messages.
func mentioned(dev *store.Device, m *waE2E.Message) bool {
	c := contextOf(m)
	if c == nil {
		return false
	}
	ours := map[string]bool{}
	for _, j := range []types.JID{dev.GetJID(), dev.GetLID()} {
		if j.User != "" {
			ours[j.User] = true
		}
	}
	for _, s := range append(c.GetMentionedJID(), c.GetParticipant()) {
		if j, err := types.ParseJID(s); err == nil && ours[j.User] {
			return true
		}
	}
	return false
}

// maxMedia is the largest file taken from a message (the chat takes 50 MB of any file).
const maxMedia = 25 << 20

// media downloads the picture, video, document or audio of a message.
func media(ctx context.Context, cn conn, m *waE2E.Message) []whatsapp.File {
	type item struct {
		msg   whatsmeow.DownloadableMessage
		size  uint64
		name  string
		mime  string
		voice bool
	}
	var items []item
	if x := m.GetImageMessage(); x != nil {
		items = append(items, item{x, x.GetFileLength(), "picture" + ext(x.GetMimetype(), ".jpg"), x.GetMimetype(), false})
	}
	if x := m.GetVideoMessage(); x != nil {
		items = append(items, item{x, x.GetFileLength(), "video" + ext(x.GetMimetype(), ".mp4"), x.GetMimetype(), false})
	}
	if x := m.GetDocumentMessage(); x != nil {
		items = append(items, item{x, x.GetFileLength(), firstOf(x.GetFileName(), "document"), x.GetMimetype(), false})
	}
	if x := m.GetAudioMessage(); x != nil {
		items = append(items, item{x, x.GetFileLength(), "audio" + ext(x.GetMimetype(), ".ogg"), x.GetMimetype(), x.GetPTT()})
	}
	if x := m.GetStickerMessage(); x != nil {
		items = append(items, item{x, x.GetFileLength(), "sticker" + ext(x.GetMimetype(), ".webp"), firstOf(x.GetMimetype(), "image/webp"), false})
	}
	var out []whatsapp.File
	for _, it := range items {
		if it.size > maxMedia {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, time.Minute)
		data, err := cn.Download(dctx, it.msg)
		cancel()
		if err != nil || len(data) == 0 || len(data) > maxMedia {
			continue
		}
		out = append(out, whatsapp.File{Name: it.name, MIME: it.mime, Data: data, Voice: it.voice})
	}
	return out
}

// ext is the file extension of a media type, or def.
func ext(mime, def string) string {
	base, _, _ := strings.Cut(mime, ";")
	switch strings.TrimSpace(base) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/jpeg":
		return ".jpg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	}
	return def
}

// phoneOf is the phone number of a sender, who may come as a LID (WhatsApp's hidden id).
func phoneOf(st *Store, sender, alt types.JID) string {
	for _, j := range []types.JID{sender, alt} {
		if j.Server == types.DefaultUserServer {
			return j.User
		}
	}
	for _, j := range []types.JID{sender, alt} {
		if j.Server == types.HiddenUserServer {
			if pn, err := st.GetPNForLID(context.Background(), j); err == nil && !pn.IsEmpty() {
				return pn.User
			}
		}
	}
	return ""
}

func (c *Client) current() (conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn, nil
}

// jidOf is a number (digits) or a JID (a group).
func jidOf(to string) (types.JID, error) {
	if strings.Contains(to, "@") {
		return types.ParseJID(to)
	}
	return types.NewJID(to, types.DefaultUserServer), nil
}

// Send implements whatsapp.Client.
func (c *Client) Send(ctx context.Context, to, text string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	jid, err := jidOf(to)
	if err != nil {
		return err
	}
	_, err = cn.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

// SendFile implements whatsapp.Client: a picture, video or audio goes as one, anything else as
// a document.
func (c *Client) SendFile(ctx context.Context, to string, f whatsapp.File, caption string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	jid, err := jidOf(to)
	if err != nil {
		return err
	}
	mime := firstOf(f.MIME, "application/octet-stream")
	kind := whatsmeow.MediaDocument
	switch {
	case mime == "image/jpeg" || mime == "image/png":
		kind = whatsmeow.MediaImage
	case mime == "video/mp4":
		kind = whatsmeow.MediaVideo
	case strings.HasPrefix(mime, "audio/"):
		kind = whatsmeow.MediaAudio
	}
	up, err := cn.Upload(ctx, f.Data, kind)
	if err != nil {
		return err
	}
	var capt *string
	if caption != "" {
		capt = proto.String(caption)
	}
	size := proto.Uint64(up.FileLength)
	var msg *waE2E.Message
	switch kind {
	case whatsmeow.MediaImage:
		msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: capt, Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: size}}
	case whatsmeow.MediaVideo:
		msg = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: capt, Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: size}}
	case whatsmeow.MediaAudio:
		audio := &waE2E.AudioMessage{Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: size}
		// A voice note (push to talk) plays in the chat like one recorded on the phone.
		if f.Voice {
			audio.PTT = proto.Bool(true)
			audio.Mimetype = proto.String(whatsapp.VoiceMIME)
			if sec := whatsapp.OggSeconds(f.Data); sec > 0 {
				audio.Seconds = proto.Uint32(sec)
			}
		}
		msg = &waE2E.Message{AudioMessage: audio}
	default:
		msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: capt, Mimetype: proto.String(mime), FileName: proto.String(f.Name), Title: proto.String(f.Name),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: size}}
	}
	if _, err := cn.SendMessage(ctx, jid, msg); err != nil {
		return err
	}
	// An audio has no caption: its words go after it.
	if kind == whatsmeow.MediaAudio && caption != "" {
		_, err = cn.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(caption)})
	}
	return err
}

// SendReply implements whatsapp.Client: the text quotes the message it answers.
func (c *Client) SendReply(ctx context.Context, to, text string, ref whatsapp.Ref, quoted string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	jid, err := jidOf(to)
	if err != nil {
		return err
	}
	ctxInfo := &waE2E.ContextInfo{StanzaID: proto.String(ref.ID), QuotedMessage: &waE2E.Message{Conversation: proto.String(quoted)}}
	if ref.Sender != "" {
		ctxInfo.Participant = proto.String(ref.Sender)
	}
	if ref.Chat != "" {
		ctxInfo.RemoteJID = proto.String(ref.Chat)
	}
	_, err = cn.SendMessage(ctx, jid, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ctxInfo}})
	return err
}

// SendMentions implements whatsapp.Mentioner: the members m names are mentioned by the JID the
// group knows them by (their "@<number>" in the text becomes "@<that id>", which WhatsApp shows as
// their name), and m.All mentions every member but this number, unseen.
func (c *Client) SendMentions(ctx context.Context, to, text string, m whatsapp.Mentions, ref whatsapp.Ref, quoted string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	jid, err := jidOf(to)
	if err != nil {
		return err
	}
	c.mu.Lock()
	groups, dev := c.groups, c.dev
	c.mu.Unlock()
	info := groupInfo{}
	if groups != nil && jid.Server == types.GroupServer {
		info = groups.of(ctx, cn, jid, membersAge)
	}
	ctxInfo := &waE2E.ContextInfo{}
	seen := map[string]bool{}
	mention := func(j types.JID) {
		if s := j.String(); !seen[s] {
			seen[s] = true
			ctxInfo.MentionedJID = append(ctxInfo.MentionedJID, s)
		}
	}
	for _, n := range m.Numbers {
		member, ok := info.members[n]
		switch {
		case len(info.members) == 0:
			member = types.NewJID(n, types.DefaultUserServer) // the members could not be asked
		case !ok:
			continue // not a member: not mentioned (their number stays as text)
		}
		if member.User != n {
			text = replaceTag(text, n, member.User)
		}
		mention(member)
	}
	if m.All {
		own := map[string]bool{}
		if dev != nil {
			own[dev.GetJID().User], own[dev.GetLID().User] = true, true
		}
		phones := make([]string, 0, len(info.members))
		for n := range info.members {
			phones = append(phones, n)
		}
		sort.Strings(phones)
		for _, n := range phones {
			if j := info.members[n]; !own[n] && !own[j.User] {
				mention(j)
			}
		}
	}
	if ref.ID != "" {
		ctxInfo.StanzaID = proto.String(ref.ID)
		ctxInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String(quoted)}
		if ref.Sender != "" {
			ctxInfo.Participant = proto.String(ref.Sender)
		}
		if ref.Chat != "" {
			ctxInfo.RemoteJID = proto.String(ref.Chat)
		}
	}
	if len(ctxInfo.MentionedJID) == 0 && ref.ID == "" {
		_, err = cn.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)})
		return err
	}
	_, err = cn.SendMessage(ctx, jid, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ctxInfo}})
	return err
}

// replaceTag writes "@from" of a text as "@to", only where it is a whole tag.
func replaceTag(text, from, to string) string {
	var out strings.Builder
	tag := "@" + from
	for {
		i := strings.Index(text, tag)
		if i < 0 {
			out.WriteString(text)
			return out.String()
		}
		end := i + len(tag)
		out.WriteString(text[:i])
		if end < len(text) && text[end] >= '0' && text[end] <= '9' {
			out.WriteString(tag)
		} else {
			out.WriteString("@" + to)
		}
		text = text[end:]
	}
}

// refOf is the chat and the sender of a message the bridge points at.
func refOf(ref whatsapp.Ref) (chat, sender types.JID, err error) {
	if ref.ID == "" {
		return chat, sender, errors.New("no message to point at")
	}
	if chat, err = types.ParseJID(ref.Chat); err != nil {
		return chat, sender, err
	}
	if ref.Sender != "" {
		sender, err = types.ParseJID(ref.Sender)
	}
	return chat, sender, err
}

// MarkRead implements whatsapp.Client.
func (c *Client) MarkRead(ctx context.Context, ref whatsapp.Ref) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	chat, sender, err := refOf(ref)
	if err != nil {
		return err
	}
	return cn.MarkRead(ctx, []types.MessageID{ref.ID}, time.Now(), chat, sender)
}

// React implements whatsapp.Client.
func (c *Client) React(ctx context.Context, ref whatsapp.Ref, emoji string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	chat, sender, err := refOf(ref)
	if err != nil {
		return err
	}
	_, err = cn.SendMessage(ctx, chat, cn.BuildReaction(chat, sender, ref.ID, emoji))
	return err
}

// Typing implements whatsapp.Client.
func (c *Client) Typing(ctx context.Context, to string, on, audio bool) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	jid, err := jidOf(to)
	if err != nil {
		return err
	}
	state := types.ChatPresencePaused
	if on {
		state = types.ChatPresenceComposing
	}
	media := types.ChatPresenceMediaText
	if audio {
		media = types.ChatPresenceMediaAudio
	}
	return cn.SendChatPresence(ctx, jid, state, media)
}

// Logout implements whatsapp.Client.
func (c *Client) Logout(ctx context.Context) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	return cn.Logout(ctx)
}

// QRImage draws a code as a PNG data URL, for whatsapp.Options.QRImage.
func QRImage(code string) (string, error) {
	q, err := qr.Encode(code, qr.M)
	if err != nil {
		return "", err
	}
	q.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(q.PNG()), nil
}
