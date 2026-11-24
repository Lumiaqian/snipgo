package main

import (
	"context"
	"image"
	"testing"

	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

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
