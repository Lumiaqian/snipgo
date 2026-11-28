package main

import (
	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestRecordingViewsAcrossThemesAndDirections(t *testing.T) {
	a := &app{}
	r := &regionRecording{}
	s := &recordStudio{source: "/tmp/SnipGo.mp4", output: "/tmp/SnipGo.mp4", end: 8, duration: 8, width: 1, rate: 1}
	for _, v := range []struct {
		name   string
		w, h   int
		view   func(*ui.Context)
		labels []string
	}{
		{"controls", 370, 44, func(c *ui.Context) { a.paintRecording(c, r) }, []string{"暂停", "系统音", "麦克风", "停止", "取消"}},
		{"preview", 760, 560, func(c *ui.Context) { a.paintRecordStudio(c, s) }, []string{"关闭录屏预览", "播放 / 暂停", "拖出 MP4", "复制文件", "保存", "裁切区间"}},
	} {
		t.Run(v.name, func(t *testing.T) {
			tt := ui.NewTester(v.view, v.w, v.h)
			tt.Frame()
			opts := uitest.AuditOptions{RequireFocusable: true, RequiredLabels: v.labels}
			uitest.AssertAgentReady(t, tt, opts)
			uitest.AssertThemeParity(t, tt, opts)
			uitest.AssertDirectionParity(t, tt, opts)
		})
	}
}

func TestRecordStudioDragRequiresCurrentOutput(t *testing.T) {
	s := recordStudio{source: "original.mp4", output: "original.mp4", duration: 8, end: 8}
	s.format = 1
	s.dirty()
	if s.output != "" {
		t.Fatal("GIF settings exposed an old MP4")
	}
	s.format = 0
	s.start = 1
	s.dirty()
	if s.output != "" {
		t.Fatal("trimmed range exposed untrimmed source")
	}
	s.start = 0
	s.dirty()
	if s.output != s.source {
		t.Fatal("reset should immediately restore original file")
	}
}

func TestRecordCountdownFirstActionWins(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action func(*ui.Tester, *recordCountdown)
		want   bool
	}{
		{"enter", func(tt *ui.Tester, _ *recordCountdown) { tt.Key(0, ui.KeyEnter) }, true},
		{"space", func(tt *ui.Tester, _ *recordCountdown) { tt.Key(0, ui.KeySpace) }, true},
		{"button", func(tt *ui.Tester, _ *recordCountdown) {
			if err := tt.Click("立即开始"); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"escape", func(tt *ui.Tester, _ *recordCountdown) { tt.Key(0, ui.KeyEscape) }, false},
		{"cancelButton", func(tt *ui.Tester, _ *recordCountdown) {
			if err := tt.Click("取消"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"timeout", func(_ *ui.Tester, cd *recordCountdown) { cd.resolve(true) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var results []bool
			cd := &recordCountdown{deadline: time.Now().Add(3 * time.Second), finish: func(start bool) { results = append(results, start) }}
			tt := ui.NewTester(func(c *ui.Context) { paintRecordCountdown(c, cd) }, 370, 44)
			tt.Frame()
			tc.action(tt, cd)
			tt.Frame()
			if len(results) != 1 || results[0] != tc.want {
				t.Fatalf("first action %v", results)
			}
			cd.resolve(true)
			cd.resolve(false)
			if len(results) != 1 {
				t.Fatalf("queued action fired again: %v", results)
			}
		})
	}
}

func TestRecordStudioSelectionsSurviveIdleFrames(t *testing.T) {
	a := &app{}
	s := &recordStudio{source: "original.mp4", output: "original.mp4", end: 8.37, duration: 8.37, width: 1, rate: 1}
	tt := ui.NewTester(func(c *ui.Context) { a.paintRecordStudio(c, s) }, 680, 500)
	tt.Frame()
	tt.Frame()
	if s.output != s.source || s.format != 0 || s.size != 0 {
		t.Fatalf("untouched MP4 lost drag output: %+v", s)
	}
	if err := tt.Click("GIF"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if s.output != "" || s.format != 1 || s.width != 1 || s.rate != 1 {
		t.Fatalf("pending GIF settings: %+v", s)
	}
	if err := tt.Click("24 fps"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.rate != 2 {
		t.Fatalf("GIF frame rate index = %d", s.rate)
	}
	if err := tt.Click("MP4"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.output != s.source {
		t.Fatal("returning to original settings must restore direct drag")
	}
}
