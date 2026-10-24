// Package replay turns the screenshots of a desktop task into an animated GIF: what a kitty did,
// as a short film the person can watch, keep or send. One palette serves every picture (so the
// colours do not flicker), each picture after the first only carries the part of the screen
// that changed, and the pictures are scaled down to a size a message can carry.
package replay

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"sort"
)

// Options say how the film is made.
type Options struct {
	// MaxWidth is the widest a picture is drawn, in pixels (the screen's own when smaller).
	MaxWidth int
	// MaxFrames is the most pictures it holds: more are thinned out evenly, first and last kept.
	MaxFrames int
	// Delay is how long each picture shows, in hundredths of a second; the last one shows 3 times as long.
	Delay int
}

// Default is a film of a task: 800 pixels wide, 60 pictures at most, a picture each 1.2 s.
var Default = Options{MaxWidth: 800, MaxFrames: 60, Delay: 120}

// ErrNoFrames is what a task without screenshots gets.
var ErrNoFrames = errors.New("this task took no screenshots to make a film of")

// GIF makes the film of PNG screenshots, in order. A picture that is not a PNG, or not the size
// of the first, is left out.
func GIF(shots [][]byte, o Options) ([]byte, error) {
	if o.MaxWidth <= 0 {
		o.MaxWidth = Default.MaxWidth
	}
	if o.MaxFrames <= 0 {
		o.MaxFrames = Default.MaxFrames
	}
	if o.Delay <= 0 {
		o.Delay = Default.Delay
	}
	var frames []*image.RGBA
	var size image.Point
	for _, data := range shots {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		b := img.Bounds()
		if size == (image.Point{}) {
			size = b.Size()
		}
		if b.Size() != size {
			continue
		}
		frames = append(frames, scale(img, o.MaxWidth))
	}
	if len(frames) == 0 {
		return nil, ErrNoFrames
	}
	frames = thin(frames, o.MaxFrames)
	pal := palette(frames)
	lookup := nearest(pal)
	out := &gif.GIF{LoopCount: 0}
	var prev *image.Paletted
	for _, f := range frames {
		cur := quantize(f, pal, lookup)
		box := cur.Bounds()
		if prev != nil {
			box = changed(prev, cur)
		}
		if box.Empty() {
			// Nothing moved: the last picture just stays longer.
			out.Delay[len(out.Delay)-1] += o.Delay
			prev = cur
			continue
		}
		frame := image.NewPaletted(box, pal)
		draw.Draw(frame, box, cur, box.Min, draw.Src)
		out.Image = append(out.Image, frame)
		out.Delay = append(out.Delay, o.Delay)
		out.Disposal = append(out.Disposal, gif.DisposalNone)
		prev = cur
	}
	if len(out.Image) == 0 {
		return nil, ErrNoFrames
	}
	// The last picture, the task's result, stays on a while.
	out.Delay[len(out.Delay)-1] = max(out.Delay[len(out.Delay)-1], o.Delay*3)
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, out); err != nil {
		return nil, fmt.Errorf("writing the film: %w", err)
	}
	return buf.Bytes(), nil
}

// thin keeps at most n frames, evenly spread, the first and the last among them.
func thin(frames []*image.RGBA, n int) []*image.RGBA {
	if n == 1 {
		return frames[len(frames)-1:]
	}
	if len(frames) <= n {
		return frames
	}
	out := make([]*image.RGBA, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, frames[i*(len(frames)-1)/(n-1)])
	}
	return out
}

// scale draws a picture no wider than max, averaging the pixels each new one covers.
func scale(src image.Image, max int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	nw, nh := max, max*h/w
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, max2((y+1)*h/nh, y*h/nh+1)
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, max2((x+1)*w/nw, x*w/nw+1)
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// A bin is a colour of 5 bits a channel, and how many pixels have it.
type bin struct {
	r, g, b uint8 // 0..31
	n       int
}

// palette is 256 colours for all the pictures: median cut over a histogram of the pixels.
func palette(frames []*image.RGBA) color.Palette {
	var hist [32768]int
	for _, f := range frames {
		for i := 0; i+3 < len(f.Pix); i += 4 {
			hist[int(f.Pix[i]>>3)<<10|int(f.Pix[i+1]>>3)<<5|int(f.Pix[i+2]>>3)]++
		}
	}
	var all []bin
	for k, n := range hist {
		if n > 0 {
			all = append(all, bin{uint8(k >> 10), uint8(k >> 5 & 31), uint8(k & 31), n})
		}
	}
	boxes := [][]bin{all}
	for len(boxes) < 256 {
		// Split the box that spans the widest range of one channel and holds more than one colour.
		best, axis, span := -1, 0, 0
		for i, box := range boxes {
			if len(box) < 2 {
				continue
			}
			if a, s := widest(box); s > span {
				best, axis, span = i, a, s
			}
		}
		if best < 0 {
			break
		}
		lo, hi := split(boxes[best], axis)
		boxes[best] = lo
		boxes = append(boxes, hi)
	}
	pal := make(color.Palette, 0, len(boxes))
	for _, box := range boxes {
		var r, g, b, n int
		for _, c := range box {
			r, g, b, n = r+int(c.r)*c.n, g+int(c.g)*c.n, b+int(c.b)*c.n, n+c.n
		}
		if n == 0 {
			continue
		}
		pal = append(pal, color.RGBA{expand(r / n), expand(g / n), expand(b / n), 255})
	}
	return pal
}

// expand takes 5 bits to 8, the middle of the bin.
func expand(v int) uint8 { return uint8(v<<3 | 4) }

func channel(c bin, axis int) uint8 { return [3]uint8{c.r, c.g, c.b}[axis] }

// widest is the channel in which the box's colours are the furthest apart, and how far.
func widest(box []bin) (axis, span int) {
	for a := 0; a < 3; a++ {
		lo, hi := uint8(255), uint8(0)
		for _, c := range box {
			v := channel(c, a)
			lo, hi = min(lo, v), max(hi, v)
		}
		if s := int(hi) - int(lo); s > span {
			axis, span = a, s
		}
	}
	return axis, span
}

// split cuts a box at the pixel count's middle along one channel.
func split(box []bin, axis int) (lo, hi []bin) {
	sort.Slice(box, func(i, j int) bool { return channel(box[i], axis) < channel(box[j], axis) })
	total := 0
	for _, c := range box {
		total += c.n
	}
	run := 0
	for i, c := range box {
		run += c.n
		if run*2 >= total {
			cut := i + 1
			if cut >= len(box) {
				cut = len(box) - 1
			}
			return box[:cut], box[cut:]
		}
	}
	return box[:1], box[1:]
}

// nearest tells, for each colour of 5 bits a channel, the closest colour of the palette.
func nearest(pal color.Palette) *[32768]uint8 {
	var table [32768]uint8
	for k := range table {
		r, g, b := int(expand(k>>10)), int(expand(k>>5&31)), int(expand(k&31))
		best, dist := 0, 1<<30
		for i, c := range pal {
			pr, pg, pb, _ := c.RGBA()
			dr, dg, db := r-int(pr>>8), g-int(pg>>8), b-int(pb>>8)
			if d := dr*dr + dg*dg + db*db; d < dist {
				best, dist = i, d
			}
		}
		table[k] = uint8(best)
	}
	return &table
}

func quantize(f *image.RGBA, pal color.Palette, table *[32768]uint8) *image.Paletted {
	out := image.NewPaletted(f.Bounds(), pal)
	for i, j := 0, 0; i+3 < len(f.Pix); i, j = i+4, j+1 {
		out.Pix[j] = table[int(f.Pix[i]>>3)<<10|int(f.Pix[i+1]>>3)<<5|int(f.Pix[i+2]>>3)]
	}
	return out
}

// changed is the smallest rectangle that holds every pixel that differs between two pictures.
func changed(a, b *image.Paletted) image.Rectangle {
	box := image.Rectangle{}
	r := b.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := (y - r.Min.Y) * b.Stride
		for x := r.Min.X; x < r.Max.X; x++ {
			if a.Pix[row+x-r.Min.X] != b.Pix[row+x-r.Min.X] {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}
