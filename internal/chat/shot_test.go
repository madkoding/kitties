package chat

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/madkoding/kitties/internal/provider"
)

func pngOf(t *testing.T, w, h int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{40, 40, 60, 255}
			if noisy { // a photo for a wallpaper
				c = color.RGBA{uint8(x + r.Intn(40)), uint8(y + r.Intn(40)), uint8(r.Intn(255)), 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestForModelSendsAPhotoAsJPEGOfTheSameSize(t *testing.T) {
	shot := pngOf(t, 320, 200, true)
	got := forModel(shot)
	if got.MIME != "image/jpeg" || len(got.Data) >= len(shot)*4/5 {
		t.Fatalf("a busy screen goes as JPEG and lighter: %s, %d of %d bytes", got.MIME, len(got.Data), len(shot))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(got.Data))
	if err != nil || cfg.Width != 320 || cfg.Height != 200 {
		t.Errorf("the size must stay the screen's, for the coordinates: %+v %v", cfg, err)
	}
}

func TestForModelKeepsAPNGThatIsAlreadyLight(t *testing.T) {
	shot := pngOf(t, 320, 200, false)
	if got := forModel(shot); got.MIME != "image/png" || !bytes.Equal(got.Data, shot) {
		t.Errorf("a flat screen is lighter as PNG: %s %d", got.MIME, len(got.Data))
	}
	if got := forModel([]byte("not a picture")); got.MIME != "image/png" {
		t.Errorf("what cannot be read goes as it came: %s", got.MIME)
	}
}

func TestImageTokensByPixels(t *testing.T) {
	for _, c := range []struct {
		data []byte
		want int
	}{
		{pngOf(t, 100, 100, false), imageTokensMin},
		{pngOf(t, 1280, 800, false), 1280 * 800 / pixelsPerToken},
		{pngOf(t, 2560, 1600, false), imageTokensMax},
		{[]byte("PNG"), imageTokensMax},
	} {
		if got := imageTokens(provider.Image{Data: c.data}); got != c.want {
			t.Errorf("imageTokens = %d, want %d", got, c.want)
		}
	}
}

// Small pictures weigh what they are: many crops fit where as many full screens would not.
func TestFitContextWeighsPicturesByTheirPixels(t *testing.T) {
	small := provider.Image{MIME: "image/png", Data: pngOf(t, 100, 100, false)}
	msgs := []provider.Message{{Role: "system", Content: "s"}}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: "crop", Images: []provider.Image{small}})
	}
	if got := fitContext(msgs, 3000); len(got) != len(msgs) {
		t.Errorf("ten small crops (~850 tokens) fit in 2,400: %d of %d kept", len(got), len(msgs))
	}
}
