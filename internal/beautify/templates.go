package beautify

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"snipgo/internal/textrender"
)

// TemplateKind 表示预设创意/实用模板类型。
type TemplateKind int

const (
	TemplateStickyNote TemplateKind = iota // 手账便签（和纸胶带 + 纸线 + 日期 + 折角）
	TemplatePolaroid                       // 拍立得相纸（宽底留白 + 手写感配文 + 日期）
	TemplateBrowser                        // 浏览器样机（红黄绿顶栏 + 网址胶囊 + 微点阵画布）
	TemplateEditorial                      // 社媒分享卡（栏目徽章 + 大标题 + 引用竖线备注）
	TemplateReceipt                        // 复古热敏小票（上下锯齿撕边 + 虚线 + 条形码）
	TemplateTerminal                       // 终端代码窗（macOS 顶栏 + 等宽标题 + 状态底栏）
	TemplateFilm                           // 35mm 电影胶片（上下镂空齿孔 + 胶片印记）
	TemplateReview                         // 评审工单卡（状态徽章 + 角标准星 + 结论栏）
	TemplatePostcard                       // 邮戳明信片（锯齿邮票 + 圆形邮戳波浪线）
	TemplateSlide                          // 幻灯片分栏（Keynote 左文右图宽幅版式）
	templateCount
)

// TemplatePadding 控制模板四周留白尺度。
type TemplatePadding int

const (
	PadCompact TemplatePadding = iota
	PadNormal
	PadWide
)

// TemplateMeta 描述一套模板的展示元信息与输入提示。
type TemplateMeta struct {
	Kind               TemplateKind
	Name               string
	ShortDesc          string
	TitlePlaceholder   string
	CaptionPlaceholder string
}

// TemplateTheme 描述模板内单套纸张/画框配色方案。
type TemplateTheme struct {
	Name    string
	Preview color.NRGBA
	Accent  color.NRGBA
}

// TemplateOptions 控制模板渲染的风格、文案、日期水印与留白。
type TemplateOptions struct {
	Kind     TemplateKind
	Theme    int
	Title    string
	Caption  string
	ShowDate bool
	DateText string // 可选自定义/测试固定日期；为空时默认当天 YYYY.MM.DD
	Padding  TemplatePadding
}

// DefaultTemplateOptions 返回默认手账便签模板配置。
func DefaultTemplateOptions() TemplateOptions {
	return TemplateOptions{
		Kind:     TemplateStickyNote,
		Theme:    0,
		ShowDate: true,
		Padding:  PadNormal,
	}
}

// AllTemplates 返回全部 6 套内置模板的元数据。
func AllTemplates() []TemplateMeta {
	return []TemplateMeta{
		{
			Kind:               TemplateStickyNote,
			Name:               "手账便签",
			ShortDesc:          "和纸胶带 · 折角便签",
			TitlePlaceholder:   "便签抬头（可选，如：设计走查）",
			CaptionPlaceholder: "底部备注 / 待办事项（可选）",
		},
		{
			Kind:               TemplatePolaroid,
			Name:               "拍立得",
			ShortDesc:          "相纸窄框 · 宽底留白",
			TitlePlaceholder:   "副标签（可选）",
			CaptionPlaceholder: "相纸底部配文（可选）",
		},
		{
			Kind:               TemplateBrowser,
			Name:               "浏览器框",
			ShortDesc:          "仿真网址栏 · 点阵底",
			TitlePlaceholder:   "地址栏网址（如 snipgo.app）",
			CaptionPlaceholder: "网页说明脚注（可选）",
		},
		{
			Kind:               TemplateEditorial,
			Name:               "分享卡片",
			ShortDesc:          "大标题 · 引用批注",
			TitlePlaceholder:   "主标题（如：新功能上线）",
			CaptionPlaceholder: "引用批注 / 核心亮点",
		},
		{
			Kind:               TemplateReceipt,
			Name:               "复古小票",
			ShortDesc:          "锯齿撕边 · 编码条码",
			TitlePlaceholder:   "小票抬头（如 SNIPGO MEMO）",
			CaptionPlaceholder: "底部摘要（如：共 1 项已完成）",
		},
		{
			Kind:               TemplateTerminal,
			Name:               "终端代码",
			ShortDesc:          "终端顶栏 · 状态底条",
			TitlePlaceholder:   "命令标题（如 zsh — ~/snipgo）",
			CaptionPlaceholder: "底部状态备注（可选）",
		},
		{
			Kind:               TemplateFilm,
			Name:               "35mm胶片",
			ShortDesc:          "胶片齿孔 · 复古边码",
			TitlePlaceholder:   "胶卷型号 / 抬头（可选）",
			CaptionPlaceholder: "底边片名 / 拍摄备注（可选）",
		},
		{
			Kind:               TemplateReview,
			Name:               "评审工单",
			ShortDesc:          "状态徽章 · 工程角标",
			TitlePlaceholder:   "评审标题（如 PR #42 走查）",
			CaptionPlaceholder: "结论或修改意见",
		},
		{
			Kind:               TemplatePostcard,
			Name:               "邮戳明信片",
			ShortDesc:          "锯齿邮票 · 圆形邮戳",
			TitlePlaceholder:   "明信片抬头（可选）",
			CaptionPlaceholder: "手写感寄语 / 备注",
		},
		{
			Kind:               TemplateSlide,
			Name:               "幻灯分栏",
			ShortDesc:          "左文右图 · Keynote风",
			TitlePlaceholder:   "幻灯片主标题",
			CaptionPlaceholder: "核心要点与功能说明",
		},
	}
}

// TemplateMetaByKind 查找指定模板元数据。
func TemplateMetaByKind(k TemplateKind) TemplateMeta {
	for _, m := range AllTemplates() {
		if m.Kind == k {
			return m
		}
	}
	return AllTemplates()[0]
}

// TemplateThemes 返回指定模板的可选配色方案。
func TemplateThemes(k TemplateKind) []TemplateTheme {
	switch k {
	case TemplateStickyNote:
		return []TemplateTheme{
			{Name: "暖黄便利贴", Preview: color.NRGBA{R: 255, G: 245, B: 196, A: 255}, Accent: color.NRGBA{R: 235, G: 158, B: 52, A: 255}},
			{Name: "樱花粉纸", Preview: color.NRGBA{R: 255, G: 234, B: 240, A: 255}, Accent: color.NRGBA{R: 228, G: 96, B: 132, A: 255}},
			{Name: "薄荷淡绿", Preview: color.NRGBA{R: 228, G: 248, B: 236, A: 255}, Accent: color.NRGBA{R: 42, G: 168, B: 112, A: 255}},
			{Name: "雾蓝手账", Preview: color.NRGBA{R: 230, G: 241, B: 255, A: 255}, Accent: color.NRGBA{R: 58, G: 132, B: 230, A: 255}},
		}
	case TemplatePolaroid:
		return []TemplateTheme{
			{Name: "经典白框", Preview: color.NRGBA{R: 251, G: 251, B: 248, A: 255}, Accent: color.NRGBA{R: 48, G: 50, B: 56, A: 255}},
			{Name: "奶油复古", Preview: color.NRGBA{R: 245, G: 237, B: 222, A: 255}, Accent: color.NRGBA{R: 132, G: 96, B: 62, A: 255}},
			{Name: "暗黑胶片", Preview: color.NRGBA{R: 34, G: 36, B: 42, A: 255}, Accent: color.NRGBA{R: 226, G: 230, B: 238, A: 255}},
		}
	case TemplateBrowser:
		return []TemplateTheme{
			{Name: "浅色画布", Preview: color.NRGBA{R: 244, G: 246, B: 250, A: 255}, Accent: color.NRGBA{R: 10, G: 132, B: 255, A: 255}},
			{Name: "深色夜览", Preview: color.NRGBA{R: 32, G: 35, B: 44, A: 255}, Accent: color.NRGBA{R: 96, G: 176, B: 255, A: 255}},
			{Name: "柔蓝展台", Preview: color.NRGBA{R: 222, G: 234, B: 248, A: 255}, Accent: color.NRGBA{R: 37, G: 99, B: 235, A: 255}},
		}
	case TemplateEditorial:
		return []TemplateTheme{
			{Name: "象牙刊物", Preview: color.NRGBA{R: 250, G: 246, B: 239, A: 255}, Accent: color.NRGBA{R: 212, G: 84, B: 52, A: 255}},
			{Name: "克莱因白", Preview: color.NRGBA{R: 255, G: 255, B: 255, A: 255}, Accent: color.NRGBA{R: 18, G: 84, B: 230, A: 255}},
			{Name: "暗夜特辑", Preview: color.NRGBA{R: 24, G: 26, B: 34, A: 255}, Accent: color.NRGBA{R: 56, G: 189, B: 248, A: 255}},
		}
	case TemplateReceipt:
		return []TemplateTheme{
			{Name: "热敏白纸", Preview: color.NRGBA{R: 251, G: 249, B: 243, A: 255}, Accent: color.NRGBA{R: 36, G: 36, B: 40, A: 255}},
			{Name: "牛皮票根", Preview: color.NRGBA{R: 242, G: 232, B: 210, A: 255}, Accent: color.NRGBA{R: 72, G: 52, B: 34, A: 255}},
			{Name: "暗夜清单", Preview: color.NRGBA{R: 32, G: 34, B: 40, A: 255}, Accent: color.NRGBA{R: 228, G: 232, B: 240, A: 255}},
		}
	case TemplateTerminal:
		return []TemplateTheme{
			{Name: "深邃暗夜", Preview: color.NRGBA{R: 24, G: 27, B: 36, A: 255}, Accent: color.NRGBA{R: 56, G: 189, B: 248, A: 255}},
			{Name: "暖岩黑", Preview: color.NRGBA{R: 36, G: 33, B: 31, A: 255}, Accent: color.NRGBA{R: 249, G: 115, B: 22, A: 255}},
			{Name: "翠绿极客", Preview: color.NRGBA{R: 13, G: 25, B: 21, A: 255}, Accent: color.NRGBA{R: 52, G: 211, B: 153, A: 255}},
			{Name: "浅灰终端", Preview: color.NRGBA{R: 246, G: 247, B: 250, A: 255}, Accent: color.NRGBA{R: 10, G: 132, B: 255, A: 255}},
		}
	case TemplateFilm:
		return []TemplateTheme{
			{Name: "柯达暖黑", Preview: color.NRGBA{R: 24, G: 22, B: 20, A: 255}, Accent: color.NRGBA{R: 238, G: 162, B: 68, A: 255}},
			{Name: "富士青绿", Preview: color.NRGBA{R: 20, G: 24, B: 23, A: 255}, Accent: color.NRGBA{R: 72, G: 208, B: 154, A: 255}},
			{Name: "银盐黑白", Preview: color.NRGBA{R: 22, G: 23, B: 26, A: 255}, Accent: color.NRGBA{R: 214, G: 218, B: 226, A: 255}},
		}
	case TemplateReview:
		return []TemplateTheme{
			{Name: "故障红", Preview: color.NRGBA{R: 255, G: 245, B: 245, A: 255}, Accent: color.NRGBA{R: 239, G: 68, B: 68, A: 255}},
			{Name: "待评审", Preview: color.NRGBA{R: 255, G: 250, B: 240, A: 255}, Accent: color.NRGBA{R: 245, G: 158, B: 11, A: 255}},
			{Name: "已修复", Preview: color.NRGBA{R: 240, G: 253, B: 246, A: 255}, Accent: color.NRGBA{R: 16, G: 185, B: 129, A: 255}},
			{Name: "说明蓝", Preview: color.NRGBA{R: 242, G: 247, B: 255, A: 255}, Accent: color.NRGBA{R: 10, G: 132, B: 255, A: 255}},
		}
	case TemplatePostcard:
		return []TemplateTheme{
			{Name: "牛皮纸", Preview: color.NRGBA{R: 242, G: 228, B: 204, A: 255}, Accent: color.NRGBA{R: 184, G: 74, B: 56, A: 255}},
			{Name: "象牙白", Preview: color.NRGBA{R: 250, G: 247, B: 240, A: 255}, Accent: color.NRGBA{R: 42, G: 92, B: 168, A: 255}},
			{Name: "雾蓝卡纸", Preview: color.NRGBA{R: 237, G: 243, B: 250, A: 255}, Accent: color.NRGBA{R: 59, G: 75, B: 160, A: 255}},
			{Name: "樱花粉", Preview: color.NRGBA{R: 253, G: 241, B: 245, A: 255}, Accent: color.NRGBA{R: 204, G: 82, B: 116, A: 255}},
		}
	case TemplateSlide:
		return []TemplateTheme{
			{Name: "深海蓝", Preview: color.NRGBA{R: 20, G: 32, B: 62, A: 255}, Accent: color.NRGBA{R: 56, G: 189, B: 248, A: 255}},
			{Name: "极简白", Preview: color.NRGBA{R: 248, G: 250, B: 252, A: 255}, Accent: color.NRGBA{R: 10, G: 132, B: 255, A: 255}},
			{Name: "暖米灰", Preview: color.NRGBA{R: 242, G: 239, B: 233, A: 255}, Accent: color.NRGBA{R: 217, G: 119, B: 6, A: 255}},
			{Name: "曜石黑", Preview: color.NRGBA{R: 20, G: 22, B: 28, A: 255}, Accent: color.NRGBA{R: 168, G: 85, B: 247, A: 255}},
		}
	default:
		return nil
	}
}

func padFactor(p TemplatePadding) float64 {
	switch p {
	case PadCompact:
		return 0.72
	case PadWide:
		return 1.38
	default:
		return 1.0
	}
}

func resolveDate(opts TemplateOptions) string {
	if !opts.ShowDate {
		return ""
	}
	if d := strings.TrimSpace(opts.DateText); d != "" {
		return d
	}
	return time.Now().Format("2006.01.02")
}

// RenderTemplate 按照指定创意/实用模板与文案选项合成高清成图。
func RenderTemplate(src *image.NRGBA, scale float64, opts TemplateOptions) *image.NRGBA {
	if src == nil || src.Bounds().Empty() {
		return src
	}
	if scale <= 0 {
		scale = 2.0
	}
	themes := TemplateThemes(opts.Kind)
	themeIdx := 0
	if len(themes) > 0 {
		themeIdx = ((opts.Theme % len(themes)) + len(themes)) % len(themes)
	}
	opts.Theme = themeIdx

	switch opts.Kind {
	case TemplatePolaroid:
		return renderPolaroidTemplate(src, scale, opts)
	case TemplateBrowser:
		return renderBrowserTemplate(src, scale, opts)
	case TemplateEditorial:
		return renderEditorialTemplate(src, scale, opts)
	case TemplateReceipt:
		return renderReceiptTemplate(src, scale, opts)
	case TemplateTerminal:
		return renderTerminalTemplate(src, scale, opts)
	case TemplateFilm:
		return renderFilmTemplate(src, scale, opts)
	case TemplateReview:
		return renderReviewTemplate(src, scale, opts)
	case TemplatePostcard:
		return renderPostcardTemplate(src, scale, opts)
	case TemplateSlide:
		return renderSlideTemplate(src, scale, opts)
	default:
		return renderStickyNoteTemplate(src, scale, opts)
	}
}

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

func drawCornerTicks(dst *image.NRGBA, x0, y0, w, h, arm, thick int, col color.NRGBA) {
	fillSeg := func(ax, ay, bx, by int) {
		for y := min(ay, by); y <= max(ay, by); y++ {
			for x := min(ax, bx); x <= max(ax, bx); x++ {
				if x >= dst.Bounds().Min.X && x < dst.Bounds().Max.X && y >= dst.Bounds().Min.Y && y < dst.Bounds().Max.Y {
					blendPixel(dst, x, y, col, 0.88)
				}
			}
		}
	}
	x1, y1 := x0+w-1, y0+h-1
	// TL, TR, BL, BR
	fillSeg(x0, y0, x0+arm, y0+thick-1)
	fillSeg(x0, y0, x0+thick-1, y0+arm)
	fillSeg(x1-arm, y0, x1, y0+thick-1)
	fillSeg(x1-thick+1, y0, x1, y0+arm)
	fillSeg(x0, y1-thick+1, x0+arm, y1)
	fillSeg(x0, y1-arm, x0+thick-1, y1)
	fillSeg(x1-arm, y1-thick+1, x1, y1)
	fillSeg(x1-thick+1, y1-arm, x1, y1)
}

func drawPerforatedStamp(dst *image.NRGBA, x0, y0, w, h int, s float64, paper, stampCol, border color.NRGBA) {
	drawRoundedBox(dst, x0, y0, w, h, 2*s, color.NRGBA{R: 255, G: 255, B: 255, A: 245}, border, 0.8)
	// 邮票四周齿轮穿孔（沿边挖出均匀半圆缺口）
	notchR := max(1.6, 2.2*s)
	step := max(5, int(math.Round(6.5*s)))
	for x := x0 + step/2; x <= x0+w-step/2; x += step {
		fillCircleAA(dst, float64(x), float64(y0), notchR, paper)
		fillCircleAA(dst, float64(x), float64(y0+h), notchR, paper)
	}
	for y := y0 + step/2; y <= y0+h-step/2; y += step {
		fillCircleAA(dst, float64(x0), float64(y), notchR, paper)
		fillCircleAA(dst, float64(x0+w), float64(y), notchR, paper)
	}
	pad := max(4, int(math.Round(5*s)))
	iw, ih := w-pad*2, h-pad*2
	if iw > 6 && ih > 6 {
		drawRoundedBox(dst, x0+pad, y0+pad, iw, ih, 1.5*s, stampCol, stampCol, 0)
		// 邮票内简约白色取景角符号
		tk := max(4, min(iw, ih)/4)
		thk := max(1, int(math.Round(1.4*s)))
		drawCornerTicks(dst, x0+pad+iw/5, y0+pad+ih/4, iw*3/5, ih/2, tk, thk, color.NRGBA{R: 255, G: 255, B: 255, A: 225})
	}
}

func drawPostmark(dst *image.NRGBA, cx, cy, s float64, ink color.NRGBA) {
	rOuter := 21.0 * s
	rInner := 14.5 * s
	strokeW := max(1.0, 1.3*s)
	b := dst.Bounds()
	reach := int(math.Ceil(rOuter + 4*s))
	baseA := float64(ink.A) / 255.0
	for y := max(b.Min.Y, int(cy)-reach); y <= min(b.Max.Y-1, int(cy)+reach); y++ {
		for x := max(b.Min.X, int(cx)-reach); x <= min(b.Max.X-1, int(cx)+reach); x++ {
			dist := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			dOut := math.Abs(dist-rOuter) - strokeW*0.5
			dIn := math.Abs(dist-rInner) - strokeW*0.5
			d := min(dOut, dIn)
			if d < 0.6 {
				blendPixel(dst, x, y, ink, min(1, 0.6-d)*baseA)
			}
		}
	}
	// 三条平行波浪销票线向左延伸
	waveLen := 38.0 * s
	for k := -1; k <= 1; k++ {
		baseY := cy + float64(k)*5.5*s
		for x := int(math.Round(cx - rOuter - waveLen)); x <= int(math.Round(cx-rInner*0.6)); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			wy := baseY + math.Sin(float64(x)/(5.0*s))*1.8*s
			iy := int(math.Round(wy))
			if iy >= b.Min.Y && iy < b.Max.Y {
				blendPixel(dst, x, iy, ink, baseA*0.85)
			}
		}
	}
}

// ================= 共享抗锯齿几何绘制原语 =================

func drawCardShadow(dst *image.NRGBA, x0, y0, w, h int, radius, shBlur, shOffY, strength float64) {
	b := dst.Bounds()
	ix0, iy0 := float64(x0), float64(y0)
	ix1, iy1 := float64(x0+w), float64(y0+h)
	cx, cy := (ix0+ix1)*0.5, (iy0+iy1)*0.5
	hx, hy := float64(w)*0.5, float64(h)*0.5
	box := image.Rect(
		max(b.Min.X, int(ix0-shBlur-3)),
		max(b.Min.Y, int(iy0+shOffY-shBlur-3)),
		min(b.Max.X, int(math.Ceil(ix1+shBlur+3))),
		min(b.Max.Y, int(math.Ceil(iy1+shOffY+shBlur+3))),
	)
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			d := roundedRectSDF(px-cx, py-(cy+shOffY), hx, hy, radius)
			if d >= shBlur {
				continue
			}
			t := max(0, min(1, 1.0-max(0, d)/shBlur))
			alpha := strength * t * t * (3 - 2*t)
			blendPixel(dst, x, y, black, alpha)
		}
	}
}

func drawRoundedBox(dst *image.NRGBA, x0, y0, w, h int, rad float64, fill, stroke color.NRGBA, strokeW float64) {
	b := dst.Bounds()
	minX := max(b.Min.X, x0-2)
	maxX := min(b.Max.X, x0+w+2)
	minY := max(b.Min.Y, y0-2)
	maxY := min(b.Max.Y, y0+h+2)
	cx := float64(x0) + float64(w)*0.5
	cy := float64(y0) + float64(h)*0.5
	hx := float64(w) * 0.5
	hy := float64(h) * 0.5
	rad = min(rad, min(hx, hy))
	for y := minY; y < maxY; y++ {
		py := float64(y) + 0.5
		for x := minX; x < maxX; x++ {
			px := float64(x) + 0.5
			d := roundedRectSDF(px-cx, py-cy, hx, hy, rad)
			if d < 0.5 {
				cov := min(1, 0.5-d) * (float64(fill.A) / 255.0)
				blendPixel(dst, x, y, fill, cov)
			}
			if strokeW > 0 {
				sd := math.Abs(d+strokeW*0.5) - strokeW*0.5
				if sd < 0.5 {
					sc := min(1, 0.5-sd) * (float64(stroke.A) / 255.0)
					blendPixel(dst, x, y, stroke, sc)
				}
			}
		}
	}
}

func stampRoundedImage(dst, src *image.NRGBA, ox, oy int, rad float64, rimCol color.NRGBA, rimW float64) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	cx := float64(ox) + float64(sw)*0.5
	cy := float64(oy) + float64(sh)*0.5
	hx := float64(sw) * 0.5
	hy := float64(sh) * 0.5
	rad = min(rad, min(hx, hy))
	db := dst.Bounds()
	for y := range sh {
		dy := oy + y
		if dy < db.Min.Y || dy >= db.Max.Y {
			continue
		}
		py := float64(dy) + 0.5
		for x := range sw {
			dx := ox + x
			if dx < db.Min.X || dx >= db.Max.X {
				continue
			}
			px := float64(dx) + 0.5
			d := roundedRectSDF(px-cx, py-cy, hx, hy, rad)
			if d >= 0.5 {
				continue
			}
			cov := min(1, 0.5-d)
			c := src.NRGBAAt(sb.Min.X+x, sb.Min.Y+y)
			if c.A > 0 {
				blendPixel(dst, dx, dy, c, cov*(float64(c.A)/255.0))
			}
			if rimW > 0 {
				rd := math.Abs(d+rimW*0.5) - rimW*0.5
				if rd < 0.5 {
					blendPixel(dst, dx, dy, rimCol, min(1, 0.5-rd)*0.55)
				}
			}
		}
	}
}

func fillCircleAA(dst *image.NRGBA, cx, cy, r float64, col color.NRGBA) {
	b := dst.Bounds()
	minX := max(b.Min.X, int(math.Floor(cx-r-2)))
	maxX := min(b.Max.X, int(math.Ceil(cx+r+2)))
	minY := max(b.Min.Y, int(math.Floor(cy-r-2)))
	maxY := min(b.Max.Y, int(math.Ceil(cy+r+2)))
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) - r
			if d < 0.5 {
				blendPixel(dst, x, y, col, min(1, 0.5-d)*(float64(col.A)/255.0))
			}
		}
	}
}

func drawDogEarCorner(dst *image.NRGBA, px1, py1 int, fold float64, backCol, edgeCol color.NRGBA) {
	// 翻卷三角顶点：(px1-fold, py1-fold), (px1-fold, py1), (px1, py1-fold)
	fx0 := float64(px1) - fold
	fy0 := float64(py1) - fold
	b := dst.Bounds()
	minX := max(b.Min.X, int(math.Floor(fx0-6)))
	maxX := min(b.Max.X, px1)
	minY := max(b.Min.Y, int(math.Floor(fy0-6)))
	maxY := min(b.Max.Y, py1)
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if fx < fx0-0.5 || fy < fy0-0.5 {
				continue
			}
			// 右上主对角线（内侧纸折区）
			diag := float64(px1) - fx + float64(py1) - fy - fold
			if diag <= -0.5 {
				continue
			}
			// 折起的背面三角形：fx >= fx0 && fy >= fy0 && diag >= 0
			inFold := fx >= fx0 && fy >= fy0
			if inFold {
				cov := min(1, fx-fx0+0.5) * min(1, fy-fy0+0.5) * min(1, diag+0.5)
				blendPixel(dst, x, y, backCol, cov)
				// 对角折痕高光边
				if math.Abs(diag) < 1.3 {
					blendPixel(dst, x, y, edgeCol, (1.0-math.Abs(diag)/1.3)*0.7)
				}
			} else {
				// 翻卷背面投在左边/上边纸面的柔和投影
				dLeft := fx0 - fx
				dTop := fy0 - fy
				dEdge := max(dLeft, dTop)
				if dEdge >= 0 && dEdge < 6 {
					shAlpha := 0.14 * (1.0 - dEdge/6.0)
					blendPixel(dst, x, y, color.NRGBA{R: 40, G: 32, B: 18, A: 255}, shAlpha)
				}
			}
		}
	}
}

func drawWashiTape(dst *image.NRGBA, cx, cy, w, h, angle, s float64, col color.NRGBA) {
	cosA, sinA := math.Cos(angle), math.Sin(angle)
	hw, hh := w*0.5, h*0.5
	b := dst.Bounds()
	reach := math.Hypot(hw, hh) + 6*s
	minX := max(b.Min.X, int(math.Floor(cx-reach)))
	maxX := min(b.Max.X, int(math.Ceil(cx+reach)))
	minY := max(b.Min.Y, int(math.Floor(cy-reach)))
	maxY := min(b.Max.Y, int(math.Ceil(cy+reach)))
	baseAlpha := float64(col.A) / 255.0

	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			// 旋转到胶带局部坐标
			lx := dx*cosA + dy*sinA
			ly := -dx*sinA + dy*cosA
			if math.Abs(ly) > hh+1.5 {
				continue
			}
			// 左右锯齿撕边偏移
			zig := math.Sin((ly/max(2.0, s))*1.6) * (2.6 * s)
			if lx < -hw-zig-1 || lx > hw+zig+1 {
				continue
			}
			edgeX := min(lx-(-hw-zig), (hw+zig)-lx)
			edgeY := hh - math.Abs(ly)
			cov := max(0, min(1, edgeX+0.5)) * max(0, min(1, edgeY+0.5))
			if cov <= 0 {
				continue
			}
			// 细微纵向织物纹理
			stripe := 0.94 + 0.06*math.Sin(lx*0.55)
			blendPixel(dst, x, y, col, cov*baseAlpha*stripe)
		}
	}
}

func fillDotGrid(dst *image.NRGBA, bg, dot color.NRGBA, s float64) {
	fillSolid(dst, bg)
	step := max(14, int(math.Round(18*s)))
	r := max(0.8, 1.0*s)
	b := dst.Bounds()
	for y := b.Min.Y + step/2; y < b.Max.Y; y += step {
		for x := b.Min.X + step/2; x < b.Max.X; x += step {
			fillCircleAA(dst, float64(x), float64(y), r, dot)
		}
	}
}

func drawDashedHLine(dst *image.NRGBA, x0, x1, y int, s float64, col color.NRGBA) {
	b := dst.Bounds()
	dashLen := max(4, int(math.Round(6*s)))
	gapLen := max(3, int(math.Round(4*s)))
	thick := max(1, int(math.Round(s)))
	period := dashLen + gapLen
	for row := y; row < y+thick && row < b.Max.Y; row++ {
		if row < b.Min.Y {
			continue
		}
		for x := max(b.Min.X, x0); x < min(b.Max.X, x1); x++ {
			if ((x - x0) % period) < dashLen {
				blendPixel(dst, x, row, col, 0.9)
			}
		}
	}
}

func drawDecorativeBarcode(dst *image.NRGBA, centerX, topY, width, height int, seedStr string, ink color.NRGBA) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seedStr))
	state := h.Sum32() | 0x9E3779B9
	nextBit := func() uint32 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return state
	}
	b := dst.Bounds()
	x0 := max(b.Min.X, centerX-width/2)
	x1 := min(b.Max.X, x0+width)
	x := x0
	for x < x1 {
		v := nextBit()
		barW := 1 + int(v%3)
		gapW := 1 + int((v>>4)%3)
		for bx := x; bx < min(x1, x+barW); bx++ {
			for by := max(b.Min.Y, topY); by < min(b.Max.Y, topY+height); by++ {
				blendPixel(dst, bx, by, ink, 0.85)
			}
		}
		x += barW + gapW
	}
}
