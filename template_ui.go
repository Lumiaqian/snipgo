package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/beautify"
)

const (
	templateWinW = 940
	templateWinH = 664
)

type templateWindow struct {
	win           *mygo.Window
	srcImg        *image.NRGBA
	srcScale      float64
	previewSrc    *image.NRGBA
	previewScale  float64
	opts          beautify.TemplateOptions
	renderedOpts  beautify.TemplateOptions
	renderedValid bool
	previewImg    *image.NRGBA
	previewBmp    *ui.Bitmap
	fullCacheOpts beautify.TemplateOptions
	fullCacheImg  *image.NRGBA
	fullCachePNG  []byte
	exporting     bool // 整图导出（渲染 + PNG 编码）正在后台进行
	statusBadge   string
	badge         badgeState
	enter         panelEnterState
}

func (tw *templateWindow) setStatusBadge(msg string) {
	var fallback *string
	if tw.win == nil {
		fallback = &tw.statusBadge
	}
	tw.badge.Set(msg, tw.win, fallback)
}

type tplChoiceMatch struct{}

func (a *app) openTemplateWindow(img *image.NRGBA, anchor image.Rectangle) {
	if img == nil || img.Bounds().Empty() {
		return
	}
	a.closeTemplateWindow()

	opts := a.lastTplOpts
	if !a.hasTplOpts {
		opts = beautify.DefaultTemplateOptions()
	}

	scale := 2.0
	if !anchor.Empty() && anchor.Dx() > 0 {
		scale = max(1.0, min(3.0, float64(img.Bounds().Dx())/float64(anchor.Dx())))
	}
	prevSrc, factor := downscaleForPreview(img, 620)
	prevScale := max(0.85, scale*factor)

	tw := &templateWindow{
		srcImg:       img,
		srcScale:     scale,
		previewSrc:   prevSrc,
		previewScale: prevScale,
		opts:         opts,
		enter:        panelEnterState{enabled: true},
	}
	tw.refreshPreview()
	a.tplWin = tw

	pos := a.placeCenteredPanel(templateWinW, templateWinH)
	tw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "SnipGo 套模板",
		Width:           templateWinW,
		Height:          templateWinH,
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
		MinWidth:        800,
		MinHeight:       540,
		Content: ui.View(func(c *ui.Context) {
			a.paintTemplateView(c, tw)
		}),
	})
	tw.win.OnClosed(func() {
		if a.tplWin == tw {
			a.tplWin = nil
		}
	})
	applyWindowTheme(tw.win, themeStateFromConfig(a.cfg))
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	tw.win.Focus()
}

func (a *app) closeTemplateWindow() {
	if a.tplWin != nil {
		old := a.tplWin
		a.tplWin = nil
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
	}
}

func (tw *templateWindow) refreshPreview() {
	if tw.previewSrc == nil {
		return
	}
	tw.previewImg = beautify.RenderTemplate(tw.previewSrc, tw.previewScale, tw.opts)
	if tw.previewImg != nil {
		tw.previewBmp = ui.NewBitmap(tw.previewImg)
	}
	tw.renderedOpts = tw.opts
	tw.renderedValid = true
}

// renderTemplateExport 渲染模板整图并编码 PNG（耗时，勿在主线程调用）。
func renderTemplateExport(src *image.NRGBA, scale float64, opts beautify.TemplateOptions) (*image.NRGBA, []byte) {
	out := beautify.RenderTemplate(src, scale, opts)
	if out == nil || out.Bounds().Empty() {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, nil
	}
	return out, buf.Bytes()
}

// ensureFullExport 同步取得整图 PNG，仅用于必须立即返回数据的拖出场景（拖拽数据回调无法异步）。
func (tw *templateWindow) ensureFullExport() []byte {
	if tw.fullCacheImg != nil && len(tw.fullCachePNG) > 0 && tw.fullCacheOpts == tw.opts {
		return tw.fullCachePNG
	}
	out, pngBytes := renderTemplateExport(tw.srcImg, tw.srcScale, tw.opts)
	if len(pngBytes) == 0 {
		return nil
	}
	tw.fullCacheImg, tw.fullCachePNG, tw.fullCacheOpts = out, pngBytes, tw.opts
	return pngBytes
}

// withFullExport 取得整图与 PNG 后回调 done（主线程）。缓存命中时同步回调；
// 否则在后台渲染并编码，期间显示“正在导出…”并忽略重复触发。
// 导出完成时窗口已关闭或参数已变化则丢弃本次动作（结果仍写入对应参数的缓存）。
func (a *app) withFullExport(tw *templateWindow, done func(out *image.NRGBA, pngBytes []byte)) {
	if tw.fullCacheImg != nil && len(tw.fullCachePNG) > 0 && tw.fullCacheOpts == tw.opts {
		done(tw.fullCacheImg, tw.fullCachePNG)
		return
	}
	if tw.exporting {
		return
	}
	tw.exporting = true
	if tw.win != nil && !tw.win.IsDestroyed() {
		tw.win.Invalidate()
	}
	src, scale, opts := tw.srcImg, tw.srcScale, tw.opts
	runBackground(func() {
		out, pngBytes := renderTemplateExport(src, scale, opts)
		runOnMain(func() {
			tw.exporting = false
			if tw.win != nil && !tw.win.IsDestroyed() {
				tw.win.Invalidate()
			}
			if len(pngBytes) == 0 {
				return
			}
			tw.fullCacheImg, tw.fullCachePNG, tw.fullCacheOpts = out, pngBytes, opts
			if a.tplWin != tw || tw.opts != opts {
				return
			}
			done(out, pngBytes)
		})
	})
}

func (a *app) copyTemplateResult(tw *templateWindow) {
	a.lastTplOpts = tw.opts
	a.hasTplOpts = true
	a.withFullExport(tw, func(out *image.NRGBA, pngBytes []byte) {
		_ = mygo.Clipboard.WriteImage(pngBytes)
		a.recordHistory(pngBytes, out.Bounds().Dx(), out.Bounds().Dy(), tw.srcScale)
		tw.setStatusBadge(fmt.Sprintf("✓ 已复制 · %d×%d px", out.Bounds().Dx(), out.Bounds().Dy()))
		a.sessionStatus("模板成图已复制到剪贴板")
		if tw.win != nil && !tw.win.IsDestroyed() {
			tw.win.Invalidate()
		}
	})
}

func (a *app) saveTemplateResult(tw *templateWindow) {
	a.lastTplOpts = tw.opts
	a.hasTplOpts = true
	a.withFullExport(tw, func(out *image.NRGBA, pngBytes []byte) {
		a.recordHistory(pngBytes, out.Bounds().Dx(), out.Bounds().Dy(), tw.srcScale)
		a.SavePNG(pngBytes)
		tw.setStatusBadge("✓ 已触发保存")
		if tw.win != nil && !tw.win.IsDestroyed() {
			tw.win.Invalidate()
		}
	})
}

func (a *app) pinTemplateResult(tw *templateWindow) {
	a.lastTplOpts = tw.opts
	a.hasTplOpts = true
	a.withFullExport(tw, func(out *image.NRGBA, pngBytes []byte) {
		a.recordHistory(pngBytes, out.Bounds().Dx(), out.Bounds().Dy(), tw.srcScale)
		sc := max(1.0, tw.srcScale)
		dipW := max(100, int(math.Round(float64(out.Bounds().Dx())/sc)))
		dipH := max(80, int(math.Round(float64(out.Bounds().Dy())/sc)))
		at := pinPlacementNearCursorOnScreen(dipW, dipH)
		a.ShowPin(out, pngBytes, at, dipW, dipH)
		tw.setStatusBadge("✓ 已钉为悬浮贴图")
		if tw.win != nil && !tw.win.IsDestroyed() {
			tw.win.Invalidate()
		}
	})
}

func (a *app) paintTemplateView(c *ui.Context, tw *templateWindow) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.closeTemplateWindow() }) {
			return
		}

		typing := c.FocusTakesText()
		cmdC := c.Shortcut(ui.Cmd, ui.KeyC)
		cmdS := c.Shortcut(ui.Cmd, ui.KeyS)
		cmdT := c.Shortcut(ui.Cmd, ui.KeyT)
		cmdEnter := c.Shortcut(ui.Cmd, ui.KeyEnter)
		plainEnter := !typing && c.Shortcut(0, ui.KeyEnter)
		plainT := !typing && c.Shortcut(0, ui.KeyT)

		if (!typing && cmdC) || cmdEnter || plainEnter {
			a.copyTemplateResult(tw)
		} else if cmdS {
			a.saveTemplateResult(tw)
		} else if cmdT || plainT {
			a.pinTemplateResult(tw)
		}

		if !tw.renderedValid || tw.opts != tw.renderedOpts {
			tw.refreshPreview()
			a.lastTplOpts = tw.opts
			a.hasTplOpts = true
		}

		winW, winH := templateWinW, templateWinH
		if tw.win != nil && !tw.win.IsDestroyed() {
			b := tw.win.Bounds()
			if b.Width > 0 && b.Height > 0 {
				winW, winH = b.Width, b.Height
			}
		}

		p := uiPalette(c)
		root := liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &tw.enter)

		root.Children(func() {
			// 1. 顶部无框拖拽标题栏（右侧轻量状态徽标）
			paintPanelHeader(c, iconTemplate, "SnipGo 套模板", "关闭模板工作台", "关闭 (⌘W / Esc)", func() {
				if tw.win != nil {
					a.closeTemplateWindow()
				}
			}, func() {
				badge := tw.badge.Text(tw.statusBadge)
				if tw.exporting {
					badge = "正在导出…"
				}
				statusBadgePill(c, badge, true)
			})

			// 2. 主体左右分栏：左侧独立圆角透明预览画布 + 右侧参数侧边栏
			mainRow := ui.Row(c).Grow(1).Padding(0, 18, 0, 18).Gap(14).AlignItems(ui.Stretch)
			mainRow.Children(func() {
				const sideW = 312
				stageW := max(260, winW-sideW-68)
				stageH := max(220, winH-48-54-36)

				stage := ui.Box(c).
					Grow(1).
					FillHeight().
					Radius(cardCornerRadius).
					Clip().
					Background(p.StageBg).
					Border(1, p.CardBorder).
					InsetShadow(0, 1, 0, 0, p.CardTopRim).
					Padding(16, 16).
					AlignItems(ui.Center).
					Justify(ui.Center)
				stage.Children(func() {
					ui.Box(c).Absolute().Left(0).Top(0).Fill().IgnoreInput(true).Draw(paintStudioDotBackdrop)

					if tw.previewBmp != nil && tw.previewImg != nil {
						pb := tw.previewImg.Bounds()
						fitW, fitH := fitBox(pb.Dx(), pb.Dy(), float32(stageW), float32(stageH))
						ui.Image(c, tw.previewBmp).
							Size(fitW, fitH).
							Fit(ui.Contain).
							Radius(6).
							Shadow(0, 14, 34, 0, ui.RGBA(0, 0, 0, 0.55)).
							Shadow(0, 3, 8, 0, ui.RGBA(0, 0, 0, 0.32))
					}

					meta := beautify.TemplateMetaByKind(tw.opts.Kind)
					themes := beautify.TemplateThemes(tw.opts.Kind)
					themeName := ""
					if len(themes) > 0 {
						themeName = themes[tw.opts.Theme%len(themes)].Name
					}
					estInfo := meta.Name
					if themeName != "" {
						estInfo += " · " + themeName
					}
					if tw.srcImg != nil && tw.previewImg != nil && tw.previewSrc != nil && tw.previewSrc.Bounds().Dx() > 0 {
						ratio := float64(tw.srcImg.Bounds().Dx()) / float64(tw.previewSrc.Bounds().Dx())
						estW := int(math.Round(float64(tw.previewImg.Bounds().Dx()) * ratio))
						estH := int(math.Round(float64(tw.previewImg.Bounds().Dy()) * ratio))
						estInfo += fmt.Sprintf(" · 约 %d×%d px", estW, estH)
					}
					liquidGlassCapsule(
						c,
						ui.Row(c).Absolute().Left(12).Bottom(12).Height(26).Padding(0, 11).
							AlignItems(ui.Center).IgnoreInput(true),
						13,
						false,
					).Children(func() {
						ui.Text(c, estInfo).FontSize(11).FontWeight(500).FontFeatures("tnum").NoWrap().TextColor(p.Text)
					})
				})

				// 右侧：参数侧边栏（带上下渐进滚动边缘模糊）
				sideWrap := ui.Box(c).Width(sideW).FillHeight().Clip()
				sideWrap.Children(func() {
					sidebar := ui.Scroll(c).Fill().Padding(2, 2, 8, 2).ScrollbarInsets(2, 0, 6)
					sidebar.Children(func() {
						col := ui.Column(c).Gap(6)
						col.Children(func() {
							// A. 模板网格（2列×5行，全部 10 套模板一屏直达）
							sectionTitle(c, "模板样式")
							metas := beautify.AllTemplates()
							grid := ui.Column(c).Gap(4)
							grid.Children(func() {
								for r := 0; r < len(metas); r += 2 {
									r := r
									row := ui.Row(c.Key(4000 + r)).Gap(5)
									row.Children(func() {
										a.paintTemplateChoiceCard(c.Key(4100+r), tw, metas[r])
										if r+1 < len(metas) {
											a.paintTemplateChoiceCard(c.Key(4100+r+1), tw, metas[r+1])
										} else {
											ui.Box(c).Grow(1)
										}
									})
								}
							})

							// B. 纸张 / 风格配色
							themes := beautify.TemplateThemes(tw.opts.Kind)
							if len(themes) > 0 {
								curIdx := ((tw.opts.Theme % len(themes)) + len(themes)) % len(themes)
								themeRow := ui.Row(c).AlignItems(ui.Center).Gap(8)
								themeRow.Children(func() {
									sectionTitle(c, "纸色风格")
									ui.Spacer(c)
									ui.Text(c, themes[curIdx].Name).FontSize(11).FontWeight(500).TextColor(p.ActiveText)
								})
								dots := ui.Row(c).Gap(8).AlignItems(ui.Center)
								dots.Children(func() {
									for i, th := range themes {
										i, th := i, th
										active := i == curIdx
										btn := ui.Row(c.Key(4300+i)).
											Label(th.Name).
											Tooltip(th.Name).
											Size(28, 20).
											Radius(10).
											Padding(2, 2).
											AlignItems(ui.Center).
											Justify(ui.Center).
											Background(ui.RGB(th.Preview.R, th.Preview.G, th.Preview.B)).
											Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.24)).
											Cursor(ui.CursorPointer)
										targetScale := float32(1.0)
										if btn.Pressed() {
											targetScale = 0.94
										} else if active || btn.Hovered() {
											targetScale = 1.07
										}
										if sc := btn.AnimateTo("tpl-sw-sc", targetScale, springPop); sc != 1.0 {
											btn.Scale(sc, sc)
										}
										bdTarget := p.SubtleBorder
										if active {
											bdTarget = p.Accent
										} else if btn.Hovered() {
											bdTarget = p.ActiveBorder
										}
										btn.Border(2, btn.AnimateColor("tpl-sw-bd", bdTarget, colorMorph))
										if active {
											btn.InsetShadow(0, 0, 0, 1.5, ui.RGB(255, 255, 255))
										}
										btn.Children(func() {
											ui.Box(c).Size(6.5, 6.5).Radius(3.25).Background(ui.RGB(th.Accent.R, th.Accent.G, th.Accent.B))
										})
										if btn.Clicked() {
											tw.opts.Theme = i
										}
									}
								})
							}

							// C. 文案编辑（标题 / 配文）
							meta := beautify.TemplateMetaByKind(tw.opts.Kind)
							sectionTitle(c, "卡片文字")
							txtCard := liquidCard(c, ui.Column(c).Padding(8, 11).Gap(5))
							txtCard.Children(func() {
								ui.Column(c).Gap(2).Children(func() {
									ui.Text(c, "标题").FontSize(10.5).FontWeight(500).TextColor(p.TextMuted)
									baseTextInput(c.Key("tpl-title"), &tw.opts.Title).
										Label("卡片标题").
										Height(27).
										Padding(0, 10).
										FontSize(12).
										Placeholder(meta.TitlePlaceholder)
								})

								ui.Column(c).Gap(2).Children(func() {
									ui.Text(c, "配文").FontSize(10.5).FontWeight(500).TextColor(p.TextMuted)
									baseTextInput(c.Key("tpl-caption"), &tw.opts.Caption).
										Label("卡片配文").
										Height(27).
										Padding(0, 10).
										FontSize(12).
										Placeholder(meta.CaptionPlaceholder)
								})

								if strings.TrimSpace(tw.opts.Title) != "" || strings.TrimSpace(tw.opts.Caption) != "" {
									clearRow := ui.Row(c).Justify(ui.End)
									clearRow.Children(func() {
										if miniPillBtn(c, nil, "清空文字", false) {
											tw.opts.Title = ""
											tw.opts.Caption = ""
										}
									})
								}
							})

							// D. 版式与日期选项
							sectionTitle(c, "版式选项")
							optCard := liquidCard(c, ui.Column(c).Padding(8, 11).Gap(7))
							optCard.Children(func() {
								liquidSwitch(c, "tpl-date", &tw.opts.ShowDate, "显示日期水印")
								padSel := 1
								switch tw.opts.Padding {
								case beautify.PadCompact:
									padSel = 0
								case beautify.PadWide:
									padSel = 2
								}
								padVals := []beautify.TemplatePadding{beautify.PadCompact, beautify.PadNormal, beautify.PadWide}
								ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
									ui.Text(c, "边距").FontSize(12).TextColor(p.TextSecondary)
									ui.Spacer(c)
									if picked := liquidSegmented(c, "tpl-pad", padSel, []string{"紧凑", "标准", "宽幅"}); picked >= 0 {
										tw.opts.Padding = padVals[picked]
									}
								})
							})
						})
					})
				})
			})

			// 3. 底部浮动导出动作栏（支持直接拖出 PNG）
			footer := ui.Row(c).Height(54).Padding(0, 18).Gap(8).AlignItems(ui.Center).ZIndex(2)
			footer.Children(func() {
				dragBtn := ui.Row(c).
					Label("拖出PNG").
					Tooltip("按住直接将套好模板的高清 PNG 拖入微信 / Slack / 浏览器 / Finder").
					Height(28).
					Padding(0, 11).
					Gap(5).
					AlignItems(ui.Center).
					Cursor(ui.CursorGrab)
				dragChip := liquidSecondaryChrome(c, dragBtn, dragBtn.Hovered(), 14)
				dragChip.DragDataFrom(func() transfer.Data {
					pngBytes := tw.ensureFullExport()
					return tempPNGDragData(pngBytes, "SnipGo-Template")
				})
				dragChip.Children(func() {
					ui.Icon(c, iconDragFile).FontSize(13.5).TextColor(p.Accent)
					ui.Text(c, "拖出 PNG").FontSize(11.5).FontWeight(500).NoWrap().TextColor(p.Text)
				})

				ui.Spacer(c)

				liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
					if glassGroupBtn(c, add, iconSave, "保存 PNG", "保存为 PNG (⌘S)") {
						a.saveTemplateResult(tw)
					}
					if glassGroupBtn(c, add, iconPin, "贴图", "钉到桌面作为悬浮贴图 (T)") {
						a.pinTemplateResult(tw)
					}
				})
				if actionPillBtn(c, iconCheck, "复制成图", "复制套好模板的高清图到剪贴板 (⌘C)", true, false) {
					a.copyTemplateResult(tw)
				}
			})
		})
	})
}

func (a *app) paintTemplateChoiceCard(c *ui.Context, tw *templateWindow, meta beautify.TemplateMeta) {
	p := uiPalette(c)
	active := tw.opts.Kind == meta.Kind
	btn := ui.Row(c).
		Label(meta.Name).
		Tooltip(meta.ShortDesc).
		Grow(1).
		Height(33).
		AlignItems(ui.Center).
		Radius(11).
		Cursor(ui.CursorPointer)
	targetScale := float32(1.0)
	if btn.Pressed() {
		targetScale = 0.965
	}
	if sc := btn.AnimateTo("tpl-card-sc", targetScale, springPop); sc != 1.0 {
		btn.Scale(sc, sc)
	}
	if !active {
		bgTarget := p.CardBg
		if btn.Hovered() {
			bgTarget = p.CardBgHover
		}
		btn.Background(btn.AnimateColor("tpl-card-bg", bgTarget, colorMorph)).
			Border(1, p.CardBorder).
			InsetShadow(0, 1, 0, 0, p.CardTopRim)
	}
	btn.Children(func() {
		if active {
			ui.Box(c.Key(fmt.Sprintf("tpl-act-%d", meta.Kind))).
				Absolute().Fill().
				Radius(12).
				Background(p.ActiveBg).
				Border(1, p.ActiveBorder).
				InsetShadow(0, 1, 0, 0, p.CardTopRim).
				ZIndex(0).
				IgnoreInput(true).
				Transition(ui.ElementTransition{
					Match:     tplChoiceMatch{},
					Animation: &springSnappy,
				})
		}
		ic := templateIconFor(meta.Kind)
		icCol := p.TextSecondary
		titleCol := p.Text
		if active {
			icCol = p.ActiveText
			titleCol = p.ActiveText
		}
		ui.Row(c).FillWidth().Padding(0, 9).Gap(6).AlignItems(ui.Center).ZIndex(1).Children(func() {
			ui.Icon(c, ic).FontSize(14).Shrink(0).TextColor(icCol)
			ui.Text(c, meta.Name).FontSize(11.5).FontWeight(600).NoWrap().MaxLines(1).Ellipsis("…").Grow(1).TextColor(titleCol)
		})
	})
	if btn.Clicked() {
		tw.opts.Kind = meta.Kind
		tw.opts.Theme = 0
	}
}

func templateIconFor(k beautify.TemplateKind) *ui.SVG {
	switch k {
	case beautify.TemplatePolaroid:
		return iconPolaroid
	case beautify.TemplateBrowser:
		return iconBrowser
	case beautify.TemplateEditorial:
		return iconEditorial
	case beautify.TemplateReceipt:
		return iconReceipt
	case beautify.TemplateTerminal:
		return iconTerminal
	case beautify.TemplateFilm:
		return iconFilm
	case beautify.TemplateReview:
		return iconReview
	case beautify.TemplatePostcard:
		return iconPostcard
	case beautify.TemplateSlide:
		return iconSlide
	default:
		return iconStickyNote
	}
}

func downscaleForPreview(src *image.NRGBA, maxSide int) (*image.NRGBA, float64) {
	if src == nil || src.Bounds().Empty() || maxSide <= 0 {
		return src, 1.0
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	longSide := max(sw, sh)
	if longSide <= maxSide {
		return src, 1.0
	}
	factor := float64(maxSide) / float64(longSide)
	dw := max(32, int(math.Round(float64(sw)*factor)))
	dh := max(32, int(math.Round(float64(sh)*factor)))
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		sy := b.Min.Y + min(sh-1, int((float64(y)+0.5)/factor))
		for x := range dw {
			sx := b.Min.X + min(sw-1, int((float64(x)+0.5)/factor))
			dst.SetNRGBA(x, y, src.NRGBAAt(sx, sy))
		}
	}
	return dst, factor
}

func fitBox(w, h int, maxW, maxH float32) (float32, float32) {
	if w <= 0 || h <= 0 || maxW <= 0 || maxH <= 0 {
		return maxW, maxH
	}
	rw := maxW / float32(w)
	rh := maxH / float32(h)
	r := min(rw, rh)
	return max(16, float32(w)*r), max(16, float32(h)*r)
}
