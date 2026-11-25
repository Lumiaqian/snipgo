package main

import (
	"image"
	"strings"
	"testing"

	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/scrollcap"
	"snipgo/internal/session"
	"snipgo/internal/store"
)

func TestOverlayAndPinTextReEditFlow(t *testing.T) {
	bounds := image.Rect(0, 0, 400, 260)
	a := &app{cfg: config.Default(t.TempDir())}
	o := &overlay{bounds: bounds, index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		OnMain:     func(fn func()) { fn() },
		Background: func(fn func()) { fn() },
		Launch: func(gen int) {
			a.session.Deliver(gen, session.Snapshot{
				Frames: []crop.Screen{{Bounds: bounds, Scale: 2, Pixels: makeTestSampleImage(bounds.Dx()*2, bounds.Dy()*2)}},
			}, nil)
		},
		UI: testOCRUI{},
	}
	a.session.StartFrozen()
	a.session.BeginDrag(0, 20, 20)
	a.session.MoveDrag(0, 340, 220)
	a.session.EndDrag()

	ac := a.session.Annotations()
	if !ac.AddText(60, 70, "初始批注") {
		t.Fatal("AddText failed")
	}

	// 双击选区内已有文字进入预填重编态
	if !a.onCanvasInput(o, ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, X: 72, Y: 78, Clicks: 2}) {
		t.Fatal("double click on existing ToolText should be handled")
	}
	if !a.textEditing || a.textEditIdx != 0 || a.textEditBuf != "初始批注" {
		t.Fatalf("unexpected text edit state: editing=%v idx=%d buf=%q", a.textEditing, a.textEditIdx, a.textEditBuf)
	}

	// Esc 取消重编不修改原内容
	tt := ui.NewTester(a.selectionView(o), bounds.Dx(), bounds.Dy())
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.textEditing || ac.Items()[0].Text != "初始批注" {
		t.Fatalf("Esc should cancel re-edit and keep original text, editing=%v item=%+v", a.textEditing, ac.Items()[0])
	}

	// 验证输入框宽度随文本长度自适应扩展
	a.textEditing = true
	a.textEditX, a.textEditY = 40, 60
	a.textEditBuf = "短"
	wTT := ui.NewTester(func(c *ui.Context) {
		a.paintTextEditor(c, o)
	}, bounds.Dx(), bounds.Dy())
	wTT.Frame()
	rShort, okShort := wTT.Find("文字批注输入框")
	if !okShort {
		t.Fatal("missing 文字批注输入框 for short text")
	}
	a.textEditBuf = "这是一段明显更长的文字批注内容用于验证输入框自动加宽"
	wTT.Frame()
	rLong, okLong := wTT.Find("文字批注输入框")
	if !okLong || rLong.W <= rShort.W {
		t.Fatalf("expected adaptive width to grow: short=%+v long=%+v", rShort, rLong)
	}
	a.textEditing = false
	a.textEditIdx = -1
	a.textEditBuf = ""

	// 再次双击修改并提交，随后验证清空删除 + Undo 恢复
	if !a.onCanvasInput(o, ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, X: 72, Y: 78, Clicks: 2}) {
		t.Fatal("second double click should enter edit mode")
	}
	a.textEditBuf = "更新后批注"
	a.commitOrCloseTextEdit()
	if ac.Items()[0].Text != "更新后批注" {
		t.Fatalf("Text after commit = %q, want 更新后批注", ac.Items()[0].Text)
	}
	if !a.onCanvasInput(o, ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, X: 72, Y: 78, Clicks: 2}) {
		t.Fatal("third double click should enter edit mode")
	}
	a.textEditBuf = "   "
	a.commitOrCloseTextEdit()
	if len(ac.Items()) != 0 {
		t.Fatalf("expected empty commit to delete item, got %d", len(ac.Items()))
	}
	if !ac.Undo() || len(ac.Items()) != 1 || ac.Items()[0].Text != "更新后批注" {
		t.Fatalf("Undo should restore deleted text item, items=%+v", ac.Items())
	}

	// 贴图编辑态同样支持双击改字与提交删除
	img := makeTestSampleImage(480, 320)
	bmp := ui.NewBitmap(img)
	p := &pinWin{
		img:         img,
		baseImg:     img,
		bmp:         bmp,
		baseBmp:     bmp,
		origW:       480,
		origH:       320,
		curW:        480,
		curH:        320,
		zoom:        1.0,
		opacity:     100,
		editMode:    true,
		textEditIdx: -1,
		annot:       annotate.NewCanvas(),
	}
	p.annot.AddText(80, 90, "贴图批注")
	pinTT := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, 480, 320)
	pinTT.Frame()
	pinTT.ClickAt(95, 98)
	pinTT.ClickAt(95, 98)
	pinTT.Frame()
	if !p.textEditing || p.textEditIdx != 0 || p.textEditBuf != "贴图批注" {
		t.Fatalf("expected pin double click to enter text re-edit, editing=%v idx=%d buf=%q", p.textEditing, p.textEditIdx, p.textEditBuf)
	}
	p.textEditBuf = "贴图新文案"
	p.commitOrClosePinTextEdit()
	if p.annot.Items()[0].Text != "贴图新文案" {
		t.Fatalf("pin Text = %q, want 贴图新文案", p.annot.Items()[0].Text)
	}
}

func TestPinEditFullParityStyleBarAndHotkeys(t *testing.T) {
	img := makeTestSampleImage(480, 320)
	bmp := ui.NewBitmap(img)
	p := &pinWin{
		img:         img,
		baseImg:     img,
		bmp:         bmp,
		baseBmp:     bmp,
		origW:       480,
		origH:       320,
		curW:        480,
		curH:        320,
		zoom:        1.0,
		opacity:     100,
		editMode:    true,
		textEditIdx: -1,
		annot:       annotate.NewCanvas(),
	}
	p.annot.Tool = annotate.ToolLoupe
	p.annot.BeginPointer(120, 120)
	p.annot.EndPointer(120, 120)
	a := &app{cfg: config.Default(t.TempDir()), pins: []*pinWin{p}}

	// 放大镜工具且已选中图元（显示颜色 + 粗细 + 删除 + 放大镜倍率全套样式栏）在 480x320 下无裁切/重叠。
	tt := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, 480, 320)
	opts := uitest.AuditOptions{
		RequiredLabels: []string{"贴图标注栏", "贴图样式栏"},
		RequiredInteractive: []string{
			"矩形", "椭圆", "箭头", "荧光笔", "序号", "马赛克", "文字", "聚光", "无痕擦除", "放大镜特写",
			"撤销贴图标注", "完成", "删除贴图标注", "2×", "2.5×", "3×", "4×",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	// 1~9 / 0 数字键切换贴图工具，再按相同数字键取消工具
	tt.Key(0, ui.Key7)
	tt.Frame()
	if p.annot.Tool != annotate.ToolText {
		t.Fatalf("Key7 Tool = %v, want ToolText", p.annot.Tool)
	}
	tt.Key(0, ui.Key8)
	tt.Frame()
	if p.annot.Tool != annotate.ToolSpotlight {
		t.Fatalf("Key8 Tool = %v, want ToolSpotlight", p.annot.Tool)
	}
	tt.Key(0, ui.Key8)
	tt.Frame()
	if p.annot.Tool != annotate.ToolNone {
		t.Fatalf("second Key8 Tool = %v, want ToolNone", p.annot.Tool)
	}

	// 文字输入态屏蔽 1~9 单键快捷键，避免打断输入
	p.textEditing = true
	p.annot.Tool = annotate.ToolText
	tt.Key(0, ui.Key1)
	tt.Frame()
	if p.annot.Tool != annotate.ToolText {
		t.Fatalf("expected digit shortcut ignored while typing, got tool=%v", p.annot.Tool)
	}
}

func TestPinZoomShortcutsAndNotices(t *testing.T) {
	img := makeTestSampleImage(400, 300)
	bmp := ui.NewBitmap(img)
	p := &pinWin{
		img:         img,
		baseImg:     img,
		bmp:         bmp,
		baseBmp:     bmp,
		origW:       400,
		origH:       300,
		curW:        400,
		curH:        300,
		zoom:        1.0,
		opacity:     100,
		textEditIdx: -1,
		annot:       annotate.NewCanvas(),
	}
	a := &app{cfg: config.Default(t.TempDir()), pins: []*pinWin{p}}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pinView(c, p)
	}, 400, 300)
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if p.curW <= 400 || p.paletteToastFallback != "115%" {
		t.Fatalf("after Cmd+= curW=%d notice=%q, want >400 and 115%%", p.curW, p.paletteToastFallback)
	}

	tt.Key(ui.Cmd, ui.KeyMinus)
	tt.Frame()
	if p.curW >= 460 {
		t.Fatalf("after Cmd+- curW=%d, want smaller than 460", p.curW)
	}

	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if p.zoom != 1.0 || p.curW != 400 || p.curH != 300 || p.paletteToastFallback != "100%" {
		t.Fatalf("after Cmd+0 zoom=%v size=%dx%d notice=%q", p.zoom, p.curW, p.curH, p.paletteToastFallback)
	}
}

func TestTemplatePrefsPersistenceAndLoad(t *testing.T) {
	dir := t.TempDir()
	a := &app{userDataDir: dir, cfg: config.Default(dir)}
	want := beautify.TemplateOptions{
		Kind:     beautify.TemplatePostcard,
		Theme:    2,
		Padding:  beautify.PadWide,
		ShowDate: false,
	}
	a.persistTemplatePrefs(want)

	a2 := &app{userDataDir: dir, cfg: config.Load(dir, dir)}
	got := a2.templateOptionsFromConfig()
	if got.Kind != want.Kind || got.Theme != want.Theme || got.Padding != want.Padding || got.ShowDate != want.ShowDate {
		t.Fatalf("reloaded template prefs = %+v, want %+v", got, want)
	}
}

func TestScrollCapturePauseShortcutsAndPausedAudit(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}
	sc := &scrollCaptureSession{
		stitch: scrollcap.New(),
		pxW:    400,
		pxH:    900,
		frames: 4,
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.paintScrollCtrlView(c, sc)
	}, scrollCtrlW, scrollCtrlH)
	tt.Frame()

	tt.Key(0, ui.KeySpace)
	tt.Frame()
	sc.mu.Lock()
	paused := sc.paused
	sc.mu.Unlock()
	if !paused || !tt.HasText("已暂停") {
		t.Fatalf("expected Space to toggle paused state & badge, paused=%v texts=%v", paused, tt.Texts())
	}

	opts := uitest.AuditOptions{
		RequiredInteractive: []string{"取消长截图", "继续拼接", "长图贴图", "保存长图", "完成复制"},
		RequireFocusable:    true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	if err := tt.Click("继续拼接"); err != nil {
		t.Fatalf("Click 继续拼接: %v", err)
	}
	tt.Frame()
	sc.mu.Lock()
	resumed := !sc.paused
	sc.mu.Unlock()
	if !resumed || !tt.HasText("拼接中") {
		t.Fatalf("expected 继续拼接 click to resume, resumed=%v", resumed)
	}
}

func TestOCRShortcutsAndExpandLinks(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}
	var copied []string
	ow := &ocrWindow{
		hasTable:  true,
		mode:      ocrModePlain,
		plainText: "alpha beta",
		mdText:    "| col1 | col2 |\n| --- | --- |\n| 1 | 2 |",
		mdReady:   true,
		tsvText:   "col1\tcol2\n1\t2",
		tsvReady:  true,
		text:      "alpha beta",
		links:     []string{"https://a.example", "https://b.example", "https://c.example"},
		onCopyText: func(s string) {
			copied = append(copied, s)
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintOCRView(c, ow)
	}, ocrWinW, ocrWinH)
	tt.Frame()
	if tt.HasText("https://c.example") {
		t.Fatal("expected third link to stay collapsed before expanding")
	}

	uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
		RequiredInteractive: []string{"展开全部链接"},
		RequireFocusable:    true,
	})

	if err := tt.Click("展开全部链接"); err != nil {
		t.Fatalf("Click 展开全部链接: %v", err)
	}
	tt.Frame()
	if !tt.HasText("https://c.example") {
		t.Fatal("expected third link visible after expanding")
	}

	// Tab 切换视图格式，Cmd+C 复制当前视图正文
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if ow.mode != ocrModeMarkdown {
		t.Fatalf("first Tab mode = %v, want ocrModeMarkdown", ow.mode)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if len(copied) == 0 || !strings.Contains(copied[len(copied)-1], "| col1 | col2 |") {
		t.Fatalf("Cmd+C should copy Markdown text, copied=%v", copied)
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if ow.mode != ocrModeTSV {
		t.Fatalf("second Tab mode = %v, want ocrModeTSV", ow.mode)
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if ow.mode != ocrModePlain {
		t.Fatalf("third Tab mode = %v, want ocrModePlain", ow.mode)
	}
}

func TestCountdownTriggerNow(t *testing.T) {
	if countdownWinW != 308 || countdownWinH != 42 {
		t.Fatalf("unexpected countdown geometry: %dx%d", countdownWinW, countdownWinH)
	}
	a := &app{cfg: config.Default(t.TempDir())}
	cd := &countdownSession{remaining: 4}
	a.countdown = cd

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintCountdownView(c, cd)
	}, countdownWinW, countdownWinH)
	tt.Frame()
	uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
		RequiredInteractive: []string{"立即定格", "取消"},
		RequireFocusable:    true,
	})

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.countdown != nil {
		t.Fatal("expected Enter on countdown capsule to clear countdown and trigger freeze")
	}
}

func TestHistorySaveAndDigitQuickCopy(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir, 5)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if _, err := st.Add(testHistoryPNG(80, 60), 80, 60, 0); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	if _, err := st.Add(testHistoryPNG(100, 75), 100, 75, 0); err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	var saved [][]byte
	cfg := config.Default(dir)
	cfg.QuickSave = true
	a := &app{
		cfg:     cfg,
		history: st,
		savePNGHook: func(data []byte, _ bool) {
			saved = append(saved, data)
		},
	}
	var copiedPNG [][]byte
	hw := &historyWindow{
		entries: st.List(),
		thumbs:  map[string]*ui.Bitmap{},
		onCopyPNG: func(raw []byte) {
			copiedPNG = append(copiedPNG, raw)
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintHistoryView(c, hw)
	}, historyWinW, historyWinH)
	tt.Frame()

	tt.Key(0, ui.Key2)
	tt.Frame()
	if len(copiedPNG) != 1 || hw.statusBadge != "已复制到剪贴板" {
		t.Fatalf("Key2 copied=%d statusBadge=%q, want 1 copy and 已复制到剪贴板", len(copiedPNG), hw.statusBadge)
	}

	if err := tt.Click("保存历史 1"); err != nil {
		t.Fatalf("Click 保存历史 1: %v", err)
	}
	tt.Frame()
	if len(saved) != 1 || len(saved[0]) == 0 {
		t.Fatalf("expected 保存历史 1 to invoke SavePNG with image bytes, got %d saves", len(saved))
	}
	if hw.statusBadge != "已保存 PNG" {
		t.Fatalf("save statusBadge = %q, want 已保存 PNG", hw.statusBadge)
	}
}
