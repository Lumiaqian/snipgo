package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestColorPickerDefaultsAndNormalize(t *testing.T) {
	def := Default("/tmp/pics")
	if def.ColorPickerShortcut != "Ctrl+Cmd+C" {
		t.Fatalf("default ColorPickerShortcut = %q, want Ctrl+Cmd+C", def.ColorPickerShortcut)
	}
	if def.ColorFormat != "hex" {
		t.Fatalf("default ColorFormat = %q, want hex", def.ColorFormat)
	}

	s := Settings{
		ColorPickerShortcut: "  Ctrl+Cmd+C ",
		ColorFormat:         "  OKLCH ",
	}
	s.Normalize("/tmp/pics")
	if s.ColorPickerShortcut != "Ctrl+Cmd+C" {
		t.Fatalf("normalized ColorPickerShortcut = %q, want Ctrl+Cmd+C", s.ColorPickerShortcut)
	}
	if s.ColorFormat != "oklch" {
		t.Fatalf("normalized ColorFormat = %q, want oklch", s.ColorFormat)
	}

	invalid := Settings{
		ColorPickerShortcut: "   ",
		ColorFormat:         "hsl",
	}
	invalid.Normalize("/tmp/pics")
	if invalid.ColorPickerShortcut != "Ctrl+Cmd+C" {
		t.Fatalf("empty ColorPickerShortcut = %q, want Ctrl+Cmd+C", invalid.ColorPickerShortcut)
	}
	if invalid.ColorFormat != "hex" {
		t.Fatalf("invalid ColorFormat = %q, want hex", invalid.ColorFormat)
	}
}

func TestColorSettingsLoadRoundtripAndBackfill(t *testing.T) {
	dir := t.TempDir()
	// Simulate older settings.json without new fields.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"snip_shortcut":"Cmd+Shift+2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := Load(dir, "/tmp/pics")
	if loaded.ColorPickerShortcut != "Ctrl+Cmd+C" || loaded.ColorFormat != "hex" {
		t.Fatalf("backfilled defaults = %+v", loaded)
	}

	loaded.ColorPickerShortcut = "Alt+Cmd+C"
	loaded.ColorFormat = "tailwind"
	if err := Save(dir, loaded); err != nil {
		t.Fatal(err)
	}
	reloaded := Load(dir, "/tmp/pics")
	if reloaded.ColorPickerShortcut != "Alt+Cmd+C" || reloaded.ColorFormat != "tailwind" {
		t.Fatalf("roundtrip = %+v", reloaded)
	}
}
