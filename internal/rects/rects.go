// Package rects makes a live view of a display out of what changed on it, the way VNC does: not
// the whole picture again and again, but the rectangles that are different from the one before.
// A quiet screen with a blinking cursor costs a few hundred bytes a frame instead of the whole
// picture, so a view can follow the display faster, over a slower link, and the browser paints
// only what moved.
//
// A Differ is given the display's pictures (JPEG, as kitty-stream writes them) and tells, for
// each, the rectangles to draw over the last (a PNG is read as well). The messages that carry them are text lines with
// binary bodies, written by WriteScreen and WriteUpdate and read by Read:
//
//	SCREEN <width> <height>
//	UPDATE <count>
//	RECT <x> <y> <width> <height> <bytes>      (count times, each followed by its JPEG)
package rects

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // the demo draws its display as PNG
	"io"
	"strconv"
	"strings"
)

// Rect is a part of the display and its picture (a JPEG).
type Rect struct {
	X, Y, W, H int
	Data       []byte
}

// Update is what to draw over the last picture to get the next.
type Update struct {
	// W and H are the display's size.
	W, H int
	// Full is true when Rects is the whole display (the first picture, a new size, a keyframe, or
	// so much changed that it was cheaper to send it all).
	Full  bool
	Rects []Rect
}

// Tile is the side of the squares that are compared: a multiple of JPEG's 16-pixel blocks, so a
// change in one tile does not smudge its neighbours.
const Tile = 64

// KeyEvery is how many updates pass before the whole picture is sent again, so what the rounding
// of many small JPEGs left behind never stays for long.
const KeyEvery = 300

// maxRects is how many rectangles an update may have: past it they are one, the box around them.
const maxRects = 12

// wholeAbove is the share of the display, in tiles, past which an update is the whole picture.
const wholeAbove = 0.5

// Differ turns a sequence of pictures into updates.
type Differ struct {
	// Quality is the JPEG quality of the rectangles (1-100; 75 when zero).
	Quality int

	prev  *image.RGBA
	since int
}

// Next compares a picture with the one before. An update with no rectangles means nothing changed.
func (d *Differ) Next(frame []byte) (Update, error) {
	img, _, err := image.Decode(bytes.NewReader(frame))
	if err != nil {
		return Update{}, fmt.Errorf("a picture of the display: %w", err)
	}
	b := img.Bounds()
	cur := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(cur, cur.Bounds(), img, b.Min, draw.Src)
	w, h := cur.Bounds().Dx(), cur.Bounds().Dy()
	whole := Update{W: w, H: h, Full: true, Rects: []Rect{{0, 0, w, h, frame}}}
	if d.prev == nil || d.prev.Bounds() != cur.Bounds() || d.since >= KeyEvery {
		d.prev, d.since = cur, 0
		return whole, nil
	}
	changed, count := d.changedTiles(cur)
	d.prev = cur
	if count == 0 {
		return Update{W: w, H: h}, nil
	}
	d.since++
	cols := (w + Tile - 1) / Tile
	if float64(count) > wholeAbove*float64(len(changed)) {
		d.since = 0
		return whole, nil
	}
	boxes := merge(changed, cols, w, h)
	q := d.Quality
	if q <= 0 {
		q = 75
	}
	up := Update{W: w, H: h}
	for _, r := range boxes {
		var buf bytes.Buffer
		sub := cur.SubImage(image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H))
		if err := jpeg.Encode(&buf, sub, &jpeg.Options{Quality: q}); err != nil {
			return Update{}, err
		}
		r.Data = buf.Bytes()
		up.Rects = append(up.Rects, r)
	}
	return up, nil
}

// changedTiles marks the tiles of cur that differ from the last picture, row by row, and
// counts them.
func (d *Differ) changedTiles(cur *image.RGBA) ([]bool, int) {
	w, h := cur.Bounds().Dx(), cur.Bounds().Dy()
	cols, rows := (w+Tile-1)/Tile, (h+Tile-1)/Tile
	changed := make([]bool, cols*rows)
	count := 0
	for ty := 0; ty < rows; ty++ {
		for tx := 0; tx < cols; tx++ {
			x0, y0 := tx*Tile, ty*Tile
			x1, y1 := min(x0+Tile, w), min(y0+Tile, h)
			for y := y0; y < y1; y++ {
				off := y*cur.Stride + x0*4
				end := y*cur.Stride + x1*4
				if !bytes.Equal(cur.Pix[off:end], d.prev.Pix[off:end]) {
					changed[ty*cols+tx] = true
					count++
					break
				}
			}
		}
	}
	return changed, count
}

// merge makes the fewest rectangles of the changed tiles that are easy to find: runs of tiles in
// a row, and runs of the same width in rows that follow each other. Past maxRects, one box.
func merge(changed []bool, cols, w, h int) []Rect {
	rows := len(changed) / cols
	type run struct{ x0, x1, y0, y1 int } // in tiles; x1 and y1 exclusive
	var runs []run
	open := map[[2]int]int{} // (x0, x1) -> index in runs of the run ending on the previous row
	for ty := 0; ty < rows; ty++ {
		next := map[[2]int]int{}
		for tx := 0; tx < cols; {
			if !changed[ty*cols+tx] {
				tx++
				continue
			}
			x0 := tx
			for tx < cols && changed[ty*cols+tx] {
				tx++
			}
			key := [2]int{x0, tx}
			if i, ok := open[key]; ok {
				runs[i].y1 = ty + 1
				next[key] = i
			} else {
				runs = append(runs, run{x0, tx, ty, ty + 1})
				next[key] = len(runs) - 1
			}
		}
		open = next
	}
	px := func(r run) Rect {
		return Rect{X: r.x0 * Tile, Y: r.y0 * Tile, W: min(r.x1*Tile, w) - r.x0*Tile, H: min(r.y1*Tile, h) - r.y0*Tile}
	}
	if len(runs) > maxRects {
		all := px(runs[0])
		x1, y1 := all.X+all.W, all.Y+all.H
		for _, r := range runs[1:] {
			p := px(r)
			all.X, all.Y = min(all.X, p.X), min(all.Y, p.Y)
			x1, y1 = max(x1, p.X+p.W), max(y1, p.Y+p.H)
		}
		all.W, all.H = x1-all.X, y1-all.Y
		return []Rect{all}
	}
	out := make([]Rect, 0, len(runs))
	for _, r := range runs {
		out = append(out, px(r))
	}
	return out
}

// WriteScreen tells the reader the display's size (first, and whenever it changes).
func WriteScreen(w io.Writer, width, height int) error {
	_, err := fmt.Fprintf(w, "SCREEN %d %d\n", width, height)
	return err
}

// WriteUpdate writes the rectangles of an update.
func WriteUpdate(w io.Writer, rs []Rect) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "UPDATE %d\n", len(rs))
	for _, r := range rs {
		fmt.Fprintf(bw, "RECT %d %d %d %d %d\n", r.X, r.Y, r.W, r.H, len(r.Data))
		bw.Write(r.Data)
	}
	return bw.Flush()
}

// Message is one thing a stream says: the display's size, or rectangles to draw.
type Message struct {
	Screen *[2]int
	Rects  []Rect
}

// maxRect is the largest picture of a rectangle.
const maxRect = 8 << 20

// Read reads the next message of a stream.
func Read(r *bufio.Reader) (Message, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return Message{}, err
	}
	f := strings.Fields(line)
	switch {
	case len(f) == 3 && f[0] == "SCREEN":
		n, err := numbers(f[1:])
		if err != nil {
			return Message{}, err
		}
		return Message{Screen: &[2]int{n[0], n[1]}}, nil
	case len(f) == 2 && f[0] == "UPDATE":
		n, err := numbers(f[1:])
		if err != nil || n[0] < 0 || n[0] > 1024 {
			return Message{}, errors.New("an update of a size out of bounds")
		}
		m := Message{}
		for i := 0; i < n[0]; i++ {
			head, err := r.ReadString('\n')
			if err != nil {
				return Message{}, err
			}
			h := strings.Fields(head)
			if len(h) != 6 || h[0] != "RECT" {
				return Message{}, fmt.Errorf("not a rectangle: %.40q", head)
			}
			v, err := numbers(h[1:])
			if err != nil || v[4] < 0 || v[4] > maxRect {
				return Message{}, errors.New("a rectangle of a size out of bounds")
			}
			data := make([]byte, v[4])
			if _, err := io.ReadFull(r, data); err != nil {
				return Message{}, err
			}
			m.Rects = append(m.Rects, Rect{v[0], v[1], v[2], v[3], data})
		}
		return m, nil
	}
	return Message{}, fmt.Errorf("not a message of the stream: %.40q", line)
}

func numbers(fields []string) ([]int, error) {
	out := make([]int, len(fields))
	for i, s := range fields {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		out[i] = n
	}
	return out, nil
}
