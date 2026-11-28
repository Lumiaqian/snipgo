// Package recordsess maps a SnipGo selection onto one recording display.
package recordsess

import (
	"errors"
	"image"
	"math"

	"snipgo/internal/capture"
)

type Region struct {
	Display capture.Display
	Global  image.Rectangle
	Local   image.Rectangle
}

func Select(rect image.Rectangle, displays []capture.Display) (Region, error) {
	var best Region
	area := 0
	for _, d := range displays {
		hit := rect.Intersect(d.Bounds)
		if overlap := hit.Dx() * hit.Dy(); overlap > area && d.Scale > 0 && !math.IsInf(d.Scale, 0) {
			area = overlap
			best = Region{Display: d, Global: hit, Local: hit.Sub(d.Bounds.Min)}
		}
	}
	if area == 0 {
		return Region{}, errors.New("选区所在显示器已断开")
	}
	if best.Local.Dx() < 64 || best.Local.Dy() < 48 {
		return Region{}, errors.New("选区过小，至少 64×48")
	}
	return best, nil
}
