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
	ocrWinW = 500
	ocrWinH = 380
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
	showRaw     bool
	links       []string
	copiedBadge string
	enter       panelEnterState
	onCopyText  func(string)
}

func (ow *ocrWindow) copyText(s string) {
	if ow.onCopyText != nil {
		ow.onCopyText(s)
		return
	}
	mygo.Clipboard.WriteText(s)
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
		MinWidth:        400,
		MinHeight:       280,
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
					ow.copiedBadge = fmt.Sprintf("已复制 · %d 字", nChars)
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
			// 1. 顶部可拖拽标题栏（无硬分割线，右侧展示轻量状态反馈胶囊）
			header := ui.Row(c).
				Height(48).
				Padding(0, 18).
				Gap(8).
				AlignItems(ui.Center).
				DragWindow()
			header.Children(func() {
				if macTrafficCloseBtn(c, "关闭识图", "关闭 (⌘W / Esc)") {
					if ow.win != nil {
						go mygo.RunOnMain(func() { a.closeOCRWindow() })
					}
				}
				ui.Box(c).Width(2)
				ui.Icon(c, iconOCR).FontSize(15).TextColor(p.Accent)
				ui.Text(c, "识图提取").FontSize(13.5).FontWeight(600).NoWrap().TextColor(p.Text)

				ui.Spacer(c)

				if ow.loading {
					statusBadgePill(c, "正在识别…", false)
				} else if ow.copiedBadge != "" {
					ui.Row(c.Key("ocr-copied-badge")).
						Height(22).Padding(0, 9).Gap(4).AlignItems(ui.Center).Radius(11).
						Background(p.BadgeSuccessBg).
						Border(1, p.BadgeSuccessBd).
						Children(func() {
							ui.Icon(c, iconCheck).FontSize(11).TextColor(p.BadgeSuccessFg)
							ui.Text(c, ow.copiedBadge).FontSize(11).FontWeight(500).NoWrap().TextColor(p.BadgeSuccessFg)
						})
				}
			})

			// 2. 浮动格式切换栏（检测到 2×2 及以上表格时出现，融入玻璃基底无整条灰带）
			if !ow.loading && ow.hasTable {
				modeRow := ui.Row(c).
					Padding(0, 18, 10, 18).
					Gap(8).
					AlignItems(ui.Center)
				modeRow.Children(func() {
					tabs := [...]struct {
						mode  ocrViewMode
						label string
						badge string
					}{
						{ocrModePlain, "纯文本", "已复制纯文本"},
						{ocrModeMarkdown, "Markdown", "已复制 Markdown"},
						{ocrModeTSV, "表格 TSV", "已复制 TSV"},
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
						ow.copyText(ow.text)
						ow.copiedBadge = tab.badge
					}
					ui.Spacer(c)
					if ow.mode == ocrModeMarkdown || ow.mode == ocrModeTSV {
						if toggled, raw := ocrViewToggle(c, ow.showRaw); toggled {
							ow.showRaw = raw
						}
					}
				})
			}

			// 3. 二维码与链接浮动卡片（检测到二维码或网址时展示）
			if !ow.loading && len(ow.links) > 0 {
				ui.Box(c).Padding(0, 18, 10, 18).Children(func() {
					linkCard := ui.Column(c).
						Radius(12).
						Padding(7, 10).
						Gap(6).
						Background(p.BadgeInfoBg).
						Border(1, p.BadgeInfoBd)
					linkCard.Children(func() {
						for i, lk := range ow.links {
							if i >= 2 {
								break
							}
							lk := lk
							isURL := strings.HasPrefix(lk, "http://") || strings.HasPrefix(lk, "https://")
							row := ui.Row(c.Key(500 + i)).Height(24).Gap(6).AlignItems(ui.Center)
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
									ow.copyText(lk)
									ow.copiedBadge = "已复制链接"
								}
							})
						}
					})
				})
			}

			// 4. 统一透亮正文玻璃卡片（表格可视化视图 / 源码与纯文本无框编辑区）
			bodyWrap := ui.Column(c).Grow(1).Padding(0, 18, 0, 18)
			bodyWrap.Children(func() {
				contentCard := liquidCard(c, ui.Column(c).Fill().Clip())
				contentCard.Children(func() {
					switch {
					case ow.loading:
						paintOCREmptyState(c, iconOCR, p.Accent,
							"正在离线识别文字与二维码…",
							"基于 macOS Vision 引擎，全程本地处理不联网",
							false)
					case ow.errMsg != "":
						paintOCREmptyState(c, iconClose, ui.RGB(235, 68, 60),
							"识别遇到问题",
							ow.errMsg,
							true)
					case strings.TrimSpace(ow.text) == "":
						paintOCREmptyState(c, iconOCR, p.TextMuted,
							"未在所选区域检测到可提取的文字或二维码",
							"试试框选更清晰的文字区域，或点击下方「重新识图」",
							false)
					case ow.hasTable && (ow.mode == ocrModeMarkdown || ow.mode == ocrModeTSV) && !ow.showRaw && len(ow.table.Rows) > 0:
						paintOCRTableView(c, ow)
					default:
						paintOCRTextView(c, ow)
					}
				})
			})

			// 5. 顶部浮动外延式底部操作栏
			footer := ui.Row(c).
				Height(54).
				Padding(0, 18).
				Gap(8).
				AlignItems(ui.Center)
			footer.Children(func() {
				hasText := !ow.loading && strings.TrimSpace(ow.text) != ""
				liquidGlassGroup(c, 13, 8, func(add func(ui.Element) ui.Element) {
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
					ctaLabel, ctaTip, copiedMsg := ocrPrimaryActionCopy(ow)
					if actionPillBtn(c, iconCopy, ctaLabel, ctaTip, true, false) {
						ow.copyText(ow.text)
						ow.copiedBadge = copiedMsg
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

func ocrPrimaryActionCopy(ow *ocrWindow) (label, tip, copied string) {
	if ow.hasTable {
		switch ow.mode {
		case ocrModeMarkdown:
			return "复制 Markdown", "复制 Markdown 格式表格", "已复制 Markdown"
		case ocrModeTSV:
			return "复制表格 TSV", "复制 TSV，可直接 ⌘V 粘贴到 Excel 或飞书表格", "已复制 TSV"
		}
	}
	nChars := utf8.RuneCountInString(strings.TrimSpace(ow.text))
	return "复制全文", "复制文本框中的全部内容", fmt.Sprintf("已复制 · %d 字", nChars)
}

func ocrViewToggle(c *ui.Context, showRaw bool) (toggled, nextRaw bool) {
	p := uiPalette(c)
	trackBg := ui.RGBA(0, 0, 0, 0.055)
	trackBd := ui.RGBA(0, 0, 0, 0.05)
	if p.Dark {
		trackBg = ui.RGBA(255, 255, 255, 0.07)
		trackBd = ui.RGBA(255, 255, 255, 0.07)
	}
	labels := [2]string{"表格", "源码"}
	selected := 0
	if showRaw {
		selected = 1
	}
	track := ui.Row(c.Key("ocr-view-toggle")).
		Height(26).
		Padding(2, 2).
		Gap(2).
		AlignItems(ui.Center).
		Radius(13).
		Background(trackBg).
		Border(1, trackBd).
		Shrink(0)
	track.Children(func() {
		for i, lbl := range labels {
			i, lbl := i, lbl
			active := i == selected
			opt := ui.Box(c.Key(fmt.Sprintf("ocr-vt-%d", i))).
				Label(lbl).
				Height(22).
				Padding(0, 9).
				Radius(11).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Cursor(ui.CursorPointer).
				Shrink(0)
			if active {
				thumbBg := ui.RGBA(255, 255, 255, 0.94)
				thumbBd := ui.RGBA(0, 0, 0, 0.06)
				if p.Dark {
					thumbBg = ui.RGBA(255, 255, 255, 0.19)
					thumbBd = ui.RGBA(255, 255, 255, 0.15)
				}
				opt.Background(thumbBg).Border(1, thumbBd)
			} else if opt.Hovered() {
				hovBg := ui.RGBA(0, 0, 0, 0.04)
				if p.Dark {
					hovBg = ui.RGBA(255, 255, 255, 0.07)
				}
				opt.Background(hovBg)
			}
			opt.Children(func() {
				txtCol := p.TextSecondary
				fw := 500
				if active {
					txtCol = p.Text
					if p.Dark {
						txtCol = ui.RGB(255, 255, 255)
					}
					fw = 600
				} else if opt.Hovered() {
					txtCol = p.Text
				}
				ui.Text(c, lbl).FontSize(11).FontWeight(fw).NoWrap().TextColor(txtCol)
			})
			if opt.Clicked() {
				toggled = true
				nextRaw = i == 1
			}
		}
	})
	return toggled, nextRaw
}

func paintOCREmptyState(c *ui.Context, ic *ui.SVG, accent ui.Color, title, subtitle string, isErr bool) {
	p := uiPalette(c)
	badgeBg := accent.Alpha(0.14)
	if isErr {
		titleCol := accent
		ui.Column(c).Fill().Padding(20, 24).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
			ui.Box(c).Size(38, 38).Radius(19).Background(badgeBg).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(c, ic).FontSize(17).TextColor(accent)
			})
			ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(titleCol)
			ui.Text(c, subtitle).FontSize(12).TextColor(p.TextSecondary)
		})
		return
	}
	ui.Column(c).Fill().Padding(20, 24).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Box(c).Size(38, 38).Radius(19).Background(badgeBg).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Icon(c, ic).FontSize(17).TextColor(accent)
		})
		ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.TextSecondary)
		ui.Text(c, subtitle).FontSize(11.5).TextColor(p.TextMuted)
	})
}

func ocrTableColWeights(tbl vision.Table) []float32 {
	cols := tbl.Cols
	if cols <= 0 && len(tbl.Rows) > 0 {
		cols = len(tbl.Rows[0])
	}
	if cols <= 0 {
		return nil
	}
	weights := make([]float32, cols)
	for j := range cols {
		maxW := 5
		for _, row := range tbl.Rows {
			if j >= len(row) {
				continue
			}
			w := 0
			for _, r := range row[j] {
				if r > 0x7f {
					w += 2
				} else {
					w++
				}
			}
			if w > maxW {
				maxW = w
			}
		}
		if maxW > 26 {
			maxW = 26
		}
		weights[j] = float32(maxW)
	}
	return weights
}

func paintOCRTableCell(c *ui.Context, key, cell string, weight float32, isHeader bool, ow *ocrWindow) {
	p := uiPalette(c)
	hovBg := p.Accent.Alpha(0.10)
	if p.Dark {
		hovBg = p.Accent.Alpha(0.14)
	}
	box := ui.Box(c.Key(key)).
		Grow(weight).
		Basis(0).
		MinWidth(44).
		Padding(9, 12).
		Cursor(ui.CursorPointer).
		Tooltip("点击复制单元格")
	if box.Hovered() {
		box.Background(hovBg)
	}
	box.Children(func() {
		fs := float32(12.5)
		fw := 400
		col := p.Text
		if isHeader {
			fs = 12
			fw = 600
			col = p.TextSecondary
		}
		ui.Text(c, cell).FontSize(fs).FontWeight(fw).LineHeight(1.4).FontFeatures("tnum").TextColor(col)
	})
	if box.Clicked() {
		ow.copyText(cell)
		ow.copiedBadge = "已复制单元格"
	}
}

func paintOCRTableView(c *ui.Context, ow *ocrWindow) {
	p := uiPalette(c)
	weights := ocrTableColWeights(ow.table)
	if len(weights) == 0 || len(ow.table.Rows) == 0 {
		paintOCRTextView(c, ow)
		return
	}

	headerBg := ui.RGBA(0, 0, 0, 0.035)
	zebraBg := ui.RGBA(0, 0, 0, 0.02)
	rowDivider := ui.RGBA(0, 0, 0, 0.045)
	if p.Dark {
		headerBg = ui.RGBA(255, 255, 255, 0.065)
		zebraBg = ui.RGBA(255, 255, 255, 0.025)
		rowDivider = ui.RGBA(255, 255, 255, 0.05)
	}

	// 表头行
	hdrRow := ui.Row(c.Key("ocr-tbl-hdr")).FillWidth().Background(headerBg)
	hdrRow.Children(func() {
		r0 := ow.table.Rows[0]
		for j, w := range weights {
			cell := ""
			if j < len(r0) {
				cell = r0[j]
			}
			paintOCRTableCell(c, fmt.Sprintf("ocr-cell-0-%d", j), cell, w, true, ow)
		}
	})
	panelHairline(c)

	// 可滚动数据行
	scroller := ui.Scroll(c.Key("ocr-tbl-scroll")).Grow(1).FillWidth()
	scroller.Children(func() {
		bodyRows := ow.table.Rows[1:]
		ui.Column(c).FillWidth().Children(func() {
			for i, row := range bodyRows {
				i, row := i, row
				tr := ui.Row(c.Key(fmt.Sprintf("ocr-tr-%d", i+1))).FillWidth()
				if i%2 == 1 {
					tr.Background(zebraBg)
				}
				tr.Children(func() {
					for j, w := range weights {
						cell := ""
						if j < len(row) {
							cell = row[j]
						}
						paintOCRTableCell(c, fmt.Sprintf("ocr-cell-%d-%d", i+1, j), cell, w, false, ow)
					}
				})
				if i < len(bodyRows)-1 {
					ui.Box(c).Height(1).FillWidth().Background(rowDivider)
				}
			}
		})
	})

	// 卡片底部状态提示条
	modeHint := fmt.Sprintf("Markdown · %d 行 × %d 列", len(ow.table.Rows), len(weights))
	if ow.mode == ocrModeTSV {
		modeHint = fmt.Sprintf("TSV · 可直接 ⌘V 粘贴 · %d 行 × %d 列", len(ow.table.Rows), len(weights))
	}
	paintOCRCardFooter(c, "点击单元格可单独复制", modeHint)
}

func ocrTextStats(s string) (lines, chars int) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, 0
	}
	lines = strings.Count(trimmed, "\n") + 1
	chars = utf8.RuneCountInString(trimmed)
	return lines, chars
}

func paintOCRCardFooter(c *ui.Context, leftHint, rightStat string) {
	p := uiPalette(c)
	panelHairline(c)
	ui.Row(c).Height(28).Padding(0, 14).AlignItems(ui.Center).Children(func() {
		ui.Text(c, leftHint).FontSize(10.5).NoWrap().TextColor(p.TextMuted)
		ui.Spacer(c)
		ui.Text(c, rightStat).FontSize(10.5).FontFeatures("tnum").NoWrap().TextColor(p.TextMuted)
	})
}

func paintOCRTextView(c *ui.Context, ow *ocrWindow) {
	p := uiPalette(c)
	taKey := fmt.Sprintf("ocr-ta-%d-%v", ow.mode, ow.showRaw)
	ta := ui.TextAreaBase(c.Key(taKey), &ow.text).
		Label("识图文本").
		Grow(1).
		FillWidth().
		Padding(14, 16).
		Background(ui.Transparent).
		TextColor(p.Text).
		CaretColor(p.Accent)

	hint := "可直接编辑或局部选中复制"
	if ow.hasTable && ow.showRaw && ow.mode != ocrModePlain {
		ta.Font("SF Mono, Menlo, monospace").FontSize(12.5).LineHeight(1.6)
		if ow.mode == ocrModeMarkdown {
			ta.TabSize(4)
			hint = "编辑 Markdown 源码"
		} else {
			ta.TabSize(8)
			hint = "编辑 TSV 源码"
		}
	} else {
		ta.FontSize(13.5).LineHeight(1.65)
	}

	lines, chars := ocrTextStats(ow.text)
	paintOCRCardFooter(c, hint, fmt.Sprintf("%d 行 · %d 字", lines, chars))
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
