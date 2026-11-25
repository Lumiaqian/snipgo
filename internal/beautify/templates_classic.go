package beautify

import (
	"image"
	"image/color"
	"math"
	"strings"

	"snipgo/internal/textrender"
)

// ================= ① 手账便签 TemplateStickyNote =================

func renderStickyNoteTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, paper, tape, line, ink, muted, foldBack, foldEdge color.NRGBA
	}
	specs := [...]spec{
		{
			desk:     color.NRGBA{R: 230, G: 224, B: 212, A: 255},
			paper:    color.NRGBA{R: 255, G: 247, B: 212, A: 255},
			tape:     color.NRGBA{R: 238, G: 184, B: 96, A: 175},
			line:     color.NRGBA{R: 224, G: 204, B: 146, A: 130},
			ink:      color.NRGBA{R: 54, G: 44, B: 28, A: 255},
			muted:    color.NRGBA{R: 132, G: 112, B: 74, A: 255},
			foldBack: color.NRGBA{R: 244, G: 229, B: 172, A: 255},
			foldEdge: color.NRGBA{R: 210, G: 188, B: 122, A: 255},
		},
		{
			desk:     color.NRGBA{R: 234, G: 224, B: 228, A: 255},
			paper:    color.NRGBA{R: 255, G: 240, B: 244, A: 255},
			tape:     color.NRGBA{R: 235, G: 134, B: 162, A: 165},
			line:     color.NRGBA{R: 232, G: 196, B: 208, A: 130},
			ink:      color.NRGBA{R: 62, G: 36, B: 46, A: 255},
			muted:    color.NRGBA{R: 150, G: 102, B: 118, A: 255},
			foldBack: color.NRGBA{R: 246, G: 216, B: 225, A: 255},
			foldEdge: color.NRGBA{R: 214, G: 172, B: 186, A: 255},
		},
		{
			desk:     color.NRGBA{R: 220, G: 231, B: 225, A: 255},
			paper:    color.NRGBA{R: 236, G: 251, B: 242, A: 255},
			tape:     color.NRGBA{R: 96, G: 192, B: 142, A: 165},
			line:     color.NRGBA{R: 188, G: 226, B: 204, A: 130},
			ink:      color.NRGBA{R: 32, G: 58, B: 46, A: 255},
			muted:    color.NRGBA{R: 88, G: 132, B: 110, A: 255},
			foldBack: color.NRGBA{R: 210, G: 238, B: 222, A: 255},
			foldEdge: color.NRGBA{R: 166, G: 206, B: 184, A: 255},
		},
		{
			desk:     color.NRGBA{R: 220, G: 228, B: 238, A: 255},
			paper:    color.NRGBA{R: 238, G: 246, B: 255, A: 255},
			tape:     color.NRGBA{R: 96, G: 156, B: 235, A: 165},
			line:     color.NRGBA{R: 192, G: 214, B: 240, A: 135},
			ink:      color.NRGBA{R: 32, G: 48, B: 70, A: 255},
			muted:    color.NRGBA{R: 92, G: 118, B: 152, A: 255},
			foldBack: color.NRGBA{R: 212, G: 228, B: 248, A: 255},
			foldEdge: color.NRGBA{R: 170, G: 196, B: 226, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(22, int(math.Round(30*s*pf)))
	tapeOverhang := int(math.Round(12 * s))
	paperPadX := max(16, int(math.Round(22*s*pf)))
	headerH := int(math.Round(42 * s))
	imgRad := min(7.0*s, float64(min(sw, sh))*0.12)

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "MEMO"
	}
	dateStr := resolveDate(opts)
	caption := strings.TrimSpace(opts.Caption)

	titleImg := textrender.Render(title, textrender.Options{
		FontSize: 13.5 * s,
		Weight:   0.32,
		Color:    sp.ink,
	})
	var dateImg *image.NRGBA
	if dateStr != "" {
		dateImg = textrender.Render(dateStr, textrender.Options{
			FontSize:  11.5 * s,
			Weight:    0.2,
			Monospace: true,
			Color:     sp.muted,
		})
	}
	var capImg *image.NRGBA
	if caption != "" {
		capImg = textrender.Render(caption, textrender.Options{
			FontSize: 13 * s,
			Weight:   0.2,
			MaxWidth: max(120, sw-int(math.Round(24*s))),
			Color:    sp.ink,
		})
	}

	footerH := int(math.Round(28 * s))
	if capImg != nil {
		footerH = capImg.Bounds().Dy() + int(math.Round(34*s))
	}

	paperW := sw + paperPadX*2
	paperH := headerH + sh + footerH
	outW := paperW + outerPad*2
	outH := paperH + outerPad*2 + tapeOverhang

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	px0, py0 := outerPad, outerPad+tapeOverhang
	px1, py1 := px0+paperW, py0+paperH
	paperRad := 6.0 * s
	fold := float64(max(18, int(math.Round(26*s))))

	// 1. 便签柔和投影（裁除右下翻折缺角）
	drawCardShadow(out, px0, py0, paperW, paperH, paperRad, 22*s, 8*s, 0.22)

	// 2. 便签纸底色（避开右下折角三角区 x+y 剪裁）
	cx, cy := float64(px0+px1)*0.5, float64(py0+py1)*0.5
	hx, hy := float64(paperW)*0.5, float64(paperH)*0.5
	for y := py0; y < py1; y++ {
		for x := px0; x < px1; x++ {
			// 右下折角对角线方程：from (px1-fold, py1) to (px1, py1-fold)
			diag := float64(px1-x) + float64(py1-y) - fold
			if diag <= -0.5 {
				continue
			}
			d := roundedRectSDF(float64(x)+0.5-cx, float64(y)+0.5-cy, hx, hy, paperRad)
			if d >= 0.5 {
				continue
			}
			cov := min(1, 0.5-d) * min(1, diag+0.5)
			blendPixel(out, x, y, sp.paper, cov)
		}
	}

	// 3. 淡淡的手账笔记本横线纹理
	lineStep := max(14, int(math.Round(22*s)))
	lineW := max(1, int(math.Round(s*0.7)))
	for ly := py0 + headerH; ly < py1-int(fold*0.3); ly += lineStep {
		for t := range lineW {
			if ly+t >= py1 {
				break
			}
			for x := px0 + paperPadX; x < px1-paperPadX; x++ {
				if float64(px1-x)+float64(py1-(ly+t))-fold > 1 {
					blendPixel(out, x, ly+t, sp.line, 0.48)
				}
			}
		}
	}
	// 抬头发丝分割线
	sepY := py0 + headerH - int(math.Round(8*s))
	for x := px0 + paperPadX; x < px1-paperPadX; x++ {
		blendPixel(out, x, sepY, sp.line, 0.85)
	}

	// 4. 抬头圆点 + Title + 右侧日期
	dotR := 3.8 * s
	dotX := float64(px0+paperPadX) + dotR + 2*s
	dotY := float64(py0) + float64(headerH-int(8*s))*0.5
	fillCircleAA(out, dotX, dotY, dotR, sp.tape)
	if titleImg != nil {
		ty := int(math.Round(dotY - float64(titleImg.Bounds().Dy())*0.5))
		stamp(out, titleImg, int(math.Round(dotX+dotR+8*s)), ty)
	}
	if dateImg != nil {
		dy := int(math.Round(dotY - float64(dateImg.Bounds().Dy())*0.5))
		stamp(out, dateImg, px1-paperPadX-dateImg.Bounds().Dx(), dy)
	}

	// 5. 居中嵌入截图 + 微细纸边阴影
	ix, iy := px0+paperPadX, py0+headerH
	stampRoundedImage(out, src, ix, iy, imgRad, sp.line, 0.9)

	// 6. 底部手账配文
	if capImg != nil {
		cyText := iy + sh + int(math.Round(13*s))
		stamp(out, capImg, ix, cyText)
	}

	// 7. 右下翻卷折角（上三角折起 + 背面微阴影）
	drawDogEarCorner(out, px1, py1, fold, sp.foldBack, sp.foldEdge)

	// 8. 顶部半透明和纸胶带（略带 -1.5° 自然倾斜 + 左右锯齿切边）
	tapeW := min(float64(paperW)*0.42, max(96*s, 132*s))
	tapeH := 22.0 * s
	tapeCX := float64(outW) * 0.5
	tapeCY := float64(py0) + 1.5*s
	drawWashiTape(out, tapeCX, tapeCY, tapeW, tapeH, -0.028, s, sp.tape)

	return out
}

// ================= ② 拍立得相纸 TemplatePolaroid =================

func renderPolaroidTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, card, border, ink, muted color.NRGBA
	}
	specs := [...]spec{
		{
			desk:   color.NRGBA{R: 38, G: 40, B: 46, A: 255},
			card:   color.NRGBA{R: 251, G: 250, B: 247, A: 255},
			border: color.NRGBA{R: 222, G: 220, B: 214, A: 255},
			ink:    color.NRGBA{R: 38, G: 38, B: 42, A: 255},
			muted:  color.NRGBA{R: 126, G: 126, B: 132, A: 255},
		},
		{
			desk:   color.NRGBA{R: 62, G: 52, B: 44, A: 255},
			card:   color.NRGBA{R: 246, G: 238, B: 223, A: 255},
			border: color.NRGBA{R: 214, G: 198, B: 172, A: 255},
			ink:    color.NRGBA{R: 64, G: 48, B: 32, A: 255},
			muted:  color.NRGBA{R: 138, G: 112, B: 84, A: 255},
		},
		{
			desk:   color.NRGBA{R: 20, G: 22, B: 26, A: 255},
			card:   color.NRGBA{R: 34, G: 36, B: 42, A: 255},
			border: color.NRGBA{R: 58, G: 62, B: 72, A: 255},
			ink:    color.NRGBA{R: 238, G: 240, B: 246, A: 255},
			muted:  color.NRGBA{R: 146, G: 152, B: 166, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(20, int(math.Round(28*s*pf)))
	sidePad := max(14, int(math.Round(18*s*pf)))
	topPad := sidePad

	capText := strings.TrimSpace(opts.Caption)
	if capText == "" {
		capText = strings.TrimSpace(opts.Title)
	}
	var capImg *image.NRGBA
	if capText != "" {
		capImg = textrender.Render(capText, textrender.Options{
			FontSize: 15.5 * s,
			Weight:   0.25,
			MaxWidth: max(100, sw),
			Color:    sp.ink,
		})
	}
	var subImg *image.NRGBA
	if dateStr := resolveDate(opts); dateStr != "" {
		lbl := strings.ReplaceAll(dateStr, ".", " · ")
		if t := strings.TrimSpace(opts.Title); t != "" && t != capText {
			lbl = t + "   " + lbl
		}
		subImg = textrender.Render(lbl, textrender.Options{
			FontSize:  11 * s,
			Weight:    0.15,
			Monospace: true,
			Color:     sp.muted,
		})
	}

	chinH := max(int(math.Round(56*s)), int(math.Round(66*s)))
	if capImg != nil {
		chinH = max(chinH, capImg.Bounds().Dy()+int(math.Round(42*s)))
	}

	cardW := sw + sidePad*2
	cardH := topPad + sh + chinH
	outW := cardW + outerPad*2
	outH := cardH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	cx0, cy0 := outerPad, outerPad
	drawCardShadow(out, cx0, cy0, cardW, cardH, 3*s, 26*s, 9*s, 0.45)
	drawRoundedBox(out, cx0, cy0, cardW, cardH, 3*s, sp.card, sp.border, 1.0)

	ix, iy := cx0+sidePad, cy0+topPad
	stampRoundedImage(out, src, ix, iy, 1.5*s, sp.border, 0.9)
	// 相纸相框顶部 2px 内凹立体阴影
	for row := range max(1, int(math.Round(2.5*s))) {
		alpha := 0.16 * (1.0 - float64(row)/max(1.0, 3.0*s))
		for x := ix; x < ix+sw; x++ {
			blendPixel(out, x, iy+row, color.NRGBA{R: 0, G: 0, B: 0, A: 255}, alpha)
		}
	}

	if capImg != nil {
		cxText := ix + (sw-capImg.Bounds().Dx())/2
		cyText := iy + sh + max(int(math.Round(14*s)), (chinH-capImg.Bounds().Dy()-int(math.Round(16*s)))/2)
		stamp(out, capImg, cxText, cyText)
	}
	if subImg != nil {
		sx := ix + sw - subImg.Bounds().Dx()
		sy := cy0 + cardH - subImg.Bounds().Dy() - int(math.Round(12*s))
		stamp(out, subImg, sx, sy)
	}
	return out
}

// ================= ③ 浏览器样机 TemplateBrowser =================

func renderBrowserTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		bg, dot, barTop, barBot, pill, pillBorder, border, ink, muted color.NRGBA
	}
	specs := [...]spec{
		{
			bg:         color.NRGBA{R: 238, G: 241, B: 246, A: 255},
			dot:        color.NRGBA{R: 196, G: 204, B: 218, A: 255},
			barTop:     color.NRGBA{R: 248, G: 249, B: 252, A: 255},
			barBot:     color.NRGBA{R: 238, G: 240, B: 245, A: 255},
			pill:       color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			pillBorder: color.NRGBA{R: 214, G: 218, B: 228, A: 255},
			border:     color.NRGBA{R: 204, G: 210, B: 222, A: 255},
			ink:        color.NRGBA{R: 52, G: 58, B: 70, A: 255},
			muted:      color.NRGBA{R: 120, G: 128, B: 144, A: 255},
		},
		{
			bg:         color.NRGBA{R: 20, G: 23, B: 30, A: 255},
			dot:        color.NRGBA{R: 52, G: 58, B: 74, A: 255},
			barTop:     color.NRGBA{R: 46, G: 49, B: 58, A: 255},
			barBot:     color.NRGBA{R: 36, G: 39, B: 48, A: 255},
			pill:       color.NRGBA{R: 26, G: 28, B: 36, A: 255},
			pillBorder: color.NRGBA{R: 72, G: 76, B: 90, A: 255},
			border:     color.NRGBA{R: 74, G: 80, B: 96, A: 255},
			ink:        color.NRGBA{R: 228, G: 232, B: 242, A: 255},
			muted:      color.NRGBA{R: 142, G: 150, B: 168, A: 255},
		},
		{
			bg:         color.NRGBA{R: 218, G: 231, B: 248, A: 255},
			dot:        color.NRGBA{R: 168, G: 192, B: 226, A: 255},
			barTop:     color.NRGBA{R: 250, G: 252, B: 255, A: 255},
			barBot:     color.NRGBA{R: 240, G: 245, B: 252, A: 255},
			pill:       color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			pillBorder: color.NRGBA{R: 204, G: 218, B: 236, A: 255},
			border:     color.NRGBA{R: 186, G: 204, B: 228, A: 255},
			ink:        color.NRGBA{R: 38, G: 52, B: 74, A: 255},
			muted:      color.NRGBA{R: 102, G: 120, B: 148, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(22, int(math.Round(34*s*pf)))
	barH := max(30, int(math.Round(38*s)))

	urlText := strings.TrimSpace(opts.Title)
	if urlText == "" {
		urlText = "snipgo.app"
	}
	urlImg := textrender.Render(urlText, textrender.Options{
		FontSize:  12 * s,
		Weight:    0.2,
		Monospace: true,
		Color:     sp.ink,
	})
	var footImg *image.NRGBA
	footParts := make([]string, 0, 2)
	if capT := strings.TrimSpace(opts.Caption); capT != "" {
		footParts = append(footParts, capT)
	}
	if d := resolveDate(opts); d != "" {
		footParts = append(footParts, d)
	}
	if len(footParts) > 0 {
		footImg = textrender.Render(strings.Join(footParts, "  ·  "), textrender.Options{
			FontSize: 11 * s,
			Weight:   0.2,
			Color:    sp.muted,
		})
	}

	winW := sw
	winH := barH + sh
	footExtra := 0
	if footImg != nil {
		footExtra = footImg.Bounds().Dy() + int(math.Round(12*s))
	}
	outW := winW + outerPad*2
	outH := winH + outerPad*2 + footExtra

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillDotGrid(out, sp.bg, sp.dot, s)

	wx0, wy0 := outerPad, outerPad
	rad := min(11.0*s, float64(min(winW, winH))*0.12)
	drawCardShadow(out, wx0, wy0, winW, winH, rad, 28*s, 11*s, 0.34)

	// 先合成完整的浏览器窗口（顶栏+内容），再统一圆角裁切嵌入。
	winBuf := image.NewNRGBA(image.Rect(0, 0, winW, winH))
	for y := range barH {
		t := float64(y) / float64(max(1, barH-1))
		row := lerpColor(sp.barTop, sp.barBot, t)
		for x := range winW {
			winBuf.SetNRGBA(x, y, row)
		}
	}
	for y := range sh {
		sOff := src.PixOffset(sb.Min.X, sb.Min.Y+y)
		dOff := winBuf.PixOffset(0, barH+y)
		copy(winBuf.Pix[dOff:dOff+sw*4], src.Pix[sOff:sOff+sw*4])
	}
	// 顶栏底部分割线
	for x := range winW {
		blendPixel(winBuf, x, barH-1, sp.border, 0.8)
	}

	// 交通灯三圆点
	lights := [3]color.NRGBA{
		{R: 255, G: 95, B: 86, A: 255},
		{R: 255, G: 189, B: 46, A: 255},
		{R: 39, G: 201, B: 63, A: 255},
	}
	dotY := float64(barH) * 0.5
	dotR := 5.5 * s
	for i, c := range lights {
		fillCircleAA(winBuf, 18*s+float64(i)*19*s, dotY, dotR, c)
	}

	// 居中地址栏胶囊
	pillW := max(int(math.Round(120*s)), min(int(float64(winW)*0.56), winW-int(math.Round(160*s))))
	if pillW >= int(80*s) {
		pillH := int(math.Round(24 * s))
		pillX := (winW - pillW) / 2
		pillY := (barH - pillH) / 2
		drawRoundedBox(winBuf, pillX, pillY, pillW, pillH, float64(pillH)*0.48, sp.pill, sp.pillBorder, 1.0)
		if urlImg != nil {
			ux := pillX + (pillW-urlImg.Bounds().Dx())/2
			if ux < pillX+int(10*s) {
				ux = pillX + int(10*s)
			}
			uy := pillY + (pillH-urlImg.Bounds().Dy())/2
			stamp(winBuf, urlImg, ux, uy)
		}
	}

	stampRoundedImage(out, winBuf, wx0, wy0, rad, sp.border, 1.0)
	if footImg != nil {
		fx := wx0 + (winW-footImg.Bounds().Dx())/2
		fy := wy0 + winH + int(math.Round(12*s))
		stamp(out, footImg, fx, fy)
	}
	return out
}

// ================= ④ 社媒分享卡 TemplateEditorial =================

func renderEditorialTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, card, border, badgeBg, accent, ink, muted color.NRGBA
	}
	specs := [...]spec{
		{
			desk:    color.NRGBA{R: 232, G: 226, B: 216, A: 255},
			card:    color.NRGBA{R: 251, G: 248, B: 242, A: 255},
			border:  color.NRGBA{R: 224, G: 214, B: 198, A: 255},
			badgeBg: color.NRGBA{R: 250, G: 232, B: 226, A: 255},
			accent:  color.NRGBA{R: 212, G: 84, B: 52, A: 255},
			ink:     color.NRGBA{R: 34, G: 30, B: 28, A: 255},
			muted:   color.NRGBA{R: 124, G: 112, B: 102, A: 255},
		},
		{
			desk:    color.NRGBA{R: 232, G: 237, B: 245, A: 255},
			card:    color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			border:  color.NRGBA{R: 222, G: 228, B: 238, A: 255},
			badgeBg: color.NRGBA{R: 234, G: 242, B: 255, A: 255},
			accent:  color.NRGBA{R: 18, G: 84, B: 230, A: 255},
			ink:     color.NRGBA{R: 20, G: 24, B: 34, A: 255},
			muted:   color.NRGBA{R: 108, G: 116, B: 134, A: 255},
		},
		{
			desk:    color.NRGBA{R: 15, G: 17, B: 22, A: 255},
			card:    color.NRGBA{R: 26, G: 28, B: 36, A: 255},
			border:  color.NRGBA{R: 54, G: 58, B: 72, A: 255},
			badgeBg: color.NRGBA{R: 22, G: 54, B: 74, A: 255},
			accent:  color.NRGBA{R: 56, G: 189, B: 248, A: 255},
			ink:     color.NRGBA{R: 242, G: 244, B: 250, A: 255},
			muted:   color.NRGBA{R: 146, G: 154, B: 172, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(18, int(math.Round(24*s*pf)))
	innerPad := max(18, int(math.Round(26*s*pf)))
	tagH := int(math.Round(22 * s))

	tagImg := textrender.Render("SNIPGO NOTE", textrender.Options{
		FontSize:  10.5 * s,
		Weight:    0.35,
		Monospace: true,
		Color:     sp.accent,
	})
	var dateImg *image.NRGBA
	if d := resolveDate(opts); d != "" {
		dateImg = textrender.Render(d, textrender.Options{
			FontSize:  11 * s,
			Weight:    0.2,
			Monospace: true,
			Color:     sp.muted,
		})
	}
	titleStr := strings.TrimSpace(opts.Title)
	var titleImg *image.NRGBA
	if titleStr != "" {
		titleImg = textrender.Render(titleStr, textrender.Options{
			FontSize: 20 * s,
			Weight:   0.42,
			MaxWidth: max(120, sw),
			Color:    sp.ink,
		})
	}
	capStr := strings.TrimSpace(opts.Caption)
	var capImg *image.NRGBA
	if capStr != "" {
		capImg = textrender.Render(capStr, textrender.Options{
			FontSize: 13.5 * s,
			Weight:   0.2,
			MaxWidth: max(120, sw-int(math.Round(18*s))),
			Color:    sp.muted,
		})
	}

	topBlockH := tagH + int(math.Round(14*s))
	if titleImg != nil {
		topBlockH += titleImg.Bounds().Dy() + int(math.Round(14*s))
	}
	botBlockH := innerPad
	if capImg != nil {
		botBlockH = capImg.Bounds().Dy() + int(math.Round(22*s)) + innerPad
	}

	cardW := sw + innerPad*2
	cardH := innerPad + topBlockH + sh + botBlockH
	outW := cardW + outerPad*2
	outH := cardH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	cx0, cy0 := outerPad, outerPad
	cardRad := 14.0 * s
	drawCardShadow(out, cx0, cy0, cardW, cardH, cardRad, 24*s, 9*s, 0.25)
	drawRoundedBox(out, cx0, cy0, cardW, cardH, cardRad, sp.card, sp.border, 1.0)

	ix, iy := cx0+innerPad, cy0+innerPad
	// 栏目胶囊
	badgeW := int(math.Round(96 * s))
	if tagImg != nil {
		badgeW = tagImg.Bounds().Dx() + int(math.Round(16*s))
	}
	drawRoundedBox(out, ix, iy, badgeW, tagH, 5*s, sp.badgeBg, sp.accent, 0.28)
	if tagImg != nil {
		stamp(out, tagImg, ix+int(math.Round(8*s)), iy+(tagH-tagImg.Bounds().Dy())/2)
	}
	if dateImg != nil {
		stamp(out, dateImg, cx0+cardW-innerPad-dateImg.Bounds().Dx(), iy+(tagH-dateImg.Bounds().Dy())/2)
	}

	curY := iy + tagH + int(math.Round(14*s))
	if titleImg != nil {
		stamp(out, titleImg, ix, curY)
		curY += titleImg.Bounds().Dy() + int(math.Round(14*s))
	}

	imgRad := min(9.0*s, float64(min(sw, sh))*0.12)
	stampRoundedImage(out, src, ix, curY, imgRad, sp.border, 1.0)
	curY += sh

	if capImg != nil {
		quoteY := curY + int(math.Round(16*s))
		barW := max(2, int(math.Round(3*s)))
		drawRoundedBox(out, ix, quoteY+int(2*s), barW, max(12, capImg.Bounds().Dy()-int(4*s)), float64(barW)*0.5, sp.accent, sp.accent, 0)
		stamp(out, capImg, ix+barW+int(math.Round(10*s)), quoteY)
	}
	return out
}

// ================= ⑤ 复古热敏小票 TemplateReceipt =================

func renderReceiptTemplate(src *image.NRGBA, s float64, opts TemplateOptions) *image.NRGBA {
	type spec struct {
		desk, paper, border, ink, muted color.NRGBA
	}
	specs := [...]spec{
		{
			desk:   color.NRGBA{R: 40, G: 42, B: 48, A: 255},
			paper:  color.NRGBA{R: 251, G: 249, B: 243, A: 255},
			border: color.NRGBA{R: 218, G: 214, B: 204, A: 255},
			ink:    color.NRGBA{R: 32, G: 32, B: 36, A: 255},
			muted:  color.NRGBA{R: 120, G: 118, B: 112, A: 255},
		},
		{
			desk:   color.NRGBA{R: 54, G: 46, B: 38, A: 255},
			paper:  color.NRGBA{R: 243, G: 233, B: 212, A: 255},
			border: color.NRGBA{R: 206, G: 188, B: 156, A: 255},
			ink:    color.NRGBA{R: 58, G: 42, B: 28, A: 255},
			muted:  color.NRGBA{R: 132, G: 108, B: 82, A: 255},
		},
		{
			desk:   color.NRGBA{R: 18, G: 19, B: 24, A: 255},
			paper:  color.NRGBA{R: 32, G: 34, B: 41, A: 255},
			border: color.NRGBA{R: 72, G: 76, B: 90, A: 255},
			ink:    color.NRGBA{R: 234, G: 238, B: 246, A: 255},
			muted:  color.NRGBA{R: 144, G: 150, B: 166, A: 255},
		},
	}
	sp := specs[opts.Theme%len(specs)]
	pf := padFactor(opts.Padding)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	outerPad := max(20, int(math.Round(28*s*pf)))
	sidePad := max(16, int(math.Round(22*s*pf)))
	toothH := max(6, int(math.Round(8*s)))
	toothW := max(10, int(math.Round(14*s)))

	titleStr := strings.ToUpper(strings.TrimSpace(opts.Title))
	if titleStr == "" {
		titleStr = "SNIPGO · ARCHIVE"
	}
	titleImg := textrender.Render(titleStr, textrender.Options{
		FontSize:  13 * s,
		Weight:    0.35,
		Monospace: true,
		Color:     sp.ink,
	})
	dateStr := resolveDate(opts)
	var metaImg *image.NRGBA
	if dateStr != "" {
		metaImg = textrender.Render(dateStr+"   #0108", textrender.Options{
			FontSize:  10.5 * s,
			Weight:    0.15,
			Monospace: true,
			Color:     sp.muted,
		})
	}
	capStr := strings.TrimSpace(opts.Caption)
	var capImg *image.NRGBA
	if capStr != "" {
		capImg = textrender.Render(capStr, textrender.Options{
			FontSize:  12 * s,
			Weight:    0.2,
			Monospace: true,
			MaxWidth:  max(100, sw),
			Color:     sp.ink,
		})
	}

	topBlockH := int(math.Round(48 * s))
	if metaImg != nil {
		topBlockH += metaImg.Bounds().Dy() + int(math.Round(6*s))
	}
	barcodeH := int(math.Round(24 * s))
	botBlockH := barcodeH + int(math.Round(34*s))
	if capImg != nil {
		botBlockH += capImg.Bounds().Dy() + int(math.Round(10*s))
	}

	paperW := sw + sidePad*2
	paperH := toothH*2 + topBlockH + sh + botBlockH
	outW := paperW + outerPad*2
	outH := paperH + outerPad*2

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillSolid(out, sp.desk)

	px0, py0 := outerPad, outerPad
	px1, py1 := px0+paperW, py0+paperH

	// 柔和阴影
	drawCardShadow(out, px0, py0+toothH/2, paperW, paperH-toothH, 2*s, 22*s, 8*s, 0.4)

	// 填充小票主体与上下锯齿边缘
	halfT := float64(toothW) * 0.5
	for y := py0; y < py1; y++ {
		for x := px0; x < px1; x++ {
			lx := float64(x - px0)
			phase := math.Mod(lx, float64(toothW))
			tri := math.Abs(phase-halfT) / halfT // 0 在齿谷，1 在齿尖
			topEdge := float64(py0) + (1.0-tri)*float64(toothH)
			botEdge := float64(py1) - (1.0-tri)*float64(toothH)
			fy := float64(y) + 0.5
			if fy < topEdge-0.5 || fy > botEdge+0.5 {
				continue
			}
			cov := min(1, fy-(topEdge-0.5)) * min(1, (botEdge+0.5)-fy)
			blendPixel(out, x, y, sp.paper, cov)
		}
	}

	ix := px0 + sidePad
	curY := py0 + toothH + int(math.Round(12*s))
	if titleImg != nil {
		stamp(out, titleImg, px0+(paperW-titleImg.Bounds().Dx())/2, curY)
		curY += titleImg.Bounds().Dy() + int(math.Round(4*s))
	}
	if metaImg != nil {
		stamp(out, metaImg, px0+(paperW-metaImg.Bounds().Dx())/2, curY)
		curY += metaImg.Bounds().Dy() + int(math.Round(8*s))
	} else {
		curY += int(math.Round(8 * s))
	}
	drawDashedHLine(out, ix, px1-sidePad, curY, s, sp.border)

	imgY := py0 + toothH + topBlockH
	stampRoundedImage(out, src, ix, imgY, 2*s, sp.border, 0.9)

	dashBotY := imgY + sh + int(math.Round(12*s))
	drawDashedHLine(out, ix, px1-sidePad, dashBotY, s, sp.border)
	curBot := dashBotY + int(math.Round(10*s))
	if capImg != nil {
		stamp(out, capImg, px0+(paperW-capImg.Bounds().Dx())/2, curBot)
		curBot += capImg.Bounds().Dy() + int(math.Round(10*s))
	}
	drawDecorativeBarcode(out, px0+paperW/2, curBot, min(sw, int(math.Round(180*s))), barcodeH, titleStr+capStr+dateStr, sp.ink)
	return out
}
