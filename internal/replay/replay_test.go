package replay

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"testing"
)

// screen is a w x h picture: a dark desktop with a window at (x, y).
func screen(t *testing.T, w, h, x, y int, window color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{20, 24, 40, 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(x, y, x+60, y+40), image.NewUniform(window), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decode(t *testing.T, data []byte) *gif.GIF {
	t.Helper()
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGIFOfATask(t *testing.T) {
	red, blue := color.RGBA{220, 40, 40, 255}, color.RGBA{40, 80, 230, 255}
	shots := [][]byte{
		screen(t, 400, 250, 10, 10, red),
		screen(t, 400, 250, 10, 10, red), // nothing moved: the picture stays longer
		screen(t, 400, 250, 120, 60, blue),
		[]byte("not a png"),
		screen(t, 200, 100, 0, 0, red), // another size
	}
	data, err := GIF(shots, Options{Delay: 50})
	if err != nil {
		t.Fatal(err)
	}
	g := decode(t, data)
	if len(g.Image) != 2 {
		t.Fatalf("frames = %d, want 2 (the repeated one folded into the first)", len(g.Image))
	}
	if b := g.Image[0].Bounds(); b != image.Rect(0, 0, 400, 250) {
		t.Errorf("the first picture is the whole screen, got %v", b)
	}
	// The second only carries what moved: the window's old and new places.
	if b := g.Image[1].Bounds(); b.Dx() >= 400 || b.Dy() >= 250 || b.Empty() {
		t.Errorf("the second picture = %v, want only the part that changed", b)
	}
	if g.Delay[0] != 100 || g.Delay[1] != 150 {
		t.Errorf("delays = %v, want [100 150]: the folded one doubled, the last tripled", g.Delay)
	}
	// What is drawn is what was on the screen: the window is blue at its new place.
	canvas := image.NewRGBA(image.Rect(0, 0, 400, 250))
	for _, f := range g.Image {
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
	}
	r, _, b, _ := canvas.At(140, 80).RGBA()
	if b>>8 < 180 || r>>8 > 100 {
		t.Errorf("the window at its new place is %v", canvas.At(140, 80))
	}
}

func TestGIFScalesAndThins(t *testing.T) {
	var shots [][]byte
	for i := 0; i < 10; i++ {
		shots = append(shots, screen(t, 1280, 800, i*20, 10, color.RGBA{uint8(25 * i), 200, 90, 255}))
	}
	data, err := GIF(shots, Options{MaxWidth: 320, MaxFrames: 4})
	if err != nil {
		t.Fatal(err)
	}
	g := decode(t, data)
	if len(g.Image) != 4 {
		t.Errorf("frames = %d, want 4", len(g.Image))
	}
	if b := g.Image[0].Bounds(); b != image.Rect(0, 0, 320, 200) {
		t.Errorf("size = %v, want 320x200", b)
	}
	// One frame is the last picture alone.
	one, err := GIF(shots, Options{MaxFrames: 1})
	if err != nil || len(decode(t, one).Image) != 1 {
		t.Errorf("a film of one picture: %v", err)
	}
	// A screen narrower than the limit is drawn as it is.
	small, err := GIF(shots[:1], Options{})
	if err != nil || decode(t, small).Image[0].Bounds().Dx() != 800 {
		t.Errorf("default width: %v", err)
	}
}

func TestGIFNeedsPictures(t *testing.T) {
	for _, shots := range [][][]byte{nil, {[]byte("nope")}} {
		if _, err := GIF(shots, Default); !errors.Is(err, ErrNoFrames) {
			t.Errorf("GIF(%d pictures) = %v, want ErrNoFrames", len(shots), err)
		}
	}
}

// Many colours still make a palette of at most 256, and every picture uses it.
func TestPaletteOfManyColours(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data, err := GIF([][]byte{buf.Bytes(), buf.Bytes()}, Default)
	if err != nil {
		t.Fatal(err)
	}
	g := decode(t, data)
	if n := len(g.Image[0].Palette); n < 200 || n > 256 {
		t.Errorf("palette = %d colours", n)
	}
	if len(g.Image) != 1 {
		t.Errorf("two equal pictures make %d frames", len(g.Image))
	}
}
