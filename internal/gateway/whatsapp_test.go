package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/whatsapp"
)

type fakeWhatsApp struct {
	st       whatsapp.Status
	applied  *whatsapp.Settings
	applyErr error
	linkErr  error
	reconns  int
	unErr    error
	testErr  error
	links    int
	unlinks  int
	tested   string
}

func (f *fakeWhatsApp) Status() whatsapp.Status { return f.st }
func (f *fakeWhatsApp) Apply(s whatsapp.Settings) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = &s
	f.st.Settings = s
	return nil
}
func (f *fakeWhatsApp) Link() error { f.links++; return f.linkErr }

func (f *fakeWhatsApp) Reconnect() error { f.reconns++; return f.linkErr }
func (f *fakeWhatsApp) Unlink(context.Context) error {
	f.unlinks++
	return f.unErr
}
func (f *fakeWhatsApp) Test(_ context.Context, to string) error {
	f.tested = to
	return f.testErr
}

func TestWhatsAppRoutes(t *testing.T) {
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	for _, r := range [][2]string{{"GET", "/v1/whatsapp"}, {"PUT", "/v1/whatsapp"}, {"POST", "/v1/whatsapp/link"}, {"POST", "/v1/whatsapp/reconnect"}, {"POST", "/v1/whatsapp/unlink"}, {"POST", "/v1/whatsapp/test"}} {
		if code, _, _ := call(t, plain, r[0], r[1], "tok", map[string]any{}); code != 404 {
			t.Errorf("%s %s without WhatsApp = %d", r[0], r[1], code)
		}
		if code, _, _ := call(t, plain, r[0], r[1], "", map[string]any{}); code != 401 {
			t.Errorf("%s %s without a token = %d", r[0], r[1], code)
		}
	}
	f := &fakeWhatsApp{st: whatsapp.Status{State: whatsapp.StateQR, QR: "2@x", CanChange: true}}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithWhatsApp(f))
	defer srv.Close()

	code, body, _ := call(t, srv, "GET", "/v1/whatsapp", "tok", nil)
	if code != 200 || body["state"] != "qr" || body["qr"] != "2@x" || body["enabled"] != false {
		t.Fatalf("get = %d %v", code, body)
	}
	// Turning it on needs the risk accepted.
	on := map[string]any{"enabled": true, "kitty": "mimi", "allow": []string{"+56912345678"}}
	if code, body, _ := call(t, srv, "PUT", "/v1/whatsapp", "tok", on); code != 400 || f.applied != nil {
		t.Fatalf("on without the risk = %d %v", code, body)
	}
	on["accept_risk"] = true
	if code, body, _ := call(t, srv, "PUT", "/v1/whatsapp", "tok", on); code != 200 || body["enabled"] != true || f.applied.Kitty != "mimi" {
		t.Fatalf("on = %d %v", code, body)
	}
	// Once on, a change of the list does not ask again.
	delete(on, "accept_risk")
	on["allow"] = []string{"+56912345678", "+15550001111"}
	if code, _, _ := call(t, srv, "PUT", "/v1/whatsapp", "tok", on); code != 200 || len(f.applied.Allow) != 2 {
		t.Fatalf("change = %d", code)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/whatsapp", "tok", "{"); code != 400 {
		t.Fatalf("bad body = %d", code)
	}
	f.applyErr = errors.New("bad number")
	if code, body, _ := call(t, srv, "PUT", "/v1/whatsapp", "tok", on); code != 400 || body["error"] == nil {
		t.Fatalf("apply error = %d %v", code, body)
	}

	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/link", "tok", nil); code != 200 || f.links != 1 {
		t.Fatalf("link = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/reconnect", "tok", nil); code != 200 || f.reconns != 1 {
		t.Fatalf("reconnect = %d", code)
	}
	f.linkErr = whatsapp.ErrNotOn
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/reconnect", "tok", nil); code != 400 {
		t.Fatalf("reconnect while off = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/link", "tok", nil); code != 400 {
		t.Fatalf("link off = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/unlink", "tok", nil); code != 200 || f.unlinks != 1 {
		t.Fatalf("unlink = %d", code)
	}
	f.unErr = errors.New("disk")
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/unlink", "tok", nil); code != 500 {
		t.Fatalf("unlink error = %d", code)
	}

	if code, body, _ := call(t, srv, "POST", "/v1/whatsapp/test", "tok", map[string]any{"to": "+56912345678"}); code != 200 || body["ok"] != true || f.tested != "+56912345678" {
		t.Fatalf("test = %d %v", code, body)
	}
	f.testErr = whatsapp.ErrNotReady
	if code, body, _ := call(t, srv, "POST", "/v1/whatsapp/test", "tok", map[string]any{"to": "+1"}); code != 200 || body["ok"] != false || body["error"] == "" {
		t.Fatalf("test error = %d %v", code, body)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/test", "tok", "{"); code != 400 {
		t.Fatalf("test bad body = %d", code)
	}
}
