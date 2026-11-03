package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/attach"
	"github.com/madkoding/kitties/internal/chat"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)

// SendFiles and File make the fake chats take files the way the service does: checked, listed on the
// turn, and handed back by id.
func (c *fakeChats) SendFiles(ctx context.Context, id, text string, files []attach.Upload) ([]chat.Turn, error) {
	if len(files) == 0 {
		return c.Send(ctx, id, text)
	}
	if err := attach.Check(files); err != nil {
		return nil, err
	}
	if c.files == nil {
		c.files = map[string]stored{}
	}
	var list []string
	for i, f := range files {
		a, err := attach.Inspect(f)
		if err != nil {
			return nil, err
		}
		a.ID = strings.Repeat("a", 11) + string(rune('0'+i))
		c.files[a.ID] = stored{a, f.Data}
		list = append(list, a.Name)
	}
	return []chat.Turn{{From: "ana", Text: "got " + strings.Join(list, ",") + " " + text}}, nil
}

type stored struct {
	a    attach.Attachment
	data []byte
}

func (c *fakeChats) File(id, fileID string) (attach.Attachment, []byte, error) {
	if _, err := c.Get(id); err != nil {
		return attach.Attachment{}, nil, err
	}
	f, ok := c.files[fileID]
	if !ok {
		return attach.Attachment{}, nil, chat.ErrNoFile
	}
	return f.a, f.data, nil
}

func file(name string, data []byte) map[string]any {
	return map[string]any{"name": name, "data": base64.StdEncoding.EncodeToString(data)}
}

func TestSendWithFiles(t *testing.T) {
	srv, _, c := setup(t)
	c.sessions["c1"] = &chat.Session{ID: "c1"}
	code, m, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{
		"text": "look", "files": []any{file("../cat.txt", pngBytes), file("notes.md", []byte("# hi"))}})
	if code != 200 || m["turns"].([]any)[0].(map[string]any)["text"] != "got cat.txt,notes.md look" {
		t.Fatalf("%d %v", code, m)
	}
	// A message may be only files.
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{"files": []any{file("a.md", []byte("x"))}}); code != 200 {
		t.Errorf("files alone: %d", code)
	}
	bad := map[string]map[string]any{
		"not base64": {"files": []any{map[string]any{"name": "a", "data": "***"}}},
		"program":    {"files": []any{file("a.txt", []byte("MZ\x90\x00"))}},
		"empty":      {"files": []any{file("a.txt", nil)}},
		"too many":   {"files": []any{file("1", []byte("1")), file("2", []byte("1")), file("3", []byte("1")), file("4", []byte("1")), file("5", []byte("1")), file("6", []byte("1")), file("7", []byte("1"))}},
	}
	for name, body := range bad {
		if code, m, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", body); code != 400 {
			t.Errorf("%s: %d %v", name, code, m)
		}
	}
	// A body over the limit is refused as too big.
	huge := `{"text":"` + strings.Repeat("a", attach.MaxBody) + `"}`
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", huge); code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge body: %d", code)
	}
}

func get(t *testing.T, srv string, path string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv+path, nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestServeAttachedFiles(t *testing.T) {
	srv, _, c := setup(t)
	c.sessions["c1"] = &chat.Session{ID: "c1"}
	body, _ := json.Marshal(map[string]any{"files": []any{
		file("pic.png", pngBytes),
		file("evil.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)),
		file("r.pdf", []byte("%PDF-1.4")),
	}})
	if resp, err := http.Post(srv.URL+"/v1/chats/c1/messages?x=1", "application/json", bytes.NewReader(body)); err != nil || resp.StatusCode != 401 {
		t.Fatalf("no token: %v %v", resp, err)
	}
	call(t, srv, "POST", "/v1/chats/c1/messages", "tok", string(body))

	resp, data := get(t, srv.URL, "/v1/chats/c1/files/aaaaaaaaaaa0")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(data, pngBytes) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("image: %d %v", resp.StatusCode, resp.Header)
	}
	for _, name := range []string{"aaaaaaaaaaa1", "aaaaaaaaaaa2"} {
		resp, _ = get(t, srv.URL, "/v1/chats/c1/files/"+name)
		if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") ||
			!strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
			t.Errorf("%s must only download: %v", name, resp.Header)
		}
	}
	if resp, _ = get(t, srv.URL, "/v1/chats/c1/files/aaaaaaaaaaa9"); resp.StatusCode != 404 {
		t.Errorf("unknown file: %d", resp.StatusCode)
	}
	if resp, _ = get(t, srv.URL, "/v1/chats/nope/files/aaaaaaaaaaa0"); resp.StatusCode != 404 {
		t.Errorf("unknown chat: %d", resp.StatusCode)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/chats/c1/files/aaaaaaaaaaa0", "", nil); code != 401 {
		t.Errorf("files need the token: %d", code)
	}
}

func TestAKittyCannotReadASoulFileThroughTheAPI(t *testing.T) {
	srv, f, _ := setup(t)
	code, m, _ := call(t, srv, "POST", "/v1/kitties", "tok", map[string]any{"name": "zed", "soul_file": "/etc/passwd"})
	if code != http.StatusBadRequest || !strings.Contains(m["error"].(string), "soul_file") {
		t.Fatalf("%d %v", code, m)
	}
	if _, err := f.Get("zed"); err == nil {
		t.Error("the kitty was made")
	}
}
