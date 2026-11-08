package chat

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png" // screenshots come as PNG

	"github.com/madkoding/kitties/internal/provider"
)

// shotQuality is the JPEG quality a screenshot goes to the model with: text stays legible.
const shotQuality = 85

// forModel is a screenshot as the model gets it. A desktop with a photo for a wallpaper weighs
// about a megabyte as PNG and a fifth of that as JPEG, so it goes as JPEG when that is clearly
// lighter. The size stays the same, so the coordinates the model clicks on are the screen's own.
// The chat keeps the PNG.
func forModel(png []byte) provider.Image {
	shot := provider.Image{MIME: "image/png", Data: png}
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return shot
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: shotQuality}); err != nil || buf.Len() > len(png)*4/5 {
		return shot
	}
	return provider.Image{MIME: "image/jpeg", Data: buf.Bytes()}
}

// Image tokens: the providers bill a picture by its pixels (about one token per 750) and scale
// big ones down, so a picture costs at most about 1,600 tokens and a small crop much less.
const (
	imageTokensMax = 1600
	imageTokensMin = 85
	pixelsPerToken = 750
)

// imageTokens is what a picture weighs in the context window, by its size; one that cannot be
// read counts as the most.
func imageTokens(img provider.Image) int {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return imageTokensMax
	}
	return min(max(cfg.Width*cfg.Height/pixelsPerToken, imageTokensMin), imageTokensMax)
}
