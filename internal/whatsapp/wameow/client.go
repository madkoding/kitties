// Package wameow is the WhatsApp connection of the bridge (internal/whatsapp): whatsmeow, the
// WhatsApp Web protocol in Go, as a linked device of the person's phone. Its store (store.go)
// keeps everything in memory and exports it as one blob that the bridge seals with the vault's
// key, so no session is ever written in the clear.
package wameow

import (
	"context"
	"encoding/base64"
	"errors"
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
	cn.AddEventHandler(func(evt any) {
		for _, ev := range translate(st, dev, evt) {
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
		text := e.Message.GetConversation()
		if text == "" {
			text = e.Message.GetExtendedTextMessage().GetText()
		}
		return []whatsapp.Event{{
			Kind: whatsapp.EvMessage, From: phoneOf(st, e.Info.Sender, e.Info.SenderAlt),
			Name: e.Info.PushName, Text: text, ID: e.Info.ID, Group: e.Info.IsGroup,
		}}
	}
	return nil
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

// Send implements whatsapp.Client.
func (c *Client) Send(ctx context.Context, phone, text string) error {
	cn, err := c.current()
	if err != nil {
		return err
	}
	_, err = cn.SendMessage(ctx, types.NewJID(phone, types.DefaultUserServer), &waE2E.Message{Conversation: proto.String(text)})
	return err
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
