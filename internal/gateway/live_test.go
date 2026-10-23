package gateway

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// The display as motion JPEG: each picture a part, with its type; the clipboard both ways.
func TestLiveStreamAndClipboard(t *testing.T) {
	srv, f, _ := setup(t)
	req, _ := http.NewRequest("GET", srv.URL+"/v1/kitties/ana/stream?fps=12&quality=99&token=tok", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	media, params, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != 200 || media != "multipart/x-mixed-replace" {
		t.Fatalf("stream = %d %s", res.StatusCode, media)
	}
	parts := multipart.NewReader(res.Body, params["boundary"])
	var got []string
	for {
		p, err := parts.NextPart()
		if err != nil {
			break
		}
		data, _ := io.ReadAll(p)
		got = append(got, p.Header.Get("Content-Type")+":"+string(data))
	}
	if len(got) != 2 || got[0] != "image/png:\x89PNG\r\n\x1a\n" || !strings.HasSuffix(got[1], ":abc") {
		t.Errorf("parts = %q", got)
	}
	// fps 12 is kept; quality 99 is out of bounds and falls back to 70.
	if !strings.Contains(strings.Join(f.calls, ","), "stream 12 70") {
		t.Errorf("calls = %v", f.calls)
	}
	// A desktop that sends nothing usable is a bad gateway; an unknown kitty is not found.
	f.stream = "garbage\n"
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/stream", "tok", nil); code != http.StatusBadGateway {
		t.Errorf("garbage = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/stream", "tok", nil); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/stream", "", nil); code != 401 {
		t.Errorf("no token = %d", code)
	}

	if code, _, _ := call(t, srv, "PUT", "/v1/kitties/ana/clipboard", "tok", map[string]any{"text": "hola"}); code != 200 || f.clip != "hola" {
		t.Errorf("set clipboard = %d %q", code, f.clip)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/clipboard", "tok", nil); code != 200 || m["text"] != "hola" {
		t.Errorf("clipboard = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/kitties/ana/clipboard", "tok", "nope"); code != 400 {
		t.Error("bad body")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/clipboard", "tok", nil); code != 404 {
		t.Error("unknown clipboard")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/kitties/zz/clipboard", "tok", map[string]any{"text": "x"}); code != 404 {
		t.Error("unknown set clipboard")
	}
	if bounded("x", 8, 1, 15) != 8 || bounded("0", 8, 1, 15) != 8 || bounded("15", 8, 1, 15) != 15 {
		t.Error("bounded")
	}
}
