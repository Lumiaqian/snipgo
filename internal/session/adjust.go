package session

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"snipgo/internal/crop"
)

// Handle 表示鼠标命中选区的哪个控制部位。
type Handle int

const (
	HandleNone Handle = iota
	HandleMove
	HandleNW
	HandleN
	HandleNE
	HandleE
	HandleSE
	HandleS
	HandleSW
	HandleW
)

// handleSlop 是 8 向控制柄的命中半径（DIP）。
const handleSlop = 6

// Loupe 是光标放大镜所需的一块冻结像素采样。
type Loupe struct {
	Side   int           // 网格边长（2*radius+1）
	Grid   []color.NRGBA // 按行排列的 Side*Side 像素，越界处为半透明深灰
	Center color.NRGBA   // 中心像素颜色
	DIP    image.Point   // 全局 DIP 坐标
	Pixel  image.Point   // 所在屏幕内的像素坐标
}

// Hex 返回 #RRGGBB 色值字符串。
func (l Loupe) Hex() string {
	return fmt.Sprintf("#%02X%02X%02X", l.Center.R, l.Center.G, l.Center.B)
}

// RGB 返回 R, G, B 色值字符串。
func (l Loupe) RGB() string {
	return fmt.Sprintf("%d, %d, %d", l.Center.R, l.Center.G, l.Center.B)
}

// HandlePoints 返回选区 8 个控制柄的中心坐标（全局 DIP），顺序与 HandleNW..HandleW 一致。
func HandlePoints(r image.Rectangle) [8]image.Point {
	r = r.Canon()
	mx := (r.Min.X + r.Max.X) / 2
	my := (r.Min.Y + r.Max.Y) / 2
	return [8]image.Point{
		{X: r.Min.X, Y: r.Min.Y}, // NW
		{X: mx, Y: r.Min.Y},      // N
		{X: r.Max.X, Y: r.Min.Y}, // NE
		{X: r.Max.X, Y: my},      // E
		{X: r.Max.X, Y: r.Max.Y}, // SE
		{X: mx, Y: r.Max.Y},      // S
		{X: r.Min.X, Y: r.Max.Y}, // SW
		{X: r.Min.X, Y: my},      // W
	}
}

// hitHandle 判断全局 DIP 点 (gx, gy) 落在选区 r 的哪个控制柄或内部。
// 向外保留完整 handleSlop 便于抓取，向内按选区尺寸收敛，保证小选区中心仍可平移。
func hitHandle(r image.Rectangle, gx, gy float64) Handle {
	r = r.Canon()
	if r.Empty() {
		return HandleNone
	}
	inXSlop := min(float64(handleSlop), max(1.5, float64(r.Dx())/4))
	inYSlop := min(float64(handleSlop), max(1.5, float64(r.Dy())/4))

	nearLeft := gx >= float64(r.Min.X)-handleSlop && gx <= float64(r.Min.X)+inXSlop
	nearRight := gx >= float64(r.Max.X)-inXSlop && gx <= float64(r.Max.X)+handleSlop
	nearTop := gy >= float64(r.Min.Y)-handleSlop && gy <= float64(r.Min.Y)+inYSlop
	nearBottom := gy >= float64(r.Max.Y)-inYSlop && gy <= float64(r.Max.Y)+handleSlop
	spanX := gx >= float64(r.Min.X)-handleSlop && gx <= float64(r.Max.X)+handleSlop
	spanY := gy >= float64(r.Min.Y)-handleSlop && gy <= float64(r.Max.Y)+handleSlop

	switch {
	case nearLeft && nearTop:
		return HandleNW
	case nearRight && nearTop:
		return HandleNE
	case nearRight && nearBottom:
		return HandleSE
	case nearLeft && nearBottom:
		return HandleSW
	case spanX && nearTop:
		return HandleN
	case spanX && nearBottom:
		return HandleS
	case spanY && nearLeft:
		return HandleW
	case spanY && nearRight:
		return HandleE
	case gx >= float64(r.Min.X) && gx < float64(r.Max.X) && gy >= float64(r.Min.Y) && gy < float64(r.Max.Y):
		return HandleMove
	default:
		return HandleNone
	}
}

func resizeByHandle(start image.Rectangle, h Handle, dx, dy int, bounds image.Rectangle) image.Rectangle {
	x0, y0, x1, y1 := start.Min.X, start.Min.Y, start.Max.X, start.Max.Y
	switch h {
	case HandleNW:
		x0 += dx
		y0 += dy
	case HandleN:
		y0 += dy
	case HandleNE:
		x1 += dx
		y0 += dy
	case HandleE:
		x1 += dx
	case HandleSE:
		x1 += dx
		y1 += dy
	case HandleS:
		y1 += dy
	case HandleSW:
		x0 += dx
		y1 += dy
	case HandleW:
		x0 += dx
	}
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if x1 == x0 {
		x1 = x0 + 1
	}
	if y1 == y0 {
		y1 = y0 + 1
	}
	r := image.Rect(x0, y0, x1, y1)
	if !bounds.Empty() {
		r = r.Intersect(bounds)
		if r.Empty() {
			return start
		}
	}
	return r
}

func moveRect(start image.Rectangle, dx, dy int, bounds image.Rectangle) image.Rectangle {
	r := start.Add(image.Pt(dx, dy))
	if bounds.Empty() {
		return r
	}
	if r.Min.X < bounds.Min.X {
		r = r.Add(image.Pt(bounds.Min.X-r.Min.X, 0))
	}
	if r.Max.X > bounds.Max.X {
		r = r.Add(image.Pt(bounds.Max.X-r.Max.X, 0))
	}
	if r.Min.Y < bounds.Min.Y {
		r = r.Add(image.Pt(0, bounds.Min.Y-r.Min.Y))
	}
	if r.Max.Y > bounds.Max.Y {
		r = r.Add(image.Pt(0, bounds.Max.Y-r.Max.Y))
	}
	return r.Intersect(bounds)
}

func screensUnion(screens []crop.Screen) image.Rectangle {
	var u image.Rectangle
	for _, s := range screens {
		u = u.Union(s.Bounds.Canon())
	}
	return u
}

// detectWindow 按严格 Z 序（由前到后）查找光标所在的可见最上层窗口，并裁剪到显示器总边界内。
func detectWindow(windows []image.Rectangle, screens []crop.Screen, gx, gy float64) image.Rectangle {
	pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
	union := screensUnion(screens)
	for _, w := range windows {
		w = w.Canon()
		if !pt.In(w) {
			continue
		}
		clipped := w
		if !union.Empty() {
			clipped = clipped.Intersect(union)
		}
		if clipped.Dx() < minDIP || clipped.Dy() < minDIP {
			continue
		}
		return clipped
	}
	return image.Rectangle{}
}

func sampleLoupe(screens []crop.Screen, gx, gy float64, radius int) (Loupe, bool) {
	if radius < 1 {
		radius = 5
	}
	pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
	var target *crop.Screen
	for i := range screens {
		b := screens[i].Bounds.Canon()
		if pt.In(b) && screens[i].Pixels != nil && screens[i].Scale > 0 {
			target = &screens[i]
			break
		}
	}
	if target == nil {
		return Loupe{}, false
	}
	b := target.Bounds.Canon()
	imgBounds := target.Pixels.Bounds()
	w, h := imgBounds.Dx(), imgBounds.Dy()
	if w <= 0 || h <= 0 {
		return Loupe{}, false
	}
	px := int(math.Floor((gx - float64(b.Min.X)) * target.Scale))
	py := int(math.Floor((gy - float64(b.Min.Y)) * target.Scale))
	px = max(0, min(px, w-1))
	py = max(0, min(py, h-1))

	side := 2*radius + 1
	grid := make([]color.NRGBA, side*side)
	oob := color.NRGBA{R: 28, G: 28, B: 32, A: 255}
	idx := 0
	for dy := -radius; dy <= radius; dy++ {
		sy := py + dy
		for dx := -radius; dx <= radius; dx++ {
			sx := px + dx
			if sx < 0 || sx >= w || sy < 0 || sy >= h {
				grid[idx] = oob
			} else {
				grid[idx] = target.Pixels.NRGBAAt(imgBounds.Min.X+sx, imgBounds.Min.Y+sy)
			}
			idx++
		}
	}
	center := target.Pixels.NRGBAAt(imgBounds.Min.X+px, imgBounds.Min.Y+py)
	return Loupe{
		Side:   side,
		Grid:   grid,
		Center: center,
		DIP:    pt,
		Pixel:  image.Pt(px, py),
	}, true
}
