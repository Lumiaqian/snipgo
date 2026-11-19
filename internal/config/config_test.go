package config

import "testing"

func TestConfigSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Default(dir)
	cfg.DelaySeconds = 5
	cfg.QuickSave = false
	cfg.SnipShortcut = "Cmd+Shift+4"
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(dir, dir)
	if got.DelaySeconds != 5 || got.QuickSave != false || got.SnipShortcut != "Cmd+Shift+4" {
		t.Fatalf("Load mismatch: %+v", got)
	}
}
