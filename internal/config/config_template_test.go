package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigTemplatePrefsRoundTripAndNormalization(t *testing.T) {
	dir := t.TempDir()
	cfg := Default(dir)
	cfg.TemplateKind = "terminal"
	cfg.TemplateTheme = 2
	cfg.TemplatePadding = "wide"
	cfg.TemplateHideDate = true

	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(dir, dir)
	if got.TemplateKind != "terminal" || got.TemplateTheme != 2 ||
		got.TemplatePadding != "wide" || !got.TemplateHideDate {
		t.Fatalf("template prefs roundtrip mismatch: %+v", got)
	}

	// 非法/越界字段应被 Normalize 兜底；历史配置未写 template_hide_date 时默认保留日期水印（false）。
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{
  "template_kind": "unknown_tpl",
  "template_theme": -3,
  "template_padding": "huge"
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback := Load(dir, dir)
	if fallback.TemplateKind != "sticky_note" || fallback.TemplateTheme != 0 ||
		fallback.TemplatePadding != "normal" || fallback.TemplateHideDate {
		t.Fatalf("Normalize fallback mismatch: %+v", fallback)
	}
}
