package beautify

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"snipgo/internal/textrender"
)

// ================= ⑥ 终端代码窗 TemplateTerminal =================

func renderTerminalTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk                           gradientSpec
		barTop, barBot, body, statusBg color.NRGBA
		border, accent, ink, muted     color.NRGBA
		specRim                        color.NRGBA
	}
	specs := [...]spec{
		{
			desk:     gradientSpec{c0: color.NRGBA{R: 18, G: 24, B: 38, A: 255}, c1: color.NRGBA{R: 26, G: 38, B: 62, A: 255}, c2: color.NRGBA{R: 14, G: 20, B: 34, A: 255}},
			barTop:   color.NRGBA{R: 38, G: 42, B: 56, A: 255},
			barBot:   color.NRGBA{R: 28, G: 31, B: 42, A: 255},
			body:     color.NRGBA{R: 21, G: 24, B: 33, A: 255},
			statusBg: color.NRGBA{R: 17, G: 19, B: 27, A: 255},
			border:   color.NRGBA{R: 76, G: 84, B: 108, A: 255},
			accent:   color.NRGBA{R: 56, G: 189, B: 248, A: 255},
			ink:      color.NRGBA{R: 228, G: 234, B: 246, A: 255},
			muted:    color.NRGBA{R: 132, G: 144, B: 168, A: 255},
			specRim:  color.NRGBA{R: 255, G: 255, B: 255, A: 64},
		},
		{
			desk:     gradientSpec{c0: color.NRGBA{R: 34, G: 29, B: 27, A: 255}, c1: color.NRGBA{R: 48, G: 39, B: 34, A: 255}, c2: color.NRGBA{R: 26, G: 22, B: 20, A: 255}},
			barTop:   color.NRGBA{R: 48, G: 44, B: 41, A: 255},
			barBot:   color.NRGBA{R: 38, G: 35, B: 32, A: 255},
			body:     color.NRGBA{R: 31, G: 28, B: 26, A: 255},
			statusBg: color.NRGBA{R: 25, G: 23, B: 21, A: 255},
			border:   color.NRGBA{R: 84, G: 76, B: 70, A: 255},
			accent:   color.NRGBA{R: 249, G: 115, B: 22, A: 255},
			ink:      color.NRGBA{R: 242, G: 236, B: 228, A: 255},
			muted:    color.NRGBA{R: 158, G: 146, B: 136, A: 255},
			specRim:  color.NRGBA{R: 255, G: 245, B: 235, A: 56},
		},
		{
			desk:     gradientSpec{c0: color.NRGBA{R: 10, G: 24, B: 20, A: 255}, c1: color.NRGBA{R: 14, G: 38, B: 30, A: 255}, c2: color.NRGBA{R: 8, G: 18, B: 15, A: 255}},
			barTop:   color.NRGBA{R: 22, G: 38, B: 33, A: 255},
			barBot:   color.NRGBA{R: 16, G: 29, B: 25, A: 255},
			body:     color.NRGBA{R: 12, G: 22, B: 19, A: 255},
			statusBg: color.NRGBA{R: 9, G: 17, B: 14, A: 255},
			border:   color.NRGBA{R: 46, G: 84, B: 70, A: 255},
			accent:   color.NRGBA{R: 52, G: 211, B: 153, A: 255},
			ink:      color.NRGBA{R: 220, G: 246, B: 234, A: 255},
			muted:    color.NRGBA{R: 116, G: 162, B: 142, A: 255},
			specRim:  color.NRGBA{R: 160, G: 255, B: 215, A: 56},
		},
		{
			desk:     gradientSpec{c0: color.NRGBA{R: 226, G: 232, B: 242, A: 255}, c1: color.NRGBA{R: 238, G: 242, B: 249, A: 255}, c2: color.NRGBA{R: 218, G: 225, B: 238, A: 255}},
			barTop:   color.NRGBA{R: 250, G: 251, B: 254, A: 255},
			barBot:   color.NRGBA{R: 240, G: 242, B: 247, A: 255},
			body:     color.NRGBA{R: 250, G: 251, B: 253, A: 255},
			statusBg: color.NRGBA{R: 242, G: 244, B: 248, A: 255},
			border:   color.NRGBA{R: 196, G: 204, B: 218, A: 255},
			accent:   color.NRGBA{R: 10, G: 132, B: 255, A: 255},
			ink:      color.NRGBA{R: 28, G: 32, B: 42, A: 255},
			muted:    color.NRGBA{R: 108, G: 118, B: 136, A: 255},
			specRim:  color.NRGBA{R: 255, G: 255, B: 255, A: 180},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(22, int(math.Round(30*s*pf)))
	wellPad := max(8, int(math.Round(10*s*pf)))
	barH := max(28, int(math.Round(34*s)))
	statusH := max(22, int(math.Round(26*s)))

	titleText := strings.TrimSpace(opts.Title)
	if titleText == "" {
		titleText = "zsh — ~/snipgo"
	}
	titleImg := textrender.Render(titleText, textrender.Options{
		FontSize:  11.5 * s,
		Weight:    0.25,
		Monospace: true,
		Color:     sp.ink,
	})
	leftStatus := strings.TrimSpace(opts.Caption)
	if leftStatus == "" {
		leftStatus = "READY"
	}
	leftStatImg := textrender.Render(leftStatus, textrender.Options{
		FontSize:  10.5 * s,
		Weight:    0.2,
		Monospace: true,
		MaxWidth:  max(80, sw-int(math.Round(130*s))),
		Color:     sp.muted,
	})
	rightTxt := "UTF-8 · macOS"
	if d := resolveDate(opts); d != "" {
		rightTxt = d + "  ·  UTF-8"
	}
	rightStatImg := textrender.Render(rightTxt, textrender.Options{
		FontSize:  10 * s,
		Weight:    0.15,
		Monospace: true,
		Color:     sp.muted,
	})

	winW := sw + wellPad*2
	winH := barH + sh + wellPad*2 + statusH
	outW := winW + outerPad*2
	outH := winH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillGradient(out, sp.desk)

	wx0, wy0 := outerPad, outerPad
	rad := min(12.0*s, float64(min(winW, winH))*0.12)
	drawCardShadow(out, wx0, wy0, winW, winH, rad, 28*s, 10*s, 0.42)

	winBuf := image.NewNRGBA(image.Rect(0, 0, winW, winH))
	fillSolid(winBuf, sp.body)
	for y := range barH {
		t := float64(y) / float64(max(1, barH-1))
		row := lerpColor(sp.barTop, sp.barBot, t)
		for x := range winW {
			winBuf.SetNRGBA(x, y, row)
		}
	}
	for y := winH - statusH; y < winH; y++ {
		for x := range winW {
			winBuf.SetNRGBA(x, y, sp.statusBg)
		}
	}
	for x := range winW {
		blendPixel(winBuf, x, barH-1, sp.border, 0.65)
		blendPixel(winBuf, x, winH-statusH, sp.border, 0.55)
		blendPixel(winBuf, x, 1, sp.specRim, float64(sp.specRim.A)/255.0)
	}

	// macOS 交通灯 + 居中标题
	lights := [3]color.NRGBA{
		{R: 255, G: 95, B: 86, A: 255},
		{R: 255, G: 189, B: 46, A: 255},
		{R: 39, G: 201, B: 63, A: 255},
	}
	dotY := float64(barH) * 0.5
	for i, c := range lights {
		fillCircleAA(winBuf, 16*s+float64(i)*17*s, dotY, 5.0*s, c)
	}
	if titleImg != nil {
		tx := max(int(math.Round(68*s)), (winW-titleImg.Bounds().Dx())/2)
		ty := (barH - titleImg.Bounds().Dy()) / 2
		stamp(winBuf, titleImg, tx, ty)
	}

	// 截图嵌入（带微圆角）
	imgRad := min(6.0*s, float64(min(sw, sh))*0.08)
	stampRoundedImage(winBuf, src, wellPad, barH+wellPad, imgRad, sp.border, 0.65)

	// 底部状态栏指示点与文本
	statY := winH - statusH
	fillCircleAA(winBuf, float64(wellPad)+4*s, float64(statY)+float64(statusH)*0.5, 3.2*s, sp.accent)
	if leftStatImg != nil {
		stamp(winBuf, leftStatImg, wellPad+int(math.Round(12*s)), statY+(statusH-leftStatImg.Bounds().Dy())/2)
	}
	if rightStatImg != nil {
		stamp(winBuf, rightStatImg, winW-wellPad-rightStatImg.Bounds().Dx(), statY+(statusH-rightStatImg.Bounds().Dy())/2)
	}

	stampRoundedImage(out, winBuf, wx0, wy0, rad, sp.border, 1.0)
	return out
}

// ================= ⑦ 35mm 电影胶片 TemplateFilm =================

func renderFilmTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, film, gateBorder, mark, muted color.NRGBA
		defaultStock                        string
	}
	specs := [...]spec{
		{
			desk:         color.NRGBA{R: 28, G: 26, B: 24, A: 255},
			film:         color.NRGBA{R: 18, G: 17, B: 16, A: 255},
			gateBorder:   color.NRGBA{R: 58, G: 52, B: 44, A: 255},
			mark:         color.NRGBA{R: 242, G: 168, B: 74, A: 235},
			muted:        color.NRGBA{R: 182, G: 144, B: 96, A: 210},
			defaultStock: "KODAK PORTRA 400",
		},
		{
			desk:         color.NRGBA{R: 20, G: 26, B: 25, A: 255},
			film:         color.NRGBA{R: 14, G: 18, B: 17, A: 255},
			gateBorder:   color.NRGBA{R: 46, G: 68, B: 60, A: 255},
			mark:         color.NRGBA{R: 82, G: 216, B: 160, A: 235},
			muted:        color.NRGBA{R: 116, G: 174, B: 150, A: 210},
			defaultStock: "FUJI SUPERIA 400",
		},
		{
			desk:         color.NRGBA{R: 32, G: 34, B: 38, A: 255},
			film:         color.NRGBA{R: 16, G: 17, B: 20, A: 255},
			gateBorder:   color.NRGBA{R: 68, G: 72, B: 82, A: 255},
			mark:         color.NRGBA{R: 222, G: 226, B: 234, A: 230},
			muted:        color.NRGBA{R: 150, G: 156, B: 168, A: 200},
			defaultStock: "ILFORD HP5 PLUS 400",
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(14, int(math.Round(20*s*pf)))
	sidePad := max(14, int(math.Round(18*s*pf)))
	railH := max(34, int(math.Round(40*s*pf)))

	stockStr := strings.ToUpper(strings.TrimSpace(opts.Title))
	if stockStr == "" {
		stockStr = sp.defaultStock
	}
	topLeftImg := textrender.Render(stockStr, textrender.Options{
		FontSize:  9.5 * s,
		Weight:    0.35,
		Monospace: true,
		Color:     sp.mark,
	})
	topRightImg := textrender.Render("36A  ▸", textrender.Options{
		FontSize:  9.5 * s,
		Weight:    0.35,
		Monospace: true,
		Color:     sp.mark,
	})
	botLeftStr := strings.TrimSpace(opts.Caption)
	if botLeftStr == "" {
		botLeftStr = "35MM · SAFETY FILM"
	}
	botLeftImg := textrender.Render(botLeftStr, textrender.Options{
		FontSize:  9.5 * s,
		Weight:    0.25,
		Monospace: true,
		MaxWidth:  max(80, sw-int(math.Round(100*s))),
		Color:     sp.muted,
	})
	var botRightImg *image.NRGBA
	if d := resolveDate(opts); d != "" {
		botRightImg = textrender.Render(d, textrender.Options{
			FontSize:  9.5 * s,
			Weight:    0.25,
			Monospace: true,
			Color:     sp.muted,
		})
	}

	stripW := sw + sidePad*2
	stripH := railH*2 + sh
	outW := stripW + outerPad*2
	outH := stripH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	fx0, fy0 := outerPad, outerPad
	drawCardShadow(out, fx0, fy0, stripW, stripH, 4*s, 22*s, 8*s, 0.45)
	drawRoundedBox(out, fx0, fy0, stripW, stripH, 4*s, sp.film, sp.gateBorder, 1.0)

	// 镂空上下胶片齿孔（透出底层桌面颜色）
	holeW := max(9, int(math.Round(12*s)))
	holeH := max(12, int(math.Round(15*s)))
	pitch := max(18, int(math.Round(25*s)))
	topHoleY := fy0 + max(5, int(math.Round(6*s)))
	botHoleY := fy0 + stripH - holeH - max(5, int(math.Round(6*s)))
	startX := fx0 + max(10, (stripW%pitch)/2)
	for hx := startX; hx+holeW <= fx0+stripW-8; hx += pitch {
		drawRoundedBox(out, hx, topHoleY, holeW, holeH, 2.5*s, sp.desk, sp.gateBorder, 0.7)
		drawRoundedBox(out, hx, botHoleY, holeW, holeH, 2.5*s, sp.desk, sp.gateBorder, 0.7)
	}

	// 齿孔内侧电影胶片喷码文字
	topTextY := topHoleY + holeH + max(3, (railH-(topHoleY-fy0+holeH)-int(12*s))/2)
	if topLeftImg != nil {
		stamp(out, topLeftImg, fx0+sidePad, topTextY)
	}
	if topRightImg != nil {
		stamp(out, topRightImg, fx0+stripW-sidePad-topRightImg.Bounds().Dx(), topTextY)
	}

	botTextY := fy0 + railH + sh + max(3, (railH-(fy0+stripH-botHoleY)-int(12*s))/2)
	if botLeftImg != nil {
		stamp(out, botLeftImg, fx0+sidePad, botTextY)
	}
	if botRightImg != nil {
		stamp(out, botRightImg, fx0+stripW-sidePad-botRightImg.Bounds().Dx(), botTextY)
	}

	// 画幅嵌入
	stampRoundedImage(out, src, fx0+sidePad, fy0+railH, 2*s, sp.gateBorder, 1.1)
	return out
}

// ================= ⑩ 评审工单卡 TemplateReview =================

func renderReviewTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, card, border, footBg, accent, ink, muted color.NRGBA
		badge                                          string
	}
	specs := [...]spec{
		{
			desk:   color.NRGBA{R: 238, G: 240, B: 244, A: 255},
			card:   color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			border: color.NRGBA{R: 214, G: 219, B: 228, A: 255},
			footBg: color.NRGBA{R: 254, G: 246, B: 246, A: 255},
			accent: color.NRGBA{R: 239, G: 68, B: 68, A: 255},
			ink:    color.NRGBA{R: 22, G: 25, B: 34, A: 255},
			muted:  color.NRGBA{R: 108, G: 116, B: 132, A: 255},
			badge:  "BUG",
		},
		{
			desk:   color.NRGBA{R: 240, G: 241, B: 245, A: 255},
			card:   color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			border: color.NRGBA{R: 218, G: 220, B: 228, A: 255},
			footBg: color.NRGBA{R: 255, G: 249, B: 238, A: 255},
			accent: color.NRGBA{R: 234, G: 136, B: 14, A: 255},
			ink:    color.NRGBA{R: 24, G: 26, B: 34, A: 255},
			muted:  color.NRGBA{R: 110, G: 114, B: 126, A: 255},
			badge:  "REVIEW",
		},
		{
			desk:   color.NRGBA{R: 236, G: 242, B: 240, A: 255},
			card:   color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			border: color.NRGBA{R: 210, G: 224, B: 218, A: 255},
			footBg: color.NRGBA{R: 240, G: 253, B: 247, A: 255},
			accent: color.NRGBA{R: 16, G: 185, B: 129, A: 255},
			ink:    color.NRGBA{R: 20, G: 28, B: 26, A: 255},
			muted:  color.NRGBA{R: 98, G: 118, B: 110, A: 255},
			badge:  "FIXED",
		},
		{
			desk:   color.NRGBA{R: 235, G: 240, B: 248, A: 255},
			card:   color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			border: color.NRGBA{R: 212, G: 222, B: 236, A: 255},
			footBg: color.NRGBA{R: 243, G: 248, B: 255, A: 255},
			accent: color.NRGBA{R: 10, G: 132, B: 255, A: 255},
			ink:    color.NRGBA{R: 20, G: 26, B: 38, A: 255},
			muted:  color.NRGBA{R: 104, G: 116, B: 136, A: 255},
			badge:  "INFO",
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(18, int(math.Round(24*s*pf)))
	innerPad := max(16, int(math.Round(20*s*pf)))
	topStripeH := max(3, int(math.Round(4.5*s)))
	headH := max(36, int(math.Round(44*s)))

	titleStr := strings.TrimSpace(opts.Title)
	if titleStr == "" {
		titleStr = "UI 走查工单记录"
	}
	capStr := strings.TrimSpace(opts.Caption)
	if capStr == "" {
		capStr = "请确认截图区域交互、边距与像素对齐"
	}
	dateStr := resolveDate(opts)

	badgeImg := textrender.Render(sp.badge, textrender.Options{
		FontSize:  10.5 * s,
		Weight:    0.45,
		Monospace: true,
		Color:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	})
	badgeW := max(int(math.Round(46*s)), badgeImg.Bounds().Dx()+int(math.Round(16*s)))
	badgeH := max(int(math.Round(20*s)), badgeImg.Bounds().Dy()+int(math.Round(8*s)))

	var dateImg *image.NRGBA
	if dateStr != "" {
		dateImg = textrender.Render(dateStr, textrender.Options{
			FontSize:  10.5 * s,
			Weight:    0.15,
			Monospace: true,
			Color:     sp.muted,
		})
	}
	dateReserve := 0
	if dateImg != nil {
		dateReserve = dateImg.Bounds().Dx() + int(math.Round(12*s))
	}
	titleImg := textrender.Render(titleStr, textrender.Options{
		FontSize: 15 * s,
		Weight:   0.38,
		MaxWidth: max(90, sw-badgeW-dateReserve-int(math.Round(16*s))),
		Color:    sp.ink,
	})
	resImg := textrender.Render(fmt.Sprintf("%d × %d px", sw, sh), textrender.Options{
		FontSize:  10.5 * s,
		Weight:    0.2,
		Monospace: true,
		Color:     sp.muted,
	})
	capImg := textrender.Render(capStr, textrender.Options{
		FontSize: 12 * s,
		Weight:   0.2,
		MaxWidth: max(90, sw-resImg.Bounds().Dx()-int(math.Round(18*s))),
		Color:    sp.ink,
	})
	footH := max(int(math.Round(38*s)), capImg.Bounds().Dy()+int(math.Round(22*s)))

	wellPad := max(10, int(math.Round(12*s)))
	cardW := sw + wellPad*2 + innerPad*2
	cardH := topStripeH + headH + sh + wellPad*2 + innerPad + footH
	outW := cardW + outerPad*2
	outH := cardH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	cx0, cy0 := outerPad, outerPad
	cardRad := 13.0 * s
	drawCardShadow(out, cx0, cy0, cardW, cardH, cardRad, 24*s, 9*s, 0.24)

	cardBuf := image.NewNRGBA(image.Rect(0, 0, cardW, cardH))
	fillSolid(cardBuf, sp.card)
	for y := range topStripeH {
		for x := range cardW {
			cardBuf.SetNRGBA(x, y, sp.accent)
		}
	}
	footY := cardH - footH
	for y := footY; y < cardH; y++ {
		for x := range cardW {
			cardBuf.SetNRGBA(x, y, sp.footBg)
		}
	}
	for x := range cardW {
		blendPixel(cardBuf, x, footY, sp.border, 0.7)
		blendPixel(cardBuf, x, topStripeH+headH-1, sp.border, 0.55)
	}

	// Header：左侧标题 + 右侧日期与状态徽章
	if titleImg != nil {
		stamp(cardBuf, titleImg, innerPad, topStripeH+(headH-titleImg.Bounds().Dy())/2)
	}
	bx := cardW - innerPad - badgeW
	by := topStripeH + (headH-badgeH)/2
	drawRoundedBox(cardBuf, bx, by, badgeW, badgeH, float64(badgeH)*0.48, sp.accent, sp.accent, 0)
	stamp(cardBuf, badgeImg, bx+(badgeW-badgeImg.Bounds().Dx())/2, by+(badgeH-badgeImg.Bounds().Dy())/2)
	if dateImg != nil {
		stamp(cardBuf, dateImg, bx-int(math.Round(10*s))-dateImg.Bounds().Dx(), topStripeH+(headH-dateImg.Bounds().Dy())/2)
	}

	// 截图与四周工程角标准星
	ix, iy := innerPad+wellPad, topStripeH+headH+innerPad/2+wellPad
	stampRoundedImage(cardBuf, src, ix, iy, min(5*s, float64(min(sw, sh))*0.06), sp.border, 1.0)
	drawCornerTicks(cardBuf, ix-wellPad/2, iy-wellPad/2, sw+wellPad, sh+wellPad, max(6, int(math.Round(8*s))), max(1, int(math.Round(1.5*s))), sp.accent)

	// 底部结论栏
	if capImg != nil {
		stamp(cardBuf, capImg, innerPad, footY+(footH-capImg.Bounds().Dy())/2)
	}
	stamp(cardBuf, resImg, cardW-innerPad-resImg.Bounds().Dx(), footY+(footH-resImg.Bounds().Dy())/2)

	stampRoundedImage(out, cardBuf, cx0, cy0, cardRad, sp.border, 1.0)
	return out
}

// ================= ⑪ 邮戳明信片 TemplatePostcard =================

func renderPostcardTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, paper, border, stampCol, line, ink, muted color.NRGBA
	}
	specs := [...]spec{
		{
			desk:     color.NRGBA{R: 54, G: 46, B: 38, A: 255},
			paper:    color.NRGBA{R: 244, G: 232, B: 208, A: 255},
			border:   color.NRGBA{R: 208, G: 190, B: 158, A: 255},
			stampCol: color.NRGBA{R: 184, G: 74, B: 56, A: 255},
			line:     color.NRGBA{R: 192, G: 170, B: 136, A: 140},
			ink:      color.NRGBA{R: 54, G: 42, B: 28, A: 255},
			muted:    color.NRGBA{R: 128, G: 106, B: 80, A: 255},
		},
		{
			desk:     color.NRGBA{R: 38, G: 42, B: 50, A: 255},
			paper:    color.NRGBA{R: 251, G: 248, B: 241, A: 255},
			border:   color.NRGBA{R: 218, G: 214, B: 204, A: 255},
			stampCol: color.NRGBA{R: 42, G: 92, B: 168, A: 255},
			line:     color.NRGBA{R: 198, G: 196, B: 188, A: 140},
			ink:      color.NRGBA{R: 38, G: 42, B: 52, A: 255},
			muted:    color.NRGBA{R: 120, G: 124, B: 136, A: 255},
		},
		{
			desk:     color.NRGBA{R: 32, G: 40, B: 52, A: 255},
			paper:    color.NRGBA{R: 237, G: 243, B: 251, A: 255},
			border:   color.NRGBA{R: 196, G: 210, B: 228, A: 255},
			stampCol: color.NRGBA{R: 59, G: 75, B: 160, A: 255},
			line:     color.NRGBA{R: 184, G: 202, B: 224, A: 140},
			ink:      color.NRGBA{R: 34, G: 46, B: 68, A: 255},
			muted:    color.NRGBA{R: 102, G: 120, B: 148, A: 255},
		},
		{
			desk:     color.NRGBA{R: 52, G: 38, B: 44, A: 255},
			paper:    color.NRGBA{R: 253, G: 242, B: 246, A: 255},
			border:   color.NRGBA{R: 228, G: 198, B: 208, A: 255},
			stampCol: color.NRGBA{R: 204, G: 82, B: 116, A: 255},
			line:     color.NRGBA{R: 216, G: 186, B: 196, A: 140},
			ink:      color.NRGBA{R: 64, G: 38, B: 48, A: 255},
			muted:    color.NRGBA{R: 144, G: 106, B: 118, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(20, int(math.Round(28*s*pf)))
	sidePad := max(18, int(math.Round(24*s*pf)))
	stampW := max(36, int(math.Round(46*s)))
	stampH := max(44, int(math.Round(54*s)))
	headH := stampH + int(math.Round(20*s))

	titleStr := strings.TrimSpace(opts.Title)
	if titleStr == "" {
		titleStr = "POSTCARD · 灵感留档"
	}
	titleImg := textrender.Render(titleStr, textrender.Options{
		FontSize: 15 * s,
		Weight:   0.38,
		MaxWidth: max(90, sw-stampW-int(math.Round(36*s))),
		Color:    sp.ink,
	})
	capStr := strings.TrimSpace(opts.Caption)
	if capStr == "" {
		capStr = "记录屏幕此刻所见"
	}
	dateStr := resolveDate(opts)
	var dateImg *image.NRGBA
	if dateStr != "" {
		dateImg = textrender.Render(dateStr, textrender.Options{
			FontSize:  10.5 * s,
			Weight:    0.15,
			Monospace: true,
			Color:     sp.muted,
		})
	}
	dateW := 0
	if dateImg != nil {
		dateW = dateImg.Bounds().Dx() + int(math.Round(12*s))
	}
	capImg := textrender.Render(capStr, textrender.Options{
		FontSize: 13 * s,
		Weight:   0.22,
		MaxWidth: max(90, sw-dateW),
		Color:    sp.ink,
	})
	footH := max(int(math.Round(44*s)), capImg.Bounds().Dy()+int(math.Round(28*s)))

	cardW := sw + sidePad*2
	cardH := headH + sh + footH
	outW := cardW + outerPad*2
	outH := cardH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	cx0, cy0 := outerPad, outerPad
	drawCardShadow(out, cx0, cy0, cardW, cardH, 4*s, 24*s, 9*s, 0.42)
	drawRoundedBox(out, cx0, cy0, cardW, cardH, 4*s, sp.paper, sp.border, 1.0)

	// 顶部标题 + 短装饰线
	ix := cx0 + sidePad
	if titleImg != nil {
		ty := cy0 + max(int(math.Round(16*s)), (headH-titleImg.Bounds().Dy()-int(math.Round(10*s)))/2)
		stamp(out, titleImg, ix, ty)
		drawRoundedBox(out, ix, ty+titleImg.Bounds().Dy()+int(math.Round(5*s)), int(math.Round(32*s)), max(2, int(math.Round(2.5*s))), 1*s, sp.stampCol, sp.stampCol, 0)
	}

	// 右上角锯齿邮票
	stX := cx0 + cardW - sidePad - stampW
	stY := cy0 + int(math.Round(12*s))
	drawPerforatedStamp(out, stX, stY, stampW, stampH, s, sp.paper, sp.stampCol, sp.border)

	// 邮票左下重叠圆形日期邮戳与三条波浪销票线
	pmX := float64(stX) - 6*s
	pmY := float64(stY+stampH) - 12*s
	drawPostmark(out, pmX, pmY, s, color.NRGBA{R: sp.ink.R, G: sp.ink.G, B: sp.ink.B, A: 115})

	// 居中截图
	iy := cy0 + headH
	stampRoundedImage(out, src, ix, iy, 3*s, sp.border, 1.0)

	// 底部信纸横线与手写寄语
	capY := iy + sh + int(math.Round(14*s))
	if capImg != nil {
		stamp(out, capImg, ix, capY)
	}
	if dateImg != nil {
		stamp(out, dateImg, ix+sw-dateImg.Bounds().Dx(), capY+max(0, capImg.Bounds().Dy()-dateImg.Bounds().Dy()))
	}
	ruleY := cy0 + cardH - int(math.Round(14*s))
	for x := ix; x < ix+sw; x++ {
		blendPixel(out, x, ruleY, sp.line, 0.72)
	}
	return out
}

// ================= ⑫ 幻灯片分栏 TemplateSlide =================

func renderSlideTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk                             color.NRGBA
		slideBg                          gradientSpec
		border, accent, pillBg, ink, mut color.NRGBA
	}
	specs := [...]spec{
		{
			desk:    color.NRGBA{R: 10, G: 14, B: 26, A: 255},
			slideBg: gradientSpec{c0: color.NRGBA{R: 17, G: 28, B: 56, A: 255}, c1: color.NRGBA{R: 25, G: 44, B: 84, A: 255}, c2: color.NRGBA{R: 15, G: 24, B: 48, A: 255}},
			border:  color.NRGBA{R: 78, G: 112, B: 172, A: 255},
			accent:  color.NRGBA{R: 56, G: 189, B: 248, A: 255},
			pillBg:  color.NRGBA{R: 32, G: 78, B: 118, A: 255},
			ink:     color.NRGBA{R: 246, G: 249, B: 255, A: 255},
			mut:     color.NRGBA{R: 174, G: 194, B: 224, A: 255},
		},
		{
			desk:    color.NRGBA{R: 226, G: 231, B: 240, A: 255},
			slideBg: gradientSpec{c0: color.NRGBA{R: 252, G: 253, B: 255, A: 255}, c1: color.NRGBA{R: 246, G: 249, B: 254, A: 255}, c2: color.NRGBA{R: 240, G: 245, B: 252, A: 255}},
			border:  color.NRGBA{R: 204, G: 214, B: 230, A: 255},
			accent:  color.NRGBA{R: 10, G: 132, B: 255, A: 255},
			pillBg:  color.NRGBA{R: 228, G: 240, B: 255, A: 255},
			ink:     color.NRGBA{R: 20, G: 26, B: 38, A: 255},
			mut:     color.NRGBA{R: 100, G: 112, B: 132, A: 255},
		},
		{
			desk:    color.NRGBA{R: 220, G: 214, B: 204, A: 255},
			slideBg: gradientSpec{c0: color.NRGBA{R: 246, G: 243, B: 236, A: 255}, c1: color.NRGBA{R: 242, G: 238, B: 230, A: 255}, c2: color.NRGBA{R: 236, G: 231, B: 221, A: 255}},
			border:  color.NRGBA{R: 208, G: 198, B: 182, A: 255},
			accent:  color.NRGBA{R: 217, G: 119, B: 6, A: 255},
			pillBg:  color.NRGBA{R: 248, G: 232, B: 206, A: 255},
			ink:     color.NRGBA{R: 42, G: 36, B: 28, A: 255},
			mut:     color.NRGBA{R: 122, G: 110, B: 96, A: 255},
		},
		{
			desk:    color.NRGBA{R: 12, G: 13, B: 17, A: 255},
			slideBg: gradientSpec{c0: color.NRGBA{R: 24, G: 25, B: 32, A: 255}, c1: color.NRGBA{R: 32, G: 29, B: 44, A: 255}, c2: color.NRGBA{R: 20, G: 21, B: 28, A: 255}},
			border:  color.NRGBA{R: 72, G: 74, B: 92, A: 255},
			accent:  color.NRGBA{R: 168, G: 85, B: 247, A: 255},
			pillBg:  color.NRGBA{R: 64, G: 44, B: 94, A: 255},
			ink:     color.NRGBA{R: 244, G: 244, B: 250, A: 255},
			mut:     color.NRGBA{R: 162, G: 164, B: 182, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(18, int(math.Round(24*s*pf)))
	padX := max(22, int(math.Round(28*s*pf)))
	padY := max(22, int(math.Round(26*s*pf)))
	colGap := max(18, int(math.Round(24*s)))
	leftW := max(int(math.Round(164*s)), min(int(math.Round(250*s)), int(float64(sw)*0.48)))

	tagImg := textrender.Render("HIGHLIGHT", textrender.Options{
		FontSize:  9.5 * s,
		Weight:    0.4,
		Monospace: true,
		Color:     sp.accent,
	})
	titleStr := strings.TrimSpace(opts.Title)
	if titleStr == "" {
		titleStr = "SnipGo 展示亮点"
	}
	titleImg := textrender.Render(titleStr, textrender.Options{
		FontSize: 20 * s,
		Weight:   0.45,
		MaxWidth: leftW,
		Color:    sp.ink,
	})
	capStr := strings.TrimSpace(opts.Caption)
	if capStr == "" {
		capStr = "轻量截图、精准标注与高质感模板输出，一气呵成。"
	}
	capImg := textrender.Render(capStr, textrender.Options{
		FontSize: 12.5 * s,
		Weight:   0.2,
		MaxWidth: leftW,
		Color:    sp.mut,
	})
	var dateImg *image.NRGBA
	if d := resolveDate(opts); d != "" {
		dateImg = textrender.Render(d+"  ·  SnipGo", textrender.Options{
			FontSize:  10 * s,
			Weight:    0.15,
			Monospace: true,
			Color:     sp.mut,
		})
	}

	tagH := max(int(math.Round(20*s)), tagImg.Bounds().Dy()+int(math.Round(8*s)))
	leftH := tagH + int(math.Round(16*s))
	if titleImg != nil {
		leftH += titleImg.Bounds().Dy() + int(math.Round(12*s))
	}
	leftH += max(3, int(math.Round(4*s))) + int(math.Round(12*s))
	if capImg != nil {
		leftH += capImg.Bounds().Dy()
	}
	if dateImg != nil {
		leftH += dateImg.Bounds().Dy() + int(math.Round(20*s))
	}

	slideW := padX + leftW + colGap + sw + padX
	slideH := max(leftH+padY*2, sh+padY*2)
	outW := slideW + outerPad*2
	outH := slideH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	sx0, sy0 := outerPad, outerPad
	slideRad := min(14.0*s, float64(min(slideW, slideH))*0.12)
	drawCardShadow(out, sx0, sy0, slideW, slideH, slideRad, 28*s, 10*s, 0.36)

	slideBuf := image.NewNRGBA(image.Rect(0, 0, slideW, slideH))
	fillGradient(slideBuf, sp.slideBg)

	// 左侧排版内容（纵向居中）
	lx := padX
	topContentH := leftH
	if dateImg != nil {
		topContentH -= dateImg.Bounds().Dy() + int(math.Round(20*s))
	}
	ly := max(padY, (slideH-topContentH)/2)
	if dateImg != nil && ly+topContentH > slideH-padY-dateImg.Bounds().Dy()-int(math.Round(12*s)) {
		ly = padY
	}
	tagW := max(int(math.Round(76*s)), tagImg.Bounds().Dx()+int(math.Round(14*s)))
	drawRoundedBox(slideBuf, lx, ly, tagW, tagH, float64(tagH)*0.5, sp.pillBg, sp.accent, 0.5)
	stamp(slideBuf, tagImg, lx+(tagW-tagImg.Bounds().Dx())/2, ly+(tagH-tagImg.Bounds().Dy())/2)
	curY := ly + tagH + int(math.Round(14*s))
	if titleImg != nil {
		stamp(slideBuf, titleImg, lx, curY)
		curY += titleImg.Bounds().Dy() + int(math.Round(12*s))
	}
	barH := max(3, int(math.Round(3.5*s)))
	drawRoundedBox(slideBuf, lx, curY, int(math.Round(32*s)), barH, float64(barH)*0.5, sp.accent, sp.accent, 0)
	curY += barH + int(math.Round(12*s))
	if capImg != nil {
		stamp(slideBuf, capImg, lx, curY)
	}
	if dateImg != nil {
		stamp(slideBuf, dateImg, lx, slideH-padY-dateImg.Bounds().Dy())
	}

	// 右侧展示区：柔和投影 + 圆角截图
	rx := padX + leftW + colGap
	ry := (slideH - sh) / 2
	imgRad := min(9.0*s, float64(min(sw, sh))*0.10)
	drawCardShadow(slideBuf, rx, ry, sw, sh, imgRad, 20*s, 8*s, 0.34)
	stampRoundedImage(slideBuf, src, rx, ry, imgRad, sp.border, 1.0)

	stampRoundedImage(out, slideBuf, sx0, sy0, slideRad, sp.border, 1.0)
	return out
}
