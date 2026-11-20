package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
)

func TestFormatRecordedShortcut(t *testing.T) {
	cases := []struct {
		name     string
		mods     ui.Modifiers
		key      ui.Key
		physical ui.Key
		want     string
		wantOK   bool
	}{
		{
			name:     "cmd_shift_4",
			mods:     ui.Cmd | ui.Shift,
			key:      ui.Key4,
			physical: ui.Key4,
			want:     "Shift+Cmd+4",
			wantOK:   true,
		},
		{
			name:     "alt_prefers_physical_key",
			mods:     ui.Alt | ui.Cmd,
			key:      ui.KeyUnknown, // macOS Option+2 produces ™ instead of Key2
			physical: ui.Key2,
			want:     "Alt+Cmd+2",
			wantOK:   true,
		},
		{
			name:     "all_mods_ordered",
			mods:     ui.Ctrl | ui.Alt | ui.Shift | ui.Cmd,
			key:      ui.KeyO,
			physical: ui.KeyO,
			want:     "Ctrl+Alt+Shift+Cmd+O",
			wantOK:   true,
		},
		{
			name:     "bare_function_key_allowed",
			mods:     0,
			key:      ui.KeyF6,
			physical: ui.KeyF6,
			want:     "F6",
			wantOK:   true,
		},
		{
			name:     "bare_modifier_rejected",
			mods:     ui.Shift,
			key:      ui.KeyUnknown,
			physical: ui.KeyUnknown,
			wantOK:   false,
		},
		{
			name:     "plain_letter_rejected",
			mods:     0,
			key:      ui.KeyA,
			physical: ui.KeyA,
			wantOK:   false,
		},
		{
			name:     "shift_only_letter_rejected",
			mods:     ui.Shift,
			key:      ui.KeyA,
			physical: ui.KeyA,
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := formatRecordedShortcut(tc.mods, tc.key, tc.physical)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("formatRecordedShortcut(%v, %v, %v) = (%q, %v), want (%q, %v)",
					tc.mods, tc.key, tc.physical, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestFormatMacShortcutSymbolsAndCanonical(t *testing.T) {
	if a, b := canonicalShortcutKey("Shift+Cmd+2"), canonicalShortcutKey("Cmd+Shift+2"); a == "" || a != b {
		t.Fatalf("canonical keys should match regardless of modifier order: %q vs %q", a, b)
	}
	if a, b := canonicalShortcutKey("Ctrl+Cmd+2"), canonicalShortcutKey("Alt+Cmd+2"); a == b {
		t.Fatalf("different modifiers must produce distinct canonical keys: %q", a)
	}

	got := formatMacShortcutSymbols("Ctrl+Shift+Cmd+2")
	want := []string{"⌃", "⇧", "⌘", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatMacShortcutSymbols = %v, want %v", got, want)
	}

	gotOpt := formatMacShortcutSymbols("Alt+Cmd+o")
	wantOpt := []string{"⌥", "⌘", "O"}
	if !reflect.DeepEqual(gotOpt, wantOpt) {
		t.Fatalf("formatMacShortcutSymbols = %v, want %v", gotOpt, wantOpt)
	}
}

func TestSettingsShortcutRecorderInteractive(t *testing.T) {
	dir := t.TempDir()
	a := &app{
		userDataDir: dir,
		picturesDir: dir,
		cfg:         config.Default(dir),
	}
	sw := &settingsWindow{draft: a.cfg}

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintSettingsView(c, sw)
	}, settingsWinW, settingsWinH)
	tt.SetScale(2)

	// 0. 切换到「全局快捷键」分页。
	tt.Frame()
	if err := tt.Click("全局快捷键"); err != nil {
		t.Fatalf("click shortcuts tab: %v", err)
	}
	tt.Frame()

	// 1. 点击「区域截图」旁边的快捷键录制按钮进入录制状态。
	if err := tt.Click("录制区域截图"); err != nil {
		t.Fatalf("click snip recorder: %v", err)
	}
	tt.Frame()
	if sw.recordingID != "snip" {
		t.Fatalf("expected recordingID == \"snip\", got %q", sw.recordingID)
	}

	// 2. 按下与「截图识图 (OCR)」冲突的 Alt+Cmd+O，应拒绝并提示冲突且保持录制态。
	tt.Key(ui.Alt|ui.Cmd, ui.KeyO)
	tt.Frame()
	if sw.recordingID != "snip" || sw.recordingErr == "" {
		t.Fatalf("expected conflict error while still recording, got id=%q err=%q", sw.recordingID, sw.recordingErr)
	}
	if sw.draft.SnipShortcut != "Cmd+Shift+2" {
		t.Fatalf("conflicting shortcut should not overwrite SnipShortcut: got %q", sw.draft.SnipShortcut)
	}

	// 3. 按下有效组合键 Cmd+Shift+4（以及通过 PhysicalKey 验证 Option+组合键不乱码），应立即写入配置并持久化。
	tt.Key(ui.Cmd|ui.Shift, ui.Key4)
	tt.Frame()
	if sw.recordingID != "" {
		t.Fatalf("expected recordingID cleared after valid capture, got %q", sw.recordingID)
	}
	if sw.draft.SnipShortcut != "Shift+Cmd+4" || a.cfg.SnipShortcut != "Shift+Cmd+4" {
		t.Fatalf("expected saved SnipShortcut == \"Shift+Cmd+4\", draft=%q cfg=%q", sw.draft.SnipShortcut, a.cfg.SnipShortcut)
	}
	reloaded := config.Load(dir, dir)
	if reloaded.SnipShortcut != "Shift+Cmd+4" {
		t.Fatalf("reloaded SnipShortcut = %q, want \"Shift+Cmd+4\"", reloaded.SnipShortcut)
	}

	// 4. 用 KeyAt（模拟 macOS Option+数字 Key=KeyUnknown 仅 PhysicalKey 有效）修改「从剪贴板贴图」为 Alt+Cmd+3。
	if err := tt.Click("录制从剪贴板贴图"); err != nil {
		t.Fatalf("click pin recorder: %v", err)
	}
	tt.Frame()
	tt.KeyAt(ui.Alt|ui.Cmd, ui.Key3)
	tt.Frame()
	if sw.draft.PinShortcut != "Alt+Cmd+3" || a.cfg.PinShortcut != "Alt+Cmd+3" {
		t.Fatalf("expected PinShortcut == \"Alt+Cmd+3\", draft=%q cfg=%q", sw.draft.PinShortcut, a.cfg.PinShortcut)
	}

	// 5. 再开启「最近截图历史」录制并按 Esc 取消，不应改动已有配置。
	if err := tt.Click("录制最近截图历史"); err != nil {
		t.Fatalf("click history recorder: %v", err)
	}
	tt.Frame()
	if sw.recordingID != "history" {
		t.Fatalf("expected recordingID == \"history\", got %q", sw.recordingID)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if sw.recordingID != "" {
		t.Fatalf("expected Esc to cancel recording, got recordingID=%q", sw.recordingID)
	}
	if sw.draft.HistoryShortcut != config.Default(dir).HistoryShortcut {
		t.Fatalf("Esc should preserve HistoryShortcut, got %q", sw.draft.HistoryShortcut)
	}
}

func TestVibrancyTintForMode(t *testing.T) {
	if got := vibrancyTintForMode("frosted", true); got != "rgba(18, 20, 28, 0.46)" {
		t.Fatalf("dark frosted tint = %q", got)
	}
	if got := vibrancyTintForMode("frosted", false); got != "rgba(248, 250, 255, 0.44)" {
		t.Fatalf("light frosted tint = %q", got)
	}
	if got := vibrancyTintForMode("regular", true); got != "rgba(18, 20, 28, 0.16)" {
		t.Fatalf("dark regular tint = %q", got)
	}
	if got := vibrancyTintForMode("regular", false); got != "" {
		t.Fatalf("light regular tint = %q, want empty", got)
	}
	if got := vibrancyTintForMode("clear", true); got != "" {
		t.Fatalf("clear tint = %q, want empty", got)
	}
}

func TestDragTempPruning(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	oldFile := filepath.Join(dir, "old.png")
	if err := os.WriteFile(oldFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-2 * time.Hour)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	for i := range 5 {
		p := filepath.Join(dir, string(rune('a'+i))+".png")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := now.Add(time.Duration(i) * time.Second)
		_ = os.Chtimes(p, ts, ts)
	}

	pruneDragTempDirIn(dir, now, 30*time.Minute, 3)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected expired file %s to be pruned", oldFile)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 newest files kept before adding 1 new file (cap=3), got %d", len(entries))
	}
}
