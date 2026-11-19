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
