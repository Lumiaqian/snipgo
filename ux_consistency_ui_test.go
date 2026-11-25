package main

import (
	"context"
	"image"
	"testing"

	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

func TestActionToastRendersInLightAndDarkTheme(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		paintActionToast(c, "已快存 SnipGo.png")
	}, 260, 36)

	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		if !tt.HasText("已快存 SnipGo.png") {
			t.Fatalf("dark=%v expected toast text 已快存 SnipGo.png", dark)
		}
	}
}

func TestAutoOCRBannerFollowsCursorDisplay(t *testing.T) {
	b0 := image.Rect(0, 0, 1440, 900)
	b1 := image.Rect(1440, 0, 2880, 900)
	a := &app{cfg: config.Default(t.TempDir())}
	o0 := &overlay{bounds: b0, index: 0}
	o1 := &overlay{bounds: b1, index: 1}
	a.overlays = []*overlay{o0, o1}
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames: []crop.Screen{
					{Bounds: b0, Scale: 2},
					{Bounds: b1, Scale: 2},
				},
			}, nil
		},
		UI: testOCRUI{},
	}
	a.session.StartLiveOCR()
	// Place cursor on secondary display o1.
	a.session.InitCursor(1800, 300)

	tt0 := ui.NewTester(a.selectionView(o0), b0.Dx(), b0.Dy())
	tt0.Frame()
	if tt0.HasText("识图模式") {
		t.Fatal("primary display should not show AutoOCR banner when cursor is on display 1")
	}

	tt1 := ui.NewTester(a.selectionView(o1), b1.Dx(), b1.Dy())
	tt1.Frame()
	if !tt1.HasText("识图模式") {
		t.Fatal("secondary display owning cursor should show AutoOCR banner")
	}
}

func TestRegularPinCopyFeedbackAndSaveAsShortcuts(t *testing.T) {
	img := makeTestSampleImage(480, 320)
	bmp := ui.NewBitmap(img)
	var saveModes []bool
	a := &app{
		cfg: config.Default(t.TempDir()),
		savePNGHook: func(_ []byte, forceDialog bool) {
			saveModes = append(saveModes, forceDialog)
		},
	}
	p := &pinWin{
		img:     img,
		baseImg: img,
		bmp:     bmp,
		baseBmp: bmp,
		png:     []byte{1, 2, 3},
		origW:   480,
		origH:   320,
		curW:    480,
		curH:    320,
		zoom:    1.0,
		opacity: 100,
		annot:   annotate.NewCanvas(),
	}
	a.pins = []*pinWin{p}

	tt := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, 480, 320)
	tt.Move(240, 160)
	tt.Frame()

	opts := uitest.AuditOptions{
		RequiredLabels:      []string{"贴图工具栏"},
		RequiredInteractive: []string{"复制贴图", "关闭贴图"},
		RequireFocusable:    true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	if err := tt.Click("复制贴图"); err != nil {
		t.Fatalf("click 复制贴图: %v", err)
	}
	tt.Frame()
	if !tt.HasText("已复制图像") {
		t.Fatal("expected 已复制图像 bottom notice after clicking 复制贴图")
	}

	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.Frame()
	if len(saveModes) != 2 || saveModes[0] != false || saveModes[1] != true {
		t.Fatalf("pin saveModes = %v, want [false true] (Cmd+S, Shift+Cmd+S)", saveModes)
	}
}

func TestTemplateStudioSaveAndSaveAsShortcuts(t *testing.T) {
	src := makeTestSampleImage(320, 200)
	var saveModes []bool
	a := &app{
		cfg: config.Default(t.TempDir()),
		savePNGHook: func(_ []byte, forceDialog bool) {
			saveModes = append(saveModes, forceDialog)
		},
	}
	opts := beautify.DefaultTemplateOptions()
	tw := &templateWindow{
		srcImg:        src,
		srcScale:      2.0,
		previewSrc:    src,
		previewScale:  2.0,
		opts:          opts,
		renderedValid: true,
		renderedOpts:  opts,
		fullCacheImg:  src,
		fullCachePNG:  []byte{1, 2, 3},
		fullCacheOpts: opts,
	}
	tw.refreshPreview()

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintTemplateView(c, tw)
	}, templateWinW, templateWinH)
	tt.Frame()

	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	if len(saveModes) != 2 || saveModes[0] != true || saveModes[1] != false {
		t.Fatalf("template saveModes = %v, want [true false] (Shift+Cmd+S, Cmd+S)", saveModes)
	}
}

func TestOverlayShiftCmdSSaveAsAndLiveKeyR(t *testing.T) {
	bounds := image.Rect(0, 0, 1440, 900)
	sel := image.Rect(200, 160, 800, 500)
	var saveModes []bool
	a := &app{
		cfg: config.Default(t.TempDir()),
		savePNGHook: func(_ []byte, forceDialog bool) {
			saveModes = append(saveModes, forceDialog)
		},
	}
	o := &overlay{bounds: bounds, index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		OnMain:     func(fn func()) { fn() },
		Background: func(fn func()) { fn() },
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: bounds, Scale: 2}},
				FrontWindow: sel,
				FrontWinID:  42,
				Windows:     []image.Rectangle{sel},
				WindowInfos: []session.WinTarget{{ID: 42, Bounds: sel}},
			}, nil
		},
		CaptureRect: func(context.Context, image.Rectangle) (*image.NRGBA, error) {
			return makeTestSampleImage(100, 100), nil
		},
		Launch: func(gen int) {
			a.session.Deliver(gen, session.Snapshot{
				Frames: []crop.Screen{{Bounds: bounds, Scale: 2, Pixels: makeTestSampleImage(bounds.Dx()*2, bounds.Dy()*2)}},
			}, nil)
		},
		UI:       testOCRUI{},
		OnSaveAs: a.SavePNGAs,
	}

	// Verify R in live PhaseSelecting switches cleanly into frozen MeasureMode.
	a.session.StartLive()
	a.session.InitCursor(300, 220)
	tt := ui.NewTester(a.selectionView(o), bounds.Dx(), bounds.Dy())
	tt.Frame()
	tt.Key(0, ui.KeyR)
	tt.Frame()
	if !a.session.MeasureMode() {
		t.Fatal("expected KeyR in live PhaseSelecting to switch into frozen MeasureMode")
	}

	// Lock a selection and test Shift+Cmd+S vs Cmd+S.
	a.session.StartFrozen()
	tt.Frame()
	a.session.BeginDrag(0, 200, 160)
	a.session.MoveDrag(0, 500, 420)
	a.session.EndDrag()
	tt.Frame()

	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.Frame()
	if len(saveModes) != 1 || saveModes[0] != true {
		t.Fatalf("overlay Shift+Cmd+S saveModes = %v, want [true]", saveModes)
	}

	a.session.StartFrozen()
	tt.Frame()
	a.session.BeginDrag(0, 200, 160)
	a.session.MoveDrag(0, 500, 420)
	a.session.EndDrag()
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	// ActionSave calls s.UI.SavePNG (testOCRUI in this unit harness), verifying Cmd+S does NOT trigger OnSaveAs.
	if len(saveModes) != 1 {
		t.Fatalf("overlay Cmd+S should route via UI.SavePNG, not OnSaveAs; saveModes = %v", saveModes)
	}
}
