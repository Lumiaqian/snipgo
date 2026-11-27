package gifenc

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func uiFrame(i int) *image.RGBA {
	f := image.NewRGBA(image.Rect(0, 0, 960, 540))
	fill := func(r image.Rectangle, c color.RGBA) { draw.Draw(f, r, image.NewUniform(c), image.Point{}, draw.Src) }
	white := color.RGBA{251, 252, 254, 255}
	ink := color.RGBA{34, 46, 68, 255}
	blue := color.RGBA{48, 99, 215, 255}
	fill(f.Bounds(), white)
	fill(image.Rect(0, 0, 185, 540), ink)
	label := func(x, y int, s string, c color.RGBA) {
		d := font.Drawer{Dst: f, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
		d.DrawString(s)
	}
	for n, s := range []string{"REELGO", "Recordings", "Exports", "Preferences"} {
		label(24, 40+n*44, s, white)
	}
	label(212, 46, "Recording history", ink)
	for x := range 704 {
		v := uint8(x * 70 / 704)
		fill(image.Rect(212+x, 70, 213+x, 173), color.RGBA{46 + v, 82 + v, 180, 255})
	}
	for row := range 8 {
		y := 190 + row*40
		if row == (i/20)%8 {
			fill(image.Rect(200, y, 925, y+35), color.RGBA{215, 226, 247, 255})
		}
		label(218, y+22, "Screen recording - UI walkthrough", ink)
		label(780, y+22, "00:10  MP4", ink)
	}
	fill(image.Rect(212, 510, 212+4*i, 518), blue)
	x, y := 214+(i*7)%660, 200+(i*3)%275
	for j := range 13 {
		fill(image.Rect(x, y+j, x+j/2+1, y+j+1), ink)
	}
	return f
}

func TestUIWalkthrough(t *testing.T) {
	const frames = 150
	src := func(yield func(*image.RGBA) error) error {
		for i := range frames {
			if err := yield(uiFrame(i)); err != nil {
				return err
			}
		}
		return nil
	}
	var encoded bytes.Buffer
	if err := Encode(t.Context(), &encoded, 15, src); err != nil {
		t.Fatal(err)
	}
	size := encoded.Len()
	g, err := gif.DecodeAll(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != frames || size > 4_000_000 {
		t.Fatalf("%d frames, %d bytes", len(g.Image), size)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 960, 540))
	var squared float64
	for i, delta := range g.Image {
		draw.Draw(canvas, delta.Bounds(), delta, delta.Bounds().Min, draw.Over)
		want := uiFrame(i)
		for j := 0; j < len(canvas.Pix); j += 4 {
			for k := range 3 {
				d := float64(canvas.Pix[j+k]) - float64(want.Pix[j+k])
				squared += d * d
			}
		}
	}
	psnr := 10 * math.Log10(255*255*960*540*3*frames/squared)
	if psnr < 34 {
		t.Fatalf("PSNR %.2f dB", psnr)
	}
	t.Logf("960x540, 150 frames, %d bytes, %.2f dB", size, psnr)
}
