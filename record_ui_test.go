package main

import (
	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"
	"testing"
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
