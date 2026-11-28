// Package gifenc encodes replayable streams of opaque screen frames.
package gifenc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"sort"
)

// Source replays the same frames in order on each invocation.
type Source func(yield func(*image.RGBA) error) error

type bin struct {
	rgb   [3]int
	count int
}

func palette(hist map[int]int) color.Palette {
	var bins []bin
	for key, n := range hist {
		bins = append(bins, bin{[3]int{(key>>10)*8 + 4, (key>>5&31)*8 + 4, (key&31)*8 + 4}, n})
	}
	boxes := [][]bin{bins}
	for len(boxes) < 255 {
		best, axis, score := -1, 0, 0
		for i, box := range boxes {
			for a := range 3 {
				lo, hi := 255, 0
				for _, b := range box {
					lo = min(lo, b.rgb[a])
					hi = max(hi, b.rgb[a])
				}
				if s := (hi - lo) * len(box); s > score {
					best, axis, score = i, a, s
				}
			}
		}
		if best < 0 {
			break
		}
		box := boxes[best]
		sort.Slice(box, func(i, j int) bool {
			for k := range 3 {
				a := (axis + k) % 3
				if box[i].rgb[a] != box[j].rgb[a] {
					return box[i].rgb[a] < box[j].rgb[a]
				}
			}
			return false
		})
		total := 0
		for _, b := range box {
			total += b.count
		}
		cut, sum := 0, 0
		for cut < len(box)-1 && sum*2 < total {
			sum += box[cut].count
			cut++
		}
		boxes[best] = box[:cut]
		boxes = append(boxes, box[cut:])
	}
	p := color.Palette{color.Transparent}
	for _, box := range boxes {
		var sums [3]int
		n := 0
		for _, b := range box {
			for k := range 3 {
				sums[k] += b.rgb[k] * b.count
			}
			n += b.count
		}
		p = append(p, color.RGBA{uint8(sums[0] / n), uint8(sums[1] / n), uint8(sums[2] / n), 255})
	}
	return p
}

func key(c color.RGBA) int { return int(c.R>>3)<<10 | int(c.G>>3)<<5 | int(c.B>>3) }

// Encode writes a looping GIF. On error callers must discard partial output.
func Encode(ctx context.Context, w io.Writer, fps int, src Source) error {
	if fps < 1 || fps > 100 {
		return errors.New("invalid frame rate")
	}
	hist := map[int]int{}
	var size image.Point
	count := 0
	err := src(func(f *image.RGBA) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f == nil || f.Rect.Empty() || f.Rect.Dx() > 65535 || f.Rect.Dy() > 65535 {
			return errors.New("invalid frame")
		}
		if count == 0 {
			size = f.Rect.Size()
		}
		if size != f.Rect.Size() {
			return errors.New("frame size changed")
		}
		for y := f.Rect.Min.Y; y < f.Rect.Max.Y; y++ {
			for x := f.Rect.Min.X; x < f.Rect.Max.X; x += 4 {
				hist[key(f.RGBAAt(x, y))]++
			}
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("no frames")
	}
	p := palette(hist)
	lookup := map[int]uint8{}
	prev := make([]uint8, size.X*size.Y)
	index := 0
	err = src(func(f *image.RGBA) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f == nil || size != f.Rect.Size() || index >= count {
			return errors.New("source changed during replay")
		}
		frame := image.NewPaletted(image.Rectangle{Max: size}, p)
		dirty := image.Rectangle{}
		for y := range size.Y {
			for x := range size.X {
				c := f.RGBAAt(f.Rect.Min.X+x, f.Rect.Min.Y+y)
				k := key(c)
				v := lookup[k]
				if v == 0 {
					v = uint8(1 + p[1:].Index(color.RGBA{uint8(k>>10)*8 + 4, uint8(k>>5&31)*8 + 4, uint8(k&31)*8 + 4, 255}))
					lookup[k] = v
				}
				i := y*size.X + x
				if prev[i] != v {
					prev[i] = v
					frame.Pix[i] = v
					dirty = dirty.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if dirty.Empty() {
			dirty = image.Rect(0, 0, 1, 1)
		}
		var buf bytes.Buffer
		delay := ((index+1)*100+fps/2)/fps - (index*100+fps/2)/fps
		g := gif.GIF{Config: image.Config{ColorModel: p, Width: size.X, Height: size.Y}, Image: []*image.Paletted{frame.SubImage(dirty).(*image.Paletted)}, Delay: []int{delay}, Disposal: []byte{gif.DisposalNone}}
		if err := gif.EncodeAll(&buf, &g); err != nil {
			return err
		}
		// Each standalone frame shares the logical screen descriptor and color table.
		data := buf.Bytes()
		header := 13 + 3*(2<<(data[10]&7))
		if index == 0 {
			if _, err := w.Write(data[:header]); err != nil {
				return err
			}
			if _, err := io.WriteString(w, "\x21\xff\x0bNETSCAPE2.0\x03\x01\x00\x00\x00"); err != nil {
				return err
			}
		}
		index++
		_, err := w.Write(data[header : len(data)-1])
		return err
	})
	if err != nil {
		return err
	}
	if index != count {
		return errors.New("incomplete replay")
	}
	_, err = io.WriteString(w, ";")
	return err
}
