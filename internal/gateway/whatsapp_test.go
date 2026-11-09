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
	st        whatsapp.Status
	applied   *whatsapp.Settings
	applyErr  error
	linkErr   error
	reconns   int
	unErr     error
	testErr   error
	links     int
	unlinks   int
	tested    string
	cleared   int
	resendErr error
	resent    string
	contacts  []whatsapp.Contact
}

func (f *fakeWhatsApp) Status() whatsapp.Status { return f.st }
func (f *fakeWhatsApp) Apply(s whatsapp.Settings, acceptRisk bool) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	if s.Enabled && !f.st.Enabled && !f.st.RiskAccepted && !acceptRisk {
		return whatsapp.ErrRisk
	}
	if acceptRisk {
		f.st.RiskAccepted = true
	}
	f.applied = &s
	f.st.Settings = s
	return nil
}
func (f *fakeWhatsApp) ClearHeld() { f.cleared++; f.st.Held = 0 }
func (f *fakeWhatsApp) Resend(_ context.Context, id string) error {
	f.resent = id
	return f.resendErr
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

func (f *fakeWhatsApp) Contacts() []whatsapp.Contact { return f.contacts }
func (f *fakeWhatsApp) NameContact(number, name string) (whatsapp.Contact, error) {
	n, err := whatsapp.Normalize(number)
	if err != nil {
		return whatsapp.Contact{}, err
	}
	for i, c := range f.contacts {
		if c.Number == n {
			f.contacts[i].Name = name
			return f.contacts[i], nil
		}
	}
	return whatsapp.Contact{}, whatsapp.ErrNoContact
}
func (f *fakeWhatsApp) ForgetContact(number string) error {
	for i, c := range f.contacts {
		if c.Number == number {
			f.contacts = append(f.contacts[:i], f.contacts[i+1:]...)
			return nil
		}
	}
	return whatsapp.ErrNoContact
}

func TestWhatsAppContactRoutes(t *testing.T) {
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	f := &fakeWhatsApp{contacts: []whatsapp.Contact{{Number: "56911111111", PushName: "Ana"}}}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithWhatsApp(f))
	defer srv.Close()
	if code, _, _ := call(t, srv, "GET", "/v1/whatsapp/contacts", "", nil); code != 401 {
		t.Fatalf("without a token = %d", code)
	}
	code, body, _ := call(t, srv, "GET", "/v1/whatsapp/contacts", "tok", nil)
	if list, _ := body["contacts"].([]any); code != 200 || len(list) != 1 || list[0].(map[string]any)["push_name"] != "Ana" {
		t.Fatalf("list = %d %v", code, body)
	}
	if code, body, _ := call(t, srv, "PUT", "/v1/whatsapp/contacts/56911111111", "tok", map[string]any{"name": "Ana Pérez"}); code != 200 || body["name"] != "Ana Pérez" {
		t.Fatalf("name = %d %v", code, body)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/whatsapp/contacts/56900000000", "tok", map[string]any{"name": "x"}); code != 404 {
		t.Fatalf("name an unknown one = %d", code)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/whatsapp/contacts/abc", "tok", map[string]any{"name": "x"}); code != 400 {
		t.Fatalf("bad number = %d", code)
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/whatsapp/contacts/56911111111", "tok", nil); code != 204 || len(f.contacts) != 0 {
		t.Fatalf("forget = %d %v", code, f.contacts)
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/whatsapp/contacts/56911111111", "tok", nil); code != 404 {
		t.Fatalf("forget again = %d", code)
	}
}

func TestWhatsAppRoutes(t *testing.T) {
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	for _, r := range [][2]string{{"GET", "/v1/whatsapp"}, {"PUT", "/v1/whatsapp"}, {"POST", "/v1/whatsapp/link"}, {"POST", "/v1/whatsapp/reconnect"}, {"POST", "/v1/whatsapp/unlink"}, {"POST", "/v1/whatsapp/test"}, {"DELETE", "/v1/whatsapp/held"}, {"POST", "/v1/whatsapp/held/x/resend"}, {"GET", "/v1/whatsapp/contacts"}, {"PUT", "/v1/whatsapp/contacts/569"}, {"DELETE", "/v1/whatsapp/contacts/569"}} {
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

	f.st.Held = 3
	if code, body, _ := call(t, srv, "DELETE", "/v1/whatsapp/held", "tok", nil); code != 200 || f.cleared != 1 || body["held"] != float64(0) {
		t.Fatalf("clear held = %d %v", code, body)
	}

	if code, _, _ := call(t, srv, "POST", "/v1/whatsapp/held/17-3/resend", "tok", nil); code != 200 || f.resent != "17-3" {
		t.Fatalf("resend = %d %q", code, f.resent)
	}
	for err, want := range map[error]int{whatsapp.ErrNoHeld: 404, whatsapp.ErrLimit: 429, whatsapp.ErrNotAllow: 400} {
		f.resendErr = err
		if code, body, _ := call(t, srv, "POST", "/v1/whatsapp/held/1/resend", "tok", nil); code != want || body["error"] == nil {
			t.Fatalf("resend %v = %d %v", err, code, body)
		}
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
