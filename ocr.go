package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/beautify"
	"snipgo/internal/vision"
)

const (
	ocrWinW = 460
	ocrWinH = 360
)

type ocrViewMode int

const (
	ocrModePlain ocrViewMode = iota
	ocrModeMarkdown
	ocrModeTSV
)

type ocrWindow struct {
	win         *mygo.Window
	thumb       *ui.Bitmap
	srcImg      *image.NRGBA
	loading     bool
	errMsg      string
	text        string
	plainText   string
	table       vision.Table
	hasTable    bool
	mode        ocrViewMode
	links       []string
	copiedBadge string
	enter       panelEnterState
}

// RecognizeImage 执行离线 Vision 识字与二维码识别，并立即弹出可视化「识图结果面板」。
func (a *app) RecognizeImage(img *image.NRGBA) {
	var anchor image.Rectangle
	if a.session != nil {
		anchor = a.session.LastOCRRect()
	}
	a.RecognizeImageAt(img, anchor)
}

// RecognizeImageAt 在指定屏幕区域旁弹出悬浮识图结果面板，自动复制识别内容并支持编辑、局部复制、打开链接或转为便签贴图。
func (a *app) RecognizeImageAt(img *image.NRGBA, anchor image.Rectangle) {
	if img == nil || img.Bounds().Empty() {
		return
	}
	mygo.RunOnMain(func() {
		a.openOCRWindow(img, anchor)
	})
}

func (a *app) closeOCRWindow() {
	if a.ocr != nil {
		old := a.ocr
		a.ocr = nil
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
	}
}

func (a *app) openOCRWindow(img *image.NRGBA, anchor image.Rectangle) {
	a.closeOCRWindow()

	pos := a.placeOCRWindow(anchor)
	ow := &ocrWindow{
		thumb:   ui.NewBitmap(img),
		srcImg:  img,
		loading: true,
		enter:   panelEnterState{enabled: true},
	}
	a.ocr = ow

	ow.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "SnipGo 识图",
		Width:           ocrWinW,
		Height:          ocrWinH,
		X:               pos.X,
		Y:               pos.Y,
		Frameless:       true,
		Transparent:     true,
		Vibrancy:        vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius:    panelCornerRadius,
		Panel:           false,
		AlwaysOnTop:     true,
		DisableMinimize: true,
		DisableMaximize: true,
		MinWidth:        360,
		MinHeight:       260,
		Content: ui.View(func(c *ui.Context) {
			a.paintOCRView(c, ow)
		}),
	})
	ow.win.OnClosed(func() {
		if a.ocr == ow {
			a.ocr = nil
		}
	})
	applyWindowTheme(ow.win, themeStateFromConfig(a.cfg))
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	ow.win.Focus()

	go func() {
		res, err := vision.Recognize(img)
		if err != nil {
			log.Printf("Vision OCR error: %v", err)
		}
		links := vision.ExtractLinks(res)
		tbl, hasTbl := vision.ExtractTable(res.Boxes)
		txt := strings.TrimSpace(res.Text)
		if txt == "" && len(res.Barcodes) > 0 {
			txt = strings.Join(res.Barcodes, "\n")
		}
		mygo.RunOnMain(func() {
			if a.ocr != ow {
				return
			}
			ow.loading = false
			if err != nil {
				ow.errMsg = err.Error()
			} else {
				ow.plainText = txt
				ow.text = txt
				ow.table = tbl
				ow.hasTable = hasTbl
				ow.mode = ocrModePlain
				ow.links = links
				if txt != "" {
					mygo.Clipboard.WriteText(txt)
					nChars := utf8.RuneCountInString(txt)
					ow.copiedBadge = fmt.Sprintf("已自动复制 · %d 字", nChars)
					a.sessionStatus(fmt.Sprintf("识图已复制（%d 字）", nChars))
				}
			}
			if ow.win != nil && !ow.win.IsDestroyed() {
				ow.win.Invalidate()
			}
		})
	}()
}

func (a *app) placeOCRWindow(anchor image.Rectangle) image.Point {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		return image.Pt(120, 120)
	}
	cur := mygo.Screen.CursorScreenPoint()
	refPt := image.Pt(cur.X, cur.Y)
	if !anchor.Empty() {
		refPt = image.Pt((anchor.Min.X+anchor.Max.X)/2, (anchor.Min.Y+anchor.Max.Y)/2)
	}
	screen := image.Rect(
		displays[0].WorkArea.X,
		displays[0].WorkArea.Y,
		displays[0].WorkArea.X+displays[0].WorkArea.Width,
		displays[0].WorkArea.Y+displays[0].WorkArea.Height,
	)
	for _, d := range displays {
		wa := image.Rect(d.WorkArea.X, d.WorkArea.Y, d.WorkArea.X+d.WorkArea.Width, d.WorkArea.Y+d.WorkArea.Height)
		if refPt.In(wa) {
			screen = wa
			break
		}
	}
	x := screen.Min.X + (screen.Dx()-ocrWinW)/2
	y := screen.Min.Y + (screen.Dy()-ocrWinH)/2
	if !anchor.Empty() {
		// 优先放在选区右侧；右侧放不下放左侧；都放不下则居中对齐在选区下方/内部。
		switch {
		case anchor.Max.X+14+ocrWinW <= screen.Max.X:
			x = anchor.Max.X + 14
			y = anchor.Min.Y
		case anchor.Min.X-14-ocrWinW >= screen.Min.X:
			x = anchor.Min.X - 14 - ocrWinW
			y = anchor.Min.Y
		default:
			x = anchor.Min.X + (anchor.Dx()-ocrWinW)/2
			y = anchor.Max.Y + 12
		}
	}
	x = max(screen.Min.X+12, min(x, screen.Max.X-ocrWinW-12))
	y = max(screen.Min.Y+12, min(y, screen.Max.Y-ocrWinH-12))
	return image.Pt(x, y)
}

func (a *app) paintOCRView(c *ui.Context, ow *ocrWindow) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.closeOCRWindow() }) {
			return
		}

		p := uiPalette(c)
		card := liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &ow.enter)

		card.Children(func() {
			// 1. 顶部可拖拽标题栏
			header := ui.Row(c).
				Height(50).
				Padding(0, 16).
				Gap(9).
				AlignItems(ui.Center).
				DragWindow()
			header.Children(func() {
				if macTrafficCloseBtn(c, "关闭识图", "关闭 (⌘W / Esc)") {
					if ow.win != nil {
						go mygo.RunOnMain(func() { a.closeOCRWindow() })
					}
				}
				ui.Box(c).Width(2)
				if ow.thumb != nil {
					ui.Box(c).Size(34, 24).Radius(6).Clip().
						Background(p.StageBg).
						Border(1, p.CardBorder).
						Children(func() {
							ui.Image(c, ow.thumb).Size(34, 24)
						})
				}
				ui.Icon(c, iconOCR).FontSize(15).TextColor(p.Accent)
				ui.Text(c, "识图提取").FontSize(13.5).FontWeight(600).NoWrap().TextColor(p.Text)

				if ow.loading {
					statusBadgePill(c, "正在识别…", false)
				} else if ow.copiedBadge != "" {
					ui.Row(c).Height(22).Padding(0, 9).Gap(4).AlignItems(ui.Center).Radius(11).
						Background(p.BadgeSuccessBg).
						Border(1, p.BadgeSuccessBd).
						InsetShadow(0, 1, 0, 0, p.CardTopRim).
						Children(func() {
							ui.Icon(c, iconCheck).FontSize(11).TextColor(p.BadgeSuccessFg)
							ui.Text(c, ow.copiedBadge).FontSize(11).FontWeight(500).NoWrap().TextColor(p.BadgeSuccessFg)
						})
				}

				ui.Spacer(c)
			})

			panelHairline(c)

			// 1.5 结构化表格格式切换栏（检测到 2×2 及以上表格时自动出现）
			if !ow.loading && ow.hasTable {
				modeBar := ui.Row(c).
					Height(40).
					Padding(0, 16).
					Gap(8).
					AlignItems(ui.Center).
					Background(p.CardBg)
				modeBar.Children(func() {
					ui.Icon(c, iconTable).FontSize(13).TextColor(p.Accent)
					tabs := [...]struct {
						mode  ocrViewMode
						label string
						badge string
					}{
						{ocrModePlain, "纯文本", "已切换并复制纯文本"},
						{ocrModeMarkdown, "Markdown 表格", fmt.Sprintf("已复制 Markdown 表格 (%d×%d)", len(ow.table.Rows), ow.table.Cols)},
						{ocrModeTSV, "Excel / 飞书 TSV", "已复制 TSV · 可直接 ⌘V 粘贴成表"},
					}
					if picked := liquidSegmented(c, "ocr-mode", int(ow.mode), []string{tabs[0].label, tabs[1].label, tabs[2].label}); picked >= 0 {
						tab := tabs[picked]
						ow.mode = tab.mode
						switch tab.mode {
						case ocrModeMarkdown:
							ow.text = ow.table.Markdown()
						case ocrModeTSV:
							ow.text = ow.table.TSV()
						default:
							ow.text = ow.plainText
						}
						mygo.Clipboard.WriteText(ow.text)
						ow.copiedBadge = tab.badge
					}
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("%d行×%d列", len(ow.table.Rows), ow.table.Cols)).
						FontSize(10.5).FontFeatures("tnum").NoWrap().
						TextColor(p.TextMuted)
				})
				panelHairline(c)
			}

			// 2. 二维码与链接快捷栏（检测到二维码或网址时展示）
			if !ow.loading && len(ow.links) > 0 {
				linkWrap := ui.Column(c).
					Padding(8, 16).
					Gap(6).
					Background(p.BadgeInfoBg)
				linkWrap.Children(func() {
					for i, lk := range ow.links {
						if i >= 2 {
							break
						}
						lk := lk
						isURL := strings.HasPrefix(lk, "http://") || strings.HasPrefix(lk, "https://")
						row := ui.Row(c.Key(500 + i)).Height(26).Gap(6).AlignItems(ui.Center)
						row.Children(func() {
							tag := "二维码"
							if isURL {
								tag = "链接"
							}
							ui.Row(c).Height(18).Padding(0, 7).AlignItems(ui.Center).Radius(9).
								Background(p.BadgeInfoBg).
								Border(1, p.BadgeInfoBd).
								Shrink(0).
								Children(func() {
									ui.Text(c, tag).FontSize(10.5).FontWeight(500).NoWrap().TextColor(p.BadgeInfoFg)
								})
							ui.Text(c, lk).FontSize(12).MaxLines(1).Ellipsis("…").Grow(1).
								TextColor(p.Text)
							if isURL {
								if miniPillBtn(c, iconExternal, "打开", true) {
									go func() { _ = mygo.Shell.OpenExternal(lk) }()
								}
							}
							if miniPillBtn(c, iconCopy, "复制", false) {
								mygo.Clipboard.WriteText(lk)
								ow.copiedBadge = "已复制链接"
							}
						})
					}
				})
				panelHairline(c)
			}

			// 3. 正文内容区
			body := ui.Column(c).Grow(1).Padding(12, 16)
			body.Children(func() {
				switch {
				case ow.loading:
					ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "正在离线识别文字与二维码…").FontSize(13).TextColor(p.TextSecondary)
						ui.Text(c, "基于 macOS Vision 引擎，全程本地处理不联网").FontSize(11.5).TextColor(p.TextMuted)
					})
				case ow.errMsg != "":
					ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(6).Children(func() {
						ui.Text(c, "识别遇到问题").FontSize(13.5).FontWeight(600).TextColor(ui.RGB(235, 68, 60))
						ui.Text(c, ow.errMsg).FontSize(12).TextColor(p.TextSecondary)
					})
				case strings.TrimSpace(ow.text) == "":
					ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "未在所选区域检测到可提取的文字或二维码").FontSize(13).TextColor(p.TextSecondary)
						ui.Text(c, "试试框选更清晰的文字区域，或点击下方「重新识图」").FontSize(11.5).TextColor(p.TextMuted)
					})
				default:
					recessedInputStyle(c, ui.TextArea(c, &ow.text).
						Label("识图文本").
						Fill().
						Padding(10, 12).
						FontSize(13).
						LineHeight(1.55))
				}
			})

			panelHairline(c)

			// 4. 底部操作栏
			footer := ui.Row(c).
				Height(52).
				Padding(0, 16).
				Gap(8).
				AlignItems(ui.Center)
			footer.Children(func() {
				hasText := !ow.loading && strings.TrimSpace(ow.text) != ""
				liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
					if glassGroupBtn(c, add, iconOCR, "重新识图", "框选新区域识图 (⌥⌘O)") {
						go mygo.RunOnMain(func() {
							a.closeOCRWindow()
							if a.session != nil {
								a.session.StartLiveOCR()
							}
						})
					}
					if hasText {
						if glassGroupBtn(c, add, iconPin, "贴为便签", "将提取的文字钉在桌面上") {
							txtCopy := ow.text
							go mygo.RunOnMain(func() {
								a.pinExtractedText(txtCopy)
							})
						}
					}
				})
				ui.Spacer(c)
				if hasText {
					if actionPillBtn(c, iconCopy, "复制全文", "复制文本框中的全部内容", true, false) {
						mygo.Clipboard.WriteText(ow.text)
						nChars := utf8.RuneCountInString(strings.TrimSpace(ow.text))
						ow.copiedBadge = fmt.Sprintf("已复制全文 · %d 字", nChars)
					}
				}
			})
		})
	})
}

func (a *app) pinExtractedText(txt string) {
	txt = strings.TrimSpace(txt)
	if txt == "" {
		return
	}
	card := beautify.RenderTextPin(txt, 2.0)
	if card == nil || card.Bounds().Empty() {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, card); err != nil {
		return
	}
	dipW := max(120, card.Bounds().Dx()/2)
	dipH := max(60, card.Bounds().Dy()/2)
	cur := mygo.Screen.CursorScreenPoint()
	at := image.Pt(max(20, cur.X-dipW/2), max(40, cur.Y-dipH/2))
	a.ShowPin(card, buf.Bytes(), at, dipW, dipH)
}

func miniPillBtn(c *ui.Context, ic *ui.SVG, label string, primary bool) bool {
	p := uiPalette(c)
	btn := ui.Row(c).
		Label(label).
		Height(24).
		Padding(0, 10).
		Gap(4).
		AlignItems(ui.Center).
		Radius(12).
		Shrink(0).
		Cursor(ui.CursorPointer)
	targetScale := float32(1.0)
	if btn.Pressed() {
		targetScale = 0.94
	}
	if sc := btn.AnimateTo("mini-sc", targetScale, springPop); sc != 1.0 {
		btn.Scale(sc, sc)
	}
	hov := btn.Hovered()
	if primary {
		liquidPrimaryChrome(c, btn, 12)
	} else {
		liquidSecondaryChrome(c, btn, hov, 12)
	}
	btn.Children(func() {
		col := p.Text
		if primary {
			col = p.AccentText
		}
		if ic != nil {
			ui.Icon(c, ic).FontSize(11.5).TextColor(col)
		}
		ui.Text(c, label).FontSize(11).FontWeight(500).NoWrap().TextColor(col)
	})
	return btn.Clicked()
}

func compactTabBtn(c *ui.Context, label string, active bool) bool {
	p := uiPalette(c)
	btn := ui.Row(c).
		Label(label).
		Height(24).
		Padding(0, 10).
		AlignItems(ui.Center).
		Radius(12).
		Shrink(0).
		Cursor(ui.CursorPointer)
	targetScale := float32(1.0)
	if btn.Pressed() {
		targetScale = 0.94
	}
	if sc := btn.AnimateTo("tab-sc", targetScale, springPop); sc != 1.0 {
		btn.Scale(sc, sc)
	}
	switch {
	case active:
		liquidActiveChipChrome(c, btn, 12)
	default:
		liquidSecondaryChrome(c, btn, btn.Hovered(), 12)
	}
	btn.Children(func() {
		col := p.TextSecondary
		if active {
			col = p.ActiveText
		}
		ui.Text(c, label).FontSize(11).FontWeight(500).NoWrap().TextColor(col)
	})
	return btn.Clicked()
}
