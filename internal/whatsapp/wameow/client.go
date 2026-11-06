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

	mu   sync.Mutex
	conn conn
}

var _ whatsapp.Client = (*Client)(nil)

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
	c.mu.Lock()
	c.conn = cn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()
	over := make(chan struct{}, 1)
	names := groupNames{names: map[types.JID]string{}}
	cn.AddEventHandler(func(evt any) {
		evs := translate(st, dev, evt)
		if m, ok := evt.(*events.Message); ok && len(evs) == 1 {
			evs[0].Files = media(ctx, cn, m.Message)
			if m.Info.IsGroup {
				evs[0].ChatName = names.of(ctx, cn, m.Info.Chat.ToNonAD())
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

// groupNames are the names of the groups that wrote, asked once each.
type groupNames struct {
	mu    sync.Mutex
	names map[types.JID]string
}

func (g *groupNames) of(ctx context.Context, cn conn, jid types.JID) string {
	g.mu.Lock()
	name, ok := g.names[jid]
	g.mu.Unlock()
	if ok {
		return name
	}
	gctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if info, err := cn.GetGroupInfo(gctx, jid); err == nil && info != nil {
		name = info.Name
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.names[jid] = name
	return name
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
		msg = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: size}}
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
func (c *Client) Typing(ctx context.Context, to string, on bool) error {
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
	return cn.SendChatPresence(ctx, jid, state, types.ChatPresenceMediaText)
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
