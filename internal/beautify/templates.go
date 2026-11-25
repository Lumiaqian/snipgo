package beautify

import (
	"image"
	"image/color"
	"strings"
	"time"
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

// TemplateKindKey 返回可序列化的模板标识键，用于配置持久化。
func TemplateKindKey(k TemplateKind) string {
	switch k {
	case TemplatePolaroid:
		return "polaroid"
	case TemplateBrowser:
		return "browser"
	case TemplateEditorial:
		return "editorial"
	case TemplateReceipt:
		return "receipt"
	case TemplateTerminal:
		return "terminal"
	case TemplateFilm:
		return "film"
	case TemplateReview:
		return "review"
	case TemplatePostcard:
		return "postcard"
	case TemplateSlide:
		return "slide"
	default:
		return "sticky_note"
	}
}

// ParseTemplateKind 将序列化键解析为 TemplateKind；不认识时返回 false。
func ParseTemplateKind(s string) (TemplateKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sticky_note":
		return TemplateStickyNote, true
	case "polaroid":
		return TemplatePolaroid, true
	case "browser":
		return TemplateBrowser, true
	case "editorial":
		return TemplateEditorial, true
	case "receipt":
		return TemplateReceipt, true
	case "terminal":
		return TemplateTerminal, true
	case "film":
		return TemplateFilm, true
	case "review":
		return TemplateReview, true
	case "postcard":
		return TemplatePostcard, true
	case "slide":
		return TemplateSlide, true
	default:
		return TemplateStickyNote, false
	}
}

// TemplatePaddingKey 返回可序列化的留白尺寸标识键。
func TemplatePaddingKey(p TemplatePadding) string {
	switch p {
	case PadCompact:
		return "compact"
	case PadWide:
		return "wide"
	default:
		return "normal"
	}
}

// ParseTemplatePadding 将序列化键解析为 TemplatePadding，默认返回 PadNormal。
func ParseTemplatePadding(s string) TemplatePadding {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "compact":
		return PadCompact
	case "wide":
		return PadWide
	default:
		return PadNormal
	}
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
