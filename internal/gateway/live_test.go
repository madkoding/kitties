package gateway

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/rects"
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

// jpegOf is a w x h picture with a bright square at (x, y), as the display's stream writes it.
func jpegOf(t *testing.T, x, y int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{20, 24, 40, 255}), image.Point{}, draw.Src)
	if x >= 0 {
		draw.Draw(img, image.Rect(x, y, x+8, y+8), image.NewUniform(color.RGBA{240, 200, 60, 255}), image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func frameOf(b []byte) string { return fmt.Sprintf("FRAME %d\n%s", len(b), b) }

// The display as what changed on it: its size, the whole picture, then only the rectangles.
func TestLiveRects(t *testing.T) {
	srv, f, _ := setup(t)
	f.stream = "PING\n" + frameOf(jpegOf(t, -1, 0)) + "PING\n" + frameOf(jpegOf(t, -1, 0)) +
		"FRAME 4\nnope" + frameOf(jpegOf(t, 100, 70))
	req, _ := http.NewRequest("GET", srv.URL+"/v1/kitties/ana/rects?fps=12&quality=60&token=tok", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != rectsType {
		t.Fatalf("rects = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	r := bufio.NewReader(res.Body)
	var msgs []rects.Message
	for {
		m, err := rects.Read(r)
		if err != nil {
			break
		}
		msgs = append(msgs, m)
	}
	// The size, the whole picture, and one tile for the square: the repeated and the broken
	// pictures sent nothing.
	if len(msgs) != 3 || msgs[0].Screen == nil || *msgs[0].Screen != [2]int{320, 200} {
		t.Fatalf("messages = %+v", msgs)
	}
	if len(msgs[1].Rects) != 1 || msgs[1].Rects[0].W != 320 || msgs[1].Rects[0].H != 200 {
		t.Errorf("the first update = %+v", msgs[1].Rects)
	}
	if len(msgs[2].Rects) != 1 || msgs[2].Rects[0].X != 64 || msgs[2].Rects[0].Y != 64 || msgs[2].Rects[0].W != 64 {
		t.Errorf("the second update = %+v", msgs[2].Rects)
	}
	if !strings.Contains(strings.Join(f.calls, ","), "stream 12 60") {
		t.Errorf("calls = %v", f.calls)
	}
	// What is not a picture is a bad gateway, as on /stream; an unknown kitty is not found.
	f.stream = "garbage\n"
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/rects", "tok", nil); code != http.StatusBadGateway {
		t.Errorf("garbage = %d", code)
	}
	f.stream = "FRAME 4\nnope"
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/rects", "tok", nil); code != http.StatusBadGateway {
		t.Errorf("a first picture that is not one = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/rects", "tok", nil); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/rects", "", nil); code != 401 {
		t.Errorf("no token = %d", code)
	}
}
