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

type ocrWindow struct {
	win         *mygo.Window
	thumb       *ui.Bitmap
	srcImg      *image.NRGBA
	loading     bool
	errMsg      string
	text        string
	links       []string
	copiedBadge string
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
		CornerRadius:    12,
		Panel:           true,
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
	ow.win.Focus()

	go func() {
		res, err := vision.Recognize(img)
		if err != nil {
			log.Printf("Vision OCR error: %v", err)
		}
		links := vision.ExtractLinks(res)
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
				ow.text = txt
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
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if ow.win != nil {
			go mygo.RunOnMain(func() { a.closeOCRWindow() })
		}
		return
	}

	card := ui.Column(c).
		Fill().
		Radius(12).
		Background(ui.RGBA(24, 25, 30, 0.97)).
		Border(1, ui.RGBA(255, 255, 255, 0.13))

	card.Children(func() {
		// 1. 顶部可拖拽标题栏
		header := ui.Row(c).
			Height(46).
			Padding(0, 12).
			Gap(9).
			AlignItems(ui.Center).
			DragWindow()
		header.Children(func() {
			if ow.thumb != nil {
				ui.Box(c).Size(34, 24).Radius(4).Clip().
					Background(ui.RGBA(255, 255, 255, 0.06)).
					Border(1, ui.RGBA(255, 255, 255, 0.14)).
					Children(func() {
						ui.Image(c, ow.thumb).Size(34, 24)
					})
			}
			ui.Icon(c, iconOCR).FontSize(15).TextColor(ui.RGB(64, 172, 255))
			ui.Text(c, "识图提取").FontSize(13).FontWeight(600).NoWrap().TextColor(ui.RGB(244, 245, 248))

			if ow.loading {
				ui.Row(c).Height(20).Padding(0, 8).AlignItems(ui.Center).Radius(10).
					Background(ui.RGBA(64, 172, 255, 0.16)).
					Children(func() {
						ui.Text(c, "正在识别…").FontSize(11).NoWrap().TextColor(ui.RGB(122, 198, 255))
					})
			} else if ow.copiedBadge != "" {
				ui.Row(c).Height(20).Padding(0, 8).Gap(4).AlignItems(ui.Center).Radius(10).
					Background(ui.RGBA(46, 204, 113, 0.16)).
					Border(1, ui.RGBA(46, 204, 113, 0.35)).
					Children(func() {
						ui.Icon(c, iconCheck).FontSize(11).TextColor(ui.RGB(78, 222, 138))
						ui.Text(c, ow.copiedBadge).FontSize(11).NoWrap().TextColor(ui.RGB(118, 236, 166))
					})
			}

			ui.Spacer(c)

			closeBtn := iconSquareBtn(c, iconClose, "关闭 (Esc)", false, false)
			if closeBtn {
				if ow.win != nil {
					go mygo.RunOnMain(func() { a.closeOCRWindow() })
				}
			}
		})

		ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))

		// 2. 二维码与链接快捷栏（检测到二维码或网址时展示）
		if !ow.loading && len(ow.links) > 0 {
			linkWrap := ui.Column(c).
				Padding(8, 12).
				Gap(6).
				Background(ui.RGBA(10, 132, 255, 0.08))
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
						ui.Row(c).Height(18).Padding(0, 6).AlignItems(ui.Center).Radius(4).
							Background(ui.RGBA(10, 132, 255, 0.22)).Shrink(0).
							Children(func() {
								ui.Text(c, tag).FontSize(10.5).NoWrap().TextColor(ui.RGB(122, 198, 255))
							})
						ui.Text(c, lk).FontSize(12).MaxLines(1).Ellipsis("…").Grow(1).
							TextColor(ui.RGBA(235, 238, 245, 0.92))
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
			ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))
		}

		// 3. 正文内容区
		body := ui.Column(c).Grow(1).Padding(10, 12)
		body.Children(func() {
			switch {
			case ow.loading:
				ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "正在离线识别文字与二维码…").FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.72))
					ui.Text(c, "基于 macOS Vision 引擎，全程本地处理不联网").FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.42))
				})
			case ow.errMsg != "":
				ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(6).Children(func() {
					ui.Text(c, "识别遇到问题").FontSize(13.5).FontWeight(600).TextColor(ui.RGB(255, 110, 110))
					ui.Text(c, ow.errMsg).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.65))
				})
			case strings.TrimSpace(ow.text) == "":
				ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "未在所选区域检测到可提取的文字或二维码").FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.72))
					ui.Text(c, "试试框选更清晰的文字区域，或点击下方「重新识图」").FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.42))
				})
			default:
				ui.TextArea(c, &ow.text).
					Label("识图文本").
					Fill().
					Padding(8, 10).
					Radius(8).
					Background(ui.RGBA(14, 15, 19, 0.85)).
					Border(1, ui.RGBA(255, 255, 255, 0.09)).
					FontSize(13).
					LineHeight(1.55).
					TextColor(ui.RGB(238, 240, 245)).
					CaretColor(ui.RGB(64, 172, 255))
			}
		})

		ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))

		// 4. 底部操作栏
		footer := ui.Row(c).
			Height(44).
			Padding(0, 12).
			Gap(8).
			AlignItems(ui.Center)
		footer.Children(func() {
			if actionPillBtn(c, iconOCR, "重新识图", "框选新区域识图 (⌥⌘O)", false, true) {
				go mygo.RunOnMain(func() {
					a.closeOCRWindow()
					if a.session != nil {
						a.session.StartLiveOCR()
					}
				})
			}
			hasText := !ow.loading && strings.TrimSpace(ow.text) != ""
			if hasText {
				if actionPillBtn(c, iconPin, "贴为便签", "将提取的文字钉在桌面上", false, true) {
					txtCopy := ow.text
					go mygo.RunOnMain(func() {
						a.pinExtractedText(txtCopy)
					})
				}
			}
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
	btn := ui.Row(c).
		Label(label).
		Height(22).
		Padding(0, 7).
		Gap(4).
		AlignItems(ui.Center).
		Radius(5).
		Shrink(0).
		Cursor(ui.CursorPointer)
	if btn.Pressed() {
		btn.Scale(0.96, 0.96)
	}
	hov := btn.Hovered()
	switch {
	case primary && hov:
		btn.Background(ui.RGB(28, 152, 255))
	case primary:
		btn.Background(ui.RGB(10, 132, 255))
	case hov:
		btn.Background(ui.RGBA(255, 255, 255, 0.16))
	default:
		btn.Background(ui.RGBA(255, 255, 255, 0.09))
	}
	btn.Children(func() {
		if ic != nil {
			ui.Icon(c, ic).FontSize(11.5).TextColor(ui.RGB(255, 255, 255))
		}
		ui.Text(c, label).FontSize(11).NoWrap().TextColor(ui.RGB(255, 255, 255))
	})
	return btn.Clicked()
}
