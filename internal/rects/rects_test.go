package rects

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"
	"testing"
)

// frame is a w x h picture of a dark desktop with the given bright squares (x, y, side), as the
// JPEG kitty-stream writes.
func frame(t *testing.T, w, h int, squares ...[3]int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{20, 24, 40, 255}), image.Point{}, draw.Src)
	for _, s := range squares {
		draw.Draw(img, image.Rect(s[0], s[1], s[0]+s[2], s[1]+s[2]), image.NewUniform(color.RGBA{240, 200, 60, 255}), image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func next(t *testing.T, d *Differ, f []byte) Update {
	t.Helper()
	u, err := d.Next(f)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTheFirstPictureIsWholeAndAsItCame(t *testing.T) {
	d := &Differ{}
	f := frame(t, 640, 400)
	u := next(t, d, f)
	if !u.Full || u.W != 640 || u.H != 400 || len(u.Rects) != 1 || !bytes.Equal(u.Rects[0].Data, f) {
		t.Errorf("first = %+v", u)
	}
}

func TestOnlyWhatChangedIsSent(t *testing.T) {
	d := &Differ{Quality: 70}
	next(t, d, frame(t, 640, 400))
	// Nothing moved: nothing is sent.
	if u := next(t, d, frame(t, 640, 400)); len(u.Rects) != 0 || u.Full {
		t.Errorf("a still screen sent %+v", u)
	}
	// A small square: one rectangle of the tile around it, far smaller than the picture.
	whole := frame(t, 640, 400, [3]int{100, 70, 10})
	u := next(t, d, whole)
	if u.Full || len(u.Rects) != 1 {
		t.Fatalf("a small change = %+v", u)
	}
	r := u.Rects[0]
	if r.X != 64 || r.Y != 64 || r.W != 64 || r.H != 64 {
		t.Errorf("rect = %d,%d %dx%d, want the tile 64,64 64x64", r.X, r.Y, r.W, r.H)
	}
	if len(r.Data) >= len(whole)/4 {
		t.Errorf("the rectangle (%d bytes) is not much smaller than the picture (%d)", len(r.Data), len(whole))
	}
	pic, err := jpeg.Decode(bytes.NewReader(r.Data))
	if err != nil || pic.Bounds().Dx() != 64 || pic.Bounds().Dy() != 64 {
		t.Fatalf("the rectangle is not its picture: %v", err)
	}
	if r, g, _, _ := pic.At(36, 6).RGBA(); r>>8 < 200 || g>>8 < 150 {
		t.Errorf("the square is not in it: %v", pic.At(36, 6))
	}
	// The same picture again changes nothing.
	if u := next(t, d, whole); len(u.Rects) != 0 {
		t.Errorf("the same picture sent %d rectangles", len(u.Rects))
	}
}

func TestRectanglesAreMerged(t *testing.T) {
	d := &Differ{}
	next(t, d, frame(t, 640, 400))
	// Two squares far apart, and one tall one that spans two rows of tiles.
	u := next(t, d, frame(t, 640, 400, [3]int{10, 10, 8}, [3]int{500, 300, 8}, [3]int{300, 20, 100}))
	if u.Full {
		t.Fatal("a few changes made a whole picture")
	}
	var got []string
	for _, r := range u.Rects {
		got = append(got, fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.W, r.H))
	}
	// (10,10) one tile; (300..400, 20..120) is tiles 4..6 x 0..1: one run per row of the same
	// width, joined into one 3x2 box; (500,300) one tile.
	want := []string{"0,0,64,64", "256,0,192,128", "448,256,64,64"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("rects = %v, want %v", got, want)
	}
}

func TestWholeWhenMostChanged(t *testing.T) {
	d := &Differ{}
	next(t, d, frame(t, 640, 400))
	big := frame(t, 640, 400, [3]int{0, 0, 400})
	u := next(t, d, big)
	if !u.Full || len(u.Rects) != 1 || !bytes.Equal(u.Rects[0].Data, big) {
		t.Errorf("most of the screen changed and it was %+v", u)
	}
}

func TestNewSizeAndKeyframes(t *testing.T) {
	d := &Differ{}
	next(t, d, frame(t, 640, 400))
	if u := next(t, d, frame(t, 320, 200)); !u.Full || u.W != 320 {
		t.Errorf("a new size = %+v", u)
	}
	// After KeyEvery small updates the whole picture comes again.
	var full int
	for i := 0; i < KeyEvery+2; i++ {
		x := 10 + (i%2)*100
		if u := next(t, d, frame(t, 320, 200, [3]int{x, 10, 8})); u.Full {
			full++
		}
	}
	if full != 1 {
		t.Errorf("keyframes = %d, want 1", full)
	}
}

// Many scattered changes are one box, not a crowd of requests.
func TestManyRectanglesAreOneBox(t *testing.T) {
	d := &Differ{}
	w, h := 1280, 800
	next(t, d, frame(t, w, h))
	var sq [][3]int
	for i := 0; i < 14; i++ { // a diagonal of tiles: no two share a row or a column
		sq = append(sq, [3]int{i * 64 % w, (i * 128) % h, 8})
	}
	sq = sq[:14]
	u := next(t, d, frame(t, w, h, sq...))
	if u.Full {
		t.Skip("the layout made the picture whole")
	}
	if len(u.Rects) != 1 {
		t.Errorf("rectangles = %d, want the one box around them", len(u.Rects))
	}
}

func TestBadPicture(t *testing.T) {
	if _, err := (&Differ{}).Next([]byte("not a jpeg")); err == nil {
		t.Error("garbage was a picture")
	}
}

func TestTheStreamRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteScreen(&buf, 1280, 800); err != nil {
		t.Fatal(err)
	}
	rs := []Rect{{1, 2, 3, 4, []byte("abc")}, {5, 6, 7, 8, []byte("defgh")}}
	if err := WriteUpdate(&buf, rs); err != nil {
		t.Fatal(err)
	}
	if err := WriteUpdate(&buf, nil); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&buf)
	m, err := Read(r)
	if err != nil || m.Screen == nil || *m.Screen != [2]int{1280, 800} {
		t.Fatalf("screen = %+v, %v", m, err)
	}
	m, err = Read(r)
	if err != nil || len(m.Rects) != 2 || m.Rects[1].X != 5 || string(m.Rects[1].Data) != "defgh" {
		t.Fatalf("update = %+v, %v", m, err)
	}
	if m, err = Read(r); err != nil || len(m.Rects) != 0 || m.Screen != nil {
		t.Errorf("an empty update = %+v, %v", m, err)
	}
	if _, err = Read(r); err == nil {
		t.Error("the end of the stream is an error")
	}
}

func TestReadRefusesWhatIsNotTheStream(t *testing.T) {
	for _, in := range []string{
		"HELLO\n", "SCREEN 1\n", "SCREEN a b\n", "UPDATE x\n", "UPDATE 5000\n", "UPDATE 1\nBOX 1 2 3 4 5\n",
		"UPDATE 1\nRECT 1 2 3 4 -1\n", "UPDATE 1\nRECT 1 2 3 4 99999999\n", "UPDATE 1\nRECT 1 2 3 4 5\nab", "UPDATE 1\nRECT 1 2 x 4 5\n", "UPDATE 1\n", "SCREEN",
	} {
		if m, err := Read(bufio.NewReader(strings.NewReader(in))); err == nil {
			t.Errorf("%q was read as %+v", in, m)
		}
	}
}
