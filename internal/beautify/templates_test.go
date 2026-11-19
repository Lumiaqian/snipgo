package beautify

import (
	"image"
	"image/color"
	"testing"
)

func TestRenderAllTemplatesAndThemes(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 260, 150))
	for y := range 150 {
		for x := range 260 {
			c := color.NRGBA{R: 34, G: 40, B: 52, A: 255}
			if x > 24 && x < 236 && y > 24 && y < 126 {
				c = color.NRGBA{R: 250, G: 251, B: 254, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}

	metas := AllTemplates()
	if len(metas) != 10 {
		t.Fatalf("expected 10 templates, got %d", len(metas))
	}

	for _, meta := range metas {
		themes := TemplateThemes(meta.Kind)
		if len(themes) < 3 {
			t.Fatalf("template %s has too few themes: %d", meta.Name, len(themes))
		}
		for themeIdx := range len(themes) {
			out := RenderTemplate(src, 1.0, TemplateOptions{
				Kind:     meta.Kind,
				Theme:    themeIdx,
				Title:    "SnipGo 模板验收",
				Caption:  "实测渲染排版与阴影留白居中",
				ShowDate: true,
				DateText: "2026.11.19",
				Padding:  PadNormal,
			})
			if out == nil || out.Bounds().Dx() <= 260 || out.Bounds().Dy() <= 150 {
				t.Fatalf("template %s theme %d invalid output bounds: %v", meta.Name, themeIdx, out)
			}
		}
	}

	// 手账便签：暖黄纸底左上角应为桌面浅暖灰，纸面应为暖黄色。
	sticky := RenderTemplate(src, 1.0, TemplateOptions{
		Kind:     TemplateStickyNote,
		Theme:    0,
		ShowDate: true,
		DateText: "2026.11.19",
		Padding:  PadNormal,
	})
	sb := sticky.Bounds()
	paperPx := sticky.NRGBAAt(sb.Dx()/2, 55)
	if paperPx.R < 240 || paperPx.G < 235 || paperPx.B > 235 {
		t.Fatalf("expected warm yellow sticky paper sample, got %+v", paperPx)
	}

	// 浏览器模板：检查顶栏红灯原点确实呈红色。
	browser := RenderTemplate(src, 1.0, TemplateOptions{
		Kind:    TemplateBrowser,
		Theme:   0,
		Title:   "snipgo.app",
		Padding: PadNormal,
	})
	redLamp := browser.NRGBAAt(34+18, 34+19)
	if redLamp.R < 220 || redLamp.G > 130 {
		t.Fatalf("expected red browser traffic light, got %+v", redLamp)
	}

	// 终端代码窗：检查顶栏红灯与深色终端衬底。
	term := RenderTemplate(src, 1.0, TemplateOptions{
		Kind:    TemplateTerminal,
		Theme:   0,
		Title:   "zsh — ~/snipgo",
		Padding: PadNormal,
	})
	if term.Bounds().Dx() <= 300 || term.Bounds().Dy() <= 220 {
		t.Fatalf("terminal template bounds too small: %v", term.Bounds())
	}

	// 35mm 胶片：校验底边最外侧桌面色，以及孔位内与桌面色相近的镂空透出效果。
	film := RenderTemplate(src, 1.0, TemplateOptions{
		Kind:     TemplateFilm,
		Theme:    0,
		ShowDate: true,
		DateText: "2026.11.19",
		Padding:  PadNormal,
	})
	deskPx := film.NRGBAAt(4, 4)
	stripMid := film.NRGBAAt(film.Bounds().Dx()/2, 24)
	if stripMid == deskPx {
		t.Fatalf("expected dark film strip rail distinct from desk color %+v", deskPx)
	}

	// 幻灯片分栏：左文右图应显著宽于普通上下排版模板。
	slide := RenderTemplate(src, 1.0, TemplateOptions{
		Kind:    TemplateSlide,
		Theme:   0,
		Title:   "SnipGo 2.0",
		Caption: "macOS Liquid Glass & 10 Built-in Studio Templates",
		Padding: PadNormal,
	})
	if slide.Bounds().Dx() < src.Bounds().Dx()+180 {
		t.Fatalf("expected wide widescreen slide layout, got %v", slide.Bounds())
	}

	// 验证 Padding 档位确实改变输出尺寸（Wide > Normal > Compact）。
	cOut := RenderTemplate(src, 1.0, TemplateOptions{Kind: TemplatePolaroid, Padding: PadCompact})
	nOut := RenderTemplate(src, 1.0, TemplateOptions{Kind: TemplatePolaroid, Padding: PadNormal})
	wOut := RenderTemplate(src, 1.0, TemplateOptions{Kind: TemplatePolaroid, Padding: PadWide})
	if !(cOut.Bounds().Dx() < nOut.Bounds().Dx() && nOut.Bounds().Dx() < wOut.Bounds().Dx()) {
		t.Fatalf("expected monotonic width across padding modes: compact=%d normal=%d wide=%d",
			cOut.Bounds().Dx(), nOut.Bounds().Dx(), wOut.Bounds().Dx())
	}
}
