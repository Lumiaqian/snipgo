package main

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/scrollcap"
	"snipgo/internal/session"
	"snipgo/internal/store"
	"snipgo/internal/vision"
)

func makeTestSampleImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: 48, G: 112, B: 210, A: 255})
		}
	}
	return img
}

func TestUITestPilotOverlayToolbarAgentReady(t *testing.T) {
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 1440, 900), index: 0}
	a.overlays = []*overlay{o}
	sel := image.Rect(200, 160, 960, 520)
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2}},
				FrontWindow: sel,
				FrontWinID:  42,
				Windows:     []image.Rectangle{sel},
				WindowInfos: []session.WinTarget{{ID: 42, Bounds: sel}},
			}, nil
		},
		CaptureWindow: func(_ context.Context, _ uint32, _ float64) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, 100, 100)), nil
		},
		UI: testOCRUI{},
	}
	a.session.StartLive()
	a.session.InitCursor(500, 300)

	tt := ui.NewTester(a.selectionView(o), 1440, 900)
	tt.Frame()
	tt.ClickAt(500, 300)
	tt.Frame()

	// Click 放大 to activate loupe tool and open the style sub-toolbar.
	if err := tt.Click("放大"); err != nil {
		t.Fatalf("click 放大: %v", err)
	}
	tt.Frame()
	if a.session.Annotations().Tool != annotate.ToolLoupe {
		t.Fatalf("expected ToolLoupe after clicking 放大, got %v", a.session.Annotations().Tool)
	}
	if !tt.Focused("放大") {
		t.Fatalf("expected 放大 to keep focus while sub-bar is shown")
	}

	opts := uitest.AuditOptions{
		RequiredLabels: []string{"标注工具栏", "标注样式栏"},
		RequiredInteractive: []string{
			"框", "圆", "箭头", "放大", "撤销", "识图", "贴图", "保存", "取消", "复制",
			"颜色 1", "细", "中", "粗", "2×", "2.5×", "3×", "4×",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)

	// Verify active toggle and swatch radio roles are exposed to assistive tech / agents.
	loupeInfo, ok := tt.Access("放大")
	if !ok || loupeInfo.Role != ui.RoleToggleButton || !loupeInfo.Checked || !loupeInfo.Focusable {
		t.Fatalf("放大 access info=%+v ok=%v, want focusable checked RoleToggleButton", loupeInfo, ok)
	}
	colorInfo, ok := tt.Access("颜色 1")
	if !ok || colorInfo.Role != ui.RoleRadio || !colorInfo.Focusable {
		t.Fatalf("颜色 1 access info=%+v ok=%v, want focusable RoleRadio", colorInfo, ok)
	}

	// Verify single-click agent/accessibility driving works on every sub-bar control.
	if err := tt.Click("颜色 2"); err != nil {
		t.Fatalf("click 颜色 2: %v", err)
	}
	if a.session.Annotations().Color != annotate.Palette[1] {
		t.Fatalf("expected Color=Palette[1], got %+v", a.session.Annotations().Color)
	}
	if info, ok := tt.Access("颜色 2"); !ok || !info.Checked || !info.Focused {
		t.Fatalf("颜色 2 should report Checked=true Focused=true after single click: ok=%v info=%+v", ok, info)
	}

	if err := tt.Click("粗"); err != nil {
		t.Fatalf("click 粗: %v", err)
	}
	if a.session.Annotations().Stroke != 6.0 {
		t.Fatalf("expected Stroke=6.0, got %v", a.session.Annotations().Stroke)
	}

	if err := tt.Click("3×"); err != nil {
		t.Fatalf("click 3×: %v", err)
	}
	if a.session.Annotations().LoupeZoom != 3.0 {
		t.Fatalf("expected LoupeZoom=3.0, got %v", a.session.Annotations().LoupeZoom)
	}

	uitest.AssertThemeParity(t, tt, opts)
}

func TestLiquidSwitchAccessibleAndKeyboardToggle(t *testing.T) {
	on := false
	changes := 0
	tt := ui.NewTester(func(c *ui.Context) {
		withAppTheme(c, func() {
			ui.Column(c).Padding(16).Children(func() {
				if liquidSwitch(c, "demo", &on, "启用快存") {
					changes++
				}
			})
		})
	}, 360, 80)

	opts := uitest.AuditOptions{
		RequiredInteractive: []string{"启用快存"},
		RequireFocusable:    true,
	}
	uitest.AssertAgentReady(t, tt, opts)

	info, ok := tt.Access("启用快存")
	if !ok || info.Role != ui.RoleSwitch || info.Checked || !info.Focusable {
		t.Fatalf("initial switch info=%+v ok=%v, want unchecked focusable RoleSwitch", info, ok)
	}

	if err := tt.Click("启用快存"); err != nil {
		t.Fatalf("click switch: %v", err)
	}
	tt.Frame()
	if !on || changes != 1 {
		t.Fatalf("after click: on=%v changes=%d, want true 1", on, changes)
	}
	if info, ok := tt.Access("启用快存"); !ok || !info.Checked {
		t.Fatalf("after click: checked=%v ok=%v, want true", info.Checked, ok)
	}

	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if on || changes != 2 {
		t.Fatalf("after Space: on=%v changes=%d, want false 2", on, changes)
	}
}

func TestAgentReadySettingsAllTabs(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	cfg.SkippedVersion = "0.1.9"
	a := &app{
		userDataDir: dir,
		picturesDir: dir,
		cfg:         cfg,
	}
	sw := &settingsWindow{draft: cfg}

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintSettingsView(c, sw)
	}, settingsWinW, settingsWinH)

	// Tab 0: 外观与主题
	tab0Opts := uitest.AuditOptions{
		RequiredInteractive: []string{
			"关闭设置", "外观与主题", "全局快捷键", "通用与更新",
			"跟随系统", "浅色", "深色", "清透", "标准", "浓磨砂",
			"自动", "海湾蓝", "恢复默认", "完成",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, tab0Opts)
	uitest.AssertThemeParity(t, tt, tab0Opts)

	if err := tt.Click("海湾蓝"); err != nil {
		t.Fatalf("click 海湾蓝: %v", err)
	}
	tt.Frame()
	if sw.draft.AccentColor != "blue" {
		t.Fatalf("expected AccentColor=blue, got %q", sw.draft.AccentColor)
	}
	if info, ok := tt.Access("海湾蓝"); !ok || info.Role != ui.RoleRadio || !info.Checked {
		t.Fatalf("海湾蓝 access info=%+v ok=%v, want checked RoleRadio", info, ok)
	}

	// Tab 1: 全局快捷键（含重置与录制态）
	sw.draft.SnipShortcut = "Shift+Cmd+9"
	sw.tab = 1
	tt.Frame()
	tab1Opts := uitest.AuditOptions{
		RequiredInteractive: []string{
			"关闭设置", "重置区域截图快捷键", "录制区域截图", "录制截图识图 (OCR)", "恢复默认", "完成",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, tab1Opts)
	uitest.AssertThemeParity(t, tt, tab1Opts)

	sw.recordingID = "snip"
	tt.Frame()
	recOpts := uitest.AuditOptions{
		RequiredInteractive: []string{"关闭设置", "录制区域截图快捷键", "完成"},
		RequireFocusable:    true,
	}
	uitest.AssertAgentReady(t, tt, recOpts)
	sw.recordingID = ""

	// Tab 2: 通用与更新
	sw.tab = 2
	tt.Frame()
	tab2Opts := uitest.AuditOptions{
		RequiredInteractive: []string{
			"关闭设置",
			"按 ⌘S 免弹窗一键快存到默认文件夹（⇧⌘S 另存为）",
			"保存 PNG 文件时自动同时复制图像到剪贴板",
			"登录 macOS 时自动启动 SnipGo",
			"保存最近截图历史（关闭后不再记录新的截图）",
			"更改目录…", "3 秒", "5 秒", "10 秒",
			"恢复默认", "完成",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, tab2Opts)
	uitest.AssertThemeParity(t, tt, tab2Opts)
}

func TestAgentReadyTemplateStudio(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}
	src := makeTestSampleImage(320, 200)
	tw := &templateWindow{
		srcImg:       src,
		srcScale:     2.0,
		previewSrc:   src,
		previewScale: 2.0,
		opts:         beautify.DefaultTemplateOptions(),
	}
	tw.refreshPreview()

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintTemplateView(c, tw)
	}, templateWinW, templateWinH)

	opts := uitest.AuditOptions{
		RequiredLabels: []string{"拖出PNG"},
		RequiredInteractive: []string{
			"关闭模板工作台", "手账便签", "拍立得", "浏览器框",
			"卡片标题", "卡片配文", "显示日期水印",
			"紧凑", "标准", "宽幅",
			"保存 PNG", "贴图", "复制成图",
		},
		RequireFocusable: true,
	}
	uitest.AssertAgentReady(t, tt, opts)
	uitest.AssertThemeParity(t, tt, opts)

	if err := tt.Click("拍立得"); err != nil {
		t.Fatalf("click 拍立得: %v", err)
	}
	tt.Frame()
	if tw.opts.Kind != beautify.TemplatePolaroid {
		t.Fatalf("expected TemplatePolaroid, got %v", tw.opts.Kind)
	}
	if info, ok := tt.Access("拍立得"); !ok || info.Role != ui.RoleRadio || !info.Checked {
		t.Fatalf("拍立得 access info=%+v ok=%v, want checked RoleRadio", info, ok)
	}
}

func TestAgentReadyHistoryPanel(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}
	bmp := ui.NewBitmap(makeTestSampleImage(80, 60))

	t.Run("empty", func(t *testing.T) {
		hw := &historyWindow{thumbs: map[string]*ui.Bitmap{}}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintHistoryView(c, hw)
		}, historyWinW, historyWinH)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{"关闭历史"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})

	t.Run("multiple_entries", func(t *testing.T) {
		now := time.Now()
		hw := &historyWindow{
			entries: []store.Entry{
				{ID: "e1", Width: 640, Height: 480, CreatedAt: now},
				{ID: "e2", Width: 800, Height: 600, CreatedAt: now.Add(-time.Minute)},
			},
			thumbs: map[string]*ui.Bitmap{"e1": bmp, "e2": bmp},
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintHistoryView(c, hw)
		}, historyWinW, historyWinH)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{
				"关闭历史", "清空全部",
				"历史截图 1", "复制历史 1", "贴图历史 1", "套模板历史 1", "识图历史 1", "删除历史 1",
				"历史截图 2", "复制历史 2", "贴图历史 2", "套模板历史 2", "识图历史 2", "删除历史 2",
			},
			RequireFocusable: true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})
}

func TestAgentReadyOCRPanel(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}

	t.Run("table_and_links", func(t *testing.T) {
		var copied string
		tbl := vision.Table{
			Cols: 2,
			Rows: [][]string{
				{"项目", "状态"},
				{"SnipGo", "通过"},
				{"Loopwall", "通过"},
			},
		}
		ow := &ocrWindow{
			text:       tbl.Markdown(),
			plainText:  "SnipGo 通过\nLoopwall 通过",
			mdText:     tbl.Markdown(),
			mdReady:    true,
			tsvText:    tbl.TSV(),
			tsvReady:   true,
			table:      tbl,
			hasTable:   true,
			mode:       ocrModeMarkdown,
			links:      []string{"https://example.com/a", "https://example.com/b"},
			onCopyText: func(s string) { copied = s },
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintOCRView(c, ow)
		}, ocrWinW, ocrWinH)

		opts := uitest.AuditOptions{
			RequiredInteractive: []string{
				"关闭识图", "纯文本", "Markdown", "表格 TSV", "表格", "源码",
				"打开链接 1", "复制链接 1", "打开链接 2", "复制链接 2",
				"单元格 1-1：项目", "单元格 2-2：通过", "单元格 3-2：通过",
				"重新识图", "贴为便签", "复制 Markdown",
			},
			RequireFocusable: true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)

		if err := tt.Click("单元格 2-1：SnipGo"); err != nil {
			t.Fatalf("click table cell: %v", err)
		}
		if copied != "SnipGo" {
			t.Fatalf("expected copied cell SnipGo, got %q", copied)
		}
	})

	t.Run("plain_text_and_loading", func(t *testing.T) {
		ow := &ocrWindow{text: "hello world", plainText: "hello world"}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintOCRView(c, ow)
		}, ocrWinW, ocrWinH)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{"关闭识图", "识图文本", "重新识图", "贴为便签", "复制全文"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)

		ow.loading = true
		tt.Frame()
		uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
			RequiredInteractive: []string{"关闭识图", "重新识图"},
			RequireFocusable:    true,
		})
	})
}

func TestAgentReadyPinModes(t *testing.T) {
	img := makeTestSampleImage(480, 320)
	bmp := ui.NewBitmap(img)
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
	peer := &pinWin{img: img, bmp: bmp, curW: 480, curH: 320}
	a := &app{
		cfg:  config.Default(t.TempDir()),
		pins: []*pinWin{p, peer},
	}

	t.Run("hover_bar", func(t *testing.T) {
		tt := ui.NewTester(func(c *ui.Context) {
			a.pinView(c, p)
		}, 480, 320)
		tt.Move(240, 160)
		tt.Frame()
		opts := uitest.AuditOptions{
			RequiredLabels: []string{"贴图工具栏", "拖出文件"},
			RequiredInteractive: []string{
				"原地标注", "套模板", "主体抠图", "拉帘对比", "识图", "点击穿透", "折叠胶囊", "复制贴图", "关闭贴图",
			},
			RequireFocusable: true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})

	t.Run("edit_bar", func(t *testing.T) {
		p.editMode = true
		defer func() { p.editMode = false }()
		tt := ui.NewTester(func(c *ui.Context) {
			a.pinView(c, p)
		}, 480, 320)
		opts := uitest.AuditOptions{
			RequiredLabels: []string{"贴图标注栏"},
			RequiredInteractive: []string{
				"矩形", "椭圆", "箭头", "荧光笔", "序号", "马赛克", "无痕擦除", "放大镜特写", "撤销贴图标注", "完成",
			},
			RequireFocusable: true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})

	t.Run("compare_bar", func(t *testing.T) {
		p.compareMode = true
		p.compareSplit = 0.5
		p.comparePeer = img
		p.comparePeerBmp = bmp
		defer func() { p.compareMode = false }()
		tt := ui.NewTester(func(c *ui.Context) {
			a.pinView(c, p)
		}, 480, 320)
		opts := uitest.AuditOptions{
			RequiredLabels:      []string{"贴图对比栏"},
			RequiredInteractive: []string{"拉帘", "热力图 · 0.00%", "复制", "退出"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})

	t.Run("collapsed_pill", func(t *testing.T) {
		p.collapsed = true
		defer func() { p.collapsed = false }()
		tt := ui.NewTester(func(c *ui.Context) {
			a.pinView(c, p)
		}, collapsedPillW, collapsedPillH)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{"展开"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})
}

func TestAgentReadyScrollUpdateCountdown(t *testing.T) {
	a := &app{cfg: config.Default(t.TempDir())}

	t.Run("scroll_ctrl", func(t *testing.T) {
		sc := &scrollCaptureSession{
			stitch: scrollcap.New(),
			pxW:    400,
			pxH:    900,
			frames: 4,
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintScrollCtrlView(c, sc)
		}, scrollCtrlW, scrollCtrlH)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{"取消长截图", "长图贴图", "保存长图", "完成复制"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})

	t.Run("updater_states", func(t *testing.T) {
		u := &updateManager{
			state:         updAvailable,
			canSelfUpdate: true,
			release: &updateCandidate{
				Version: "0.1.9",
				Notes:   "## 更新内容\n- 完善全浮窗无障碍与键盘导航",
				Size:    8 * 1024 * 1024,
			},
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintUpdateView(c, u)
		}, updateWinW, updateWinH)

		availOpts := uitest.AuditOptions{
			RequiredInteractive: []string{"关闭更新窗口", "跳过", "稍后", "发布页", "立即更新"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, availOpts)
		uitest.AssertThemeParity(t, tt, availOpts)

		u.state = updReady
		u.stagedVersion = "0.1.9"
		tt.Frame()
		uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
			RequiredInteractive: []string{"关闭更新窗口", "下次启动生效", "立即重启应用"},
			RequireFocusable:    true,
		})

		u.state = updUpToDate
		tt.Frame()
		uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
			RequiredInteractive: []string{"关闭更新窗口", "GitHub 发布页", "好"},
			RequireFocusable:    true,
		})

		u.state = updError
		u.errMsg = "网络超时"
		tt.Frame()
		uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
			RequiredInteractive: []string{"关闭更新窗口", "手动下载", "重新检查"},
			RequireFocusable:    true,
		})
	})

	t.Run("countdown", func(t *testing.T) {
		cd := &countdownSession{remaining: 3}
		tt := ui.NewTester(func(c *ui.Context) {
			a.paintCountdownView(c, cd)
		}, 246, 42)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{"取消"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)
	})
}
