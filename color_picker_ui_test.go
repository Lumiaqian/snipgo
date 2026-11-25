package main

import (
	"image"
	"image/color"
	"testing"

	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

func makeSplitColorScreen(bounds image.Rectangle, leftCol, rightCol color.NRGBA) crop.Screen {
	img := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	midX := bounds.Dx() / 2
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			if x < midX {
				img.SetNRGBA(x, y, leftCol)
			} else {
				img.SetNRGBA(x, y, rightCol)
			}
		}
	}
	return crop.Screen{Bounds: bounds, Scale: 1, Pixels: img}
}

func TestAgentReadyPaletteBarInteractive(t *testing.T) {
	sky := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	dark := color.NRGBA{R: 24, G: 24, B: 27, A: 255}
	bounds := image.Rect(0, 0, 1440, 120)

	var copied []string
	a := &app{
		cfg: config.Default(t.TempDir()),
		clipboardTextHook: func(s string) {
			copied = append(copied, s)
		},
	}
	o := &overlay{bounds: bounds, index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		OnMain: func(fn func()) { fn() },
		Launch: func(gen int) {
			a.session.Deliver(gen, session.Snapshot{Frames: []crop.Screen{makeSplitColorScreen(bounds, sky, dark)}}, nil)
		},
		UI: testOCRUI{},
	}
	a.session.StartFrozen()
	a.session.Hover(100, 40)
	a.session.SampleColorToPalette(nil)
	a.session.Hover(1000, 40)
	a.session.SampleColorToPalette(nil)

	tt := ui.NewTester(func(c *ui.Context) {
		withAppTheme(c, func() {
			a.paintPickedPaletteBar(c, o, bounds)
		})
	}, bounds.Dx(), bounds.Dy())

	opts := uitest.AuditOptions{
		RequiredLabels: []string{"已采集色板栏"},
		RequiredInteractive: []string{
			"复制色点 1 (#0EA5E9)",
			"复制色点 2 (#18181B)",
			"切换取色格式",
			"复制 CSS 变量",
			"贴出多色卡",
			"清空已采集色板",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)
	if !tt.HasText("复制代码") || !tt.HasText("贴色卡 (⇧P)") || !tt.HasText("清空") {
		t.Fatal("expected palette bar action labels to be visible")
	}

	wantContrast := beautify.FormatContrastSummary(sky, dark)
	if !tt.HasText(wantContrast) {
		t.Fatalf("expected WCAG contrast %q in palette bar", wantContrast)
	}

	if err := tt.Click("复制色点 1 (#0EA5E9)"); err != nil {
		t.Fatalf("click swatch 1: %v", err)
	}
	tt.Frame()
	if len(copied) == 0 || copied[len(copied)-1] != "#0EA5E9" {
		t.Fatalf("copied after swatch 1 click = %v, want #0EA5E9", copied)
	}

	if err := tt.Click("切换取色格式"); err != nil {
		t.Fatalf("click format button: %v", err)
	}
	tt.Frame()
	if a.session.ColorFormat() != beautify.ColorRGB {
		t.Fatalf("format after click = %v, want ColorRGB", a.session.ColorFormat())
	}

	if err := tt.Click("复制 CSS 变量"); err != nil {
		t.Fatalf("click 复制 CSS 变量: %v", err)
	}
	tt.Frame()
	wantCSS := beautify.FormatPaletteCSS([]color.NRGBA{sky, dark}, beautify.ColorRGB)
	if len(copied) == 0 || copied[len(copied)-1] != wantCSS {
		t.Fatalf("copied CSS = %q, want %q", copied[len(copied)-1], wantCSS)
	}

	if err := tt.RightClick("复制色点 1 (rgb(14, 165, 233))"); err != nil {
		t.Fatalf("right click swatch 1: %v", err)
	}
	tt.Frame()
	if got := a.session.PickedColors(); len(got) != 1 || got[0] != dark {
		t.Fatalf("picked colors after right click remove = %+v, want [%+v]", got, dark)
	}
}

func TestAgentReadyInteractiveColorSwatchPin(t *testing.T) {
	sky := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	card := beautify.RenderTextPin("#0EA5E9", 2.0)
	dipW := card.Bounds().Dx() / 2
	dipH := card.Bounds().Dy() / 2

	var copied []string
	a := &app{
		cfg: config.Default(t.TempDir()),
		clipboardTextHook: func(s string) {
			copied = append(copied, s)
		},
	}
	bmp := ui.NewBitmap(card)
	p := &pinWin{
		img:           card,
		baseImg:       card,
		bmp:           bmp,
		baseBmp:       bmp,
		png:           []byte{1, 2, 3},
		origW:         dipW,
		origH:         dipH,
		curW:          dipW,
		curH:          dipH,
		zoom:          1.0,
		opacity:       100,
		annot:         annotate.NewCanvas(),
		paletteColors: []color.NRGBA{sky},
		paletteFmt:    beautify.ColorHEX,
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, dipW, dipH)

	opts := uitest.AuditOptions{
		RequiredInteractive: []string{
			"复制 HEX 色值",
			"复制 RGB 色值",
			"复制 OKLCH 色值",
			"复制 Tailwind 色值",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	if err := tt.Click("复制 HEX 色值"); err != nil {
		t.Fatalf("click HEX row: %v", err)
	}
	tt.Frame()
	if len(copied) == 0 || copied[len(copied)-1] != "#0EA5E9" {
		t.Fatalf("copied HEX = %v", copied)
	}
	if !tt.HasText("已复制 #0EA5E9") {
		t.Fatal("expected swatch pin toast badge for HEX")
	}

	if err := tt.Click("复制 OKLCH 色值"); err != nil {
		t.Fatalf("click OKLCH row: %v", err)
	}
	tt.Frame()
	wantOKLCH := beautify.FormatColor(sky, beautify.ColorOKLCH)
	if len(copied) == 0 || copied[len(copied)-1] != wantOKLCH {
		t.Fatalf("copied OKLCH = %v, want %q", copied, wantOKLCH)
	}
}

func TestAgentReadyInteractivePalettePin(t *testing.T) {
	cols := []color.NRGBA{
		{R: 14, G: 165, B: 233, A: 255},
		{R: 24, G: 24, B: 27, A: 255},
	}
	palImg := beautify.RenderPaletteCard(cols, 2.0)
	dipW := palImg.Bounds().Dx() / 2
	dipH := palImg.Bounds().Dy() / 2

	var copied []string
	a := &app{
		cfg: config.Default(t.TempDir()),
		clipboardTextHook: func(s string) {
			copied = append(copied, s)
		},
	}
	bmp := ui.NewBitmap(palImg)
	p := &pinWin{
		img:           palImg,
		baseImg:       palImg,
		bmp:           bmp,
		baseBmp:       bmp,
		png:           []byte{1, 2, 3},
		origW:         dipW,
		origH:         dipH,
		curW:          dipW,
		curH:          dipH,
		zoom:          1.0,
		opacity:       100,
		annot:         annotate.NewCanvas(),
		paletteColors: cols,
		paletteFmt:    beautify.ColorHEX,
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, dipW, dipH)
	tt.Move(float32(dipW)/2, float32(dipH)/2)
	tt.Frame()

	opts := uitest.AuditOptions{
		RequiredLabels: []string{"贴图工具栏"},
		RequiredInteractive: []string{
			"复制色块 1 (#0EA5E9)",
			"复制色块 2 (#18181B)",
			"复制 CSS",
			"复制贴图",
			"关闭贴图",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	if err := tt.Click("复制色块 1 (#0EA5E9)"); err != nil {
		t.Fatalf("click swatch 1: %v", err)
	}
	tt.Frame()
	if len(copied) == 0 || copied[len(copied)-1] != "#0EA5E9" {
		t.Fatalf("copied swatch 1 = %v", copied)
	}

	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if p.paletteFmt != beautify.ColorRGB || !tt.HasText("格式: RGB") {
		t.Fatalf("expected Tab to switch pin palette format to RGB, got %v", p.paletteFmt)
	}

	if err := tt.Click("复制色块 2 (rgb(24, 24, 27))"); err != nil {
		t.Fatalf("click swatch 2: %v", err)
	}
	tt.Frame()
	if len(copied) == 0 || copied[len(copied)-1] != "rgb(24, 24, 27)" {
		t.Fatalf("copied swatch 2 = %v", copied)
	}

	if err := tt.Click("复制 CSS"); err != nil {
		t.Fatalf("click 复制 CSS hover button: %v", err)
	}
	tt.Frame()
	wantCSS := beautify.FormatPaletteCSS(cols, beautify.ColorRGB)
	if len(copied) == 0 || copied[len(copied)-1] != wantCSS {
		t.Fatalf("copied CSS = %q, want %q", copied[len(copied)-1], wantCSS)
	}
}

func TestColorToastRendersInLightAndDarkTheme(t *testing.T) {
	col := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	tt := ui.NewTester(func(c *ui.Context) {
		paintColorToast(c, "#0EA5E9", col)
	}, 200, 36)

	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		if !tt.HasText("已复制 #0EA5E9") {
			t.Fatalf("dark=%v expected toast text 已复制 #0EA5E9", dark)
		}
	}
}

func TestColorPickerBannerAndLoupeRender(t *testing.T) {
	sky := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	bounds := image.Rect(0, 0, 1440, 900)
	a := &app{cfg: config.Default(t.TempDir())}
	o := &overlay{bounds: bounds, index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		OnMain: func(fn func()) { fn() },
		Launch: func(gen int) {
			a.session.Deliver(gen, session.Snapshot{Frames: []crop.Screen{makeSplitColorScreen(bounds, sky, sky)}}, nil)
		},
		UI: testOCRUI{},
	}
	a.session.StartColorPicker()
	a.session.InitCursor(300, 300)

	tt := ui.NewTester(a.selectionView(o), 1440, 900)
	tt.Frame()
	if !tt.HasText("屏幕取色器 · 左键 / C 复制 · ⇧C 去前缀 · P / ⇧左键 采色 · Tab 切格式 · Esc 退出") {
		t.Fatal("expected standalone color picker hint banner")
	}
	if !tt.HasText("#0EA5E9") {
		t.Fatal("expected #0EA5E9 in color picker loupe")
	}
}
