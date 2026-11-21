package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Default(dir)
	cfg.DelaySeconds = 5
	cfg.QuickSave = false
	cfg.SnipShortcut = "Cmd+Shift+4"
	cfg.AutoCheckUpdates = false
	cfg.IncludePrereleases = true
	cfg.SkippedVersion = "0.2.0"
	cfg.ThemeAppearance = "dark"
	cfg.GlassLevel = "frosted"
	cfg.AccentColor = "purple"
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(dir, dir)
	if got.DelaySeconds != 5 || got.QuickSave != false || got.SnipShortcut != "Cmd+Shift+4" ||
		got.AutoCheckUpdates != false || got.IncludePrereleases != true || got.SkippedVersion != "0.2.0" ||
		got.ThemeAppearance != "dark" || got.GlassLevel != "frosted" || got.AccentColor != "purple" {
		t.Fatalf("Load mismatch: %+v", got)
	}
}

func TestConfigLegacyJSONKeepsAutoCheckDefaultTrue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"quick_save": false}`), 0o600); err != nil {
		t.Fatalf("write legacy settings: %v", err)
	}
	got := Load(dir, dir)
	if !got.AutoCheckUpdates {
		t.Fatalf("expected legacy config to default AutoCheckUpdates=true, got false")
	}
	if got.ThemeAppearance != "system" || got.GlassLevel != "regular" || got.AccentColor != "system" {
		t.Fatalf("unexpected theme defaults: %+v", got)
	}
}

func TestLoadCheckedBacksUpCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	corrupt := []byte(`{"quick_save": false, "delay_seconds": `)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadChecked(dir, dir)
	if err == nil {
		t.Fatal("损坏文件应返回错误")
	}
	want := Default(dir)
	want.Normalize(dir)
	if got != want {
		t.Fatalf("损坏时应返回默认配置: %+v", got)
	}
	baks, _ := filepath.Glob(filepath.Join(dir, "settings.json.bad-*"))
	if len(baks) != 1 {
		t.Fatalf("应生成 1 个备份, got %v", baks)
	}
	if b, _ := os.ReadFile(baks[0]); string(b) != string(corrupt) {
		t.Fatalf("备份内容应为原始字节, got %q", b)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatalf("损坏的原文件应已被移走: %v", serr)
	}
	// 之后保存不会覆盖备份，且新文件可正常读回。
	if err := Save(dir, got); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(baks[0]); string(b) != string(corrupt) {
		t.Fatal("保存后备份不应改变")
	}
	if b, _ := os.ReadFile(path); string(b) == string(corrupt) {
		t.Fatal("原路径应已是新写入的有效配置")
	}
	if _, err := LoadChecked(dir, dir); err != nil {
		t.Fatalf("保存后应能正常加载: %v", err)
	}
}

func TestLoadCheckedMissingOrValidHasNoError(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadChecked(dir, dir); err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if err := Save(dir, Default(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadChecked(dir, dir); err != nil {
		t.Fatalf("有效文件不应报错: %v", err)
	}
	if baks, _ := filepath.Glob(filepath.Join(dir, "*.bad-*")); len(baks) != 0 {
		t.Fatalf("不应产生备份: %v", baks)
	}
}

func TestSaveSurfacesError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(blocker, Default(dir)); err == nil {
		t.Fatal("baseDir 是文件时 Save 应返回错误")
	}
}
