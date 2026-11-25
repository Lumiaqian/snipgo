// Package config 管理 SnipGo 的本地持久化设置（快捷键、快存目录、保存时复制、延时秒数、开机自启）。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Settings 是用户可配置的全部偏好项。
type Settings struct {
	SnipShortcut        string `json:"snip_shortcut"`
	OCRShortcut         string `json:"ocr_shortcut"`
	FreezeShortcut      string `json:"freeze_shortcut"`
	PinShortcut         string `json:"pin_shortcut"`
	HistoryShortcut     string `json:"history_shortcut"`
	DelayShortcut       string `json:"delay_shortcut"`
	ColorPickerShortcut string `json:"color_picker_shortcut,omitempty"`

	QuickSave          bool   `json:"quick_save"`
	QuickSaveDir       string `json:"quick_save_dir"`
	CopyOnSave         bool   `json:"copy_on_save"`
	DelaySeconds       int    `json:"delay_seconds"`
	OpenAtLogin        bool   `json:"open_at_login"`
	AutoCheckUpdates   bool   `json:"auto_check_updates"`
	IncludePrereleases bool   `json:"include_prereleases"`
	SkippedVersion     string `json:"skipped_version,omitempty"`
	ThemeAppearance    string `json:"theme_appearance,omitempty"`
	GlassLevel         string `json:"glass_level,omitempty"`
	AccentColor        string `json:"accent_color,omitempty"`
	// DisableHistory 为 true 时不再保存最近截图历史；默认 false（保存历史）。
	DisableHistory bool `json:"disable_history,omitempty"`
	// ColorFormat 为默认开发者取色格式（"hex" / "rgb" / "oklch" / "tailwind"）。
	ColorFormat string `json:"color_format,omitempty"`
}

// Default 返回带合理默认值的配置。
func Default(defaultPicturesDir string) Settings {
	saveDir := defaultPicturesDir
	if saveDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			saveDir = filepath.Join(home, "Pictures", "SnipGo")
		}
	} else {
		saveDir = filepath.Join(saveDir, "SnipGo")
	}
	return Settings{
		SnipShortcut:        "Cmd+Shift+2",
		OCRShortcut:         "Alt+Cmd+O",
		FreezeShortcut:      "Ctrl+Cmd+2",
		PinShortcut:         "Alt+Cmd+2",
		HistoryShortcut:     "Cmd+Shift+H",
		DelayShortcut:       "Ctrl+Shift+Cmd+2",
		ColorPickerShortcut: "Ctrl+Cmd+C",
		QuickSave:           true,
		QuickSaveDir:        saveDir,
		CopyOnSave:          true,
		DelaySeconds:        3,
		OpenAtLogin:         false,
		AutoCheckUpdates:    true,
		ThemeAppearance:     "system",
		GlassLevel:          "regular",
		AccentColor:         "system",
		ColorFormat:         "hex",
	}
}

// Normalize 修正空字段或越界参数。
func (s *Settings) Normalize(defaultPicturesDir string) {
	def := Default(defaultPicturesDir)
	if strings.TrimSpace(s.SnipShortcut) == "" {
		s.SnipShortcut = def.SnipShortcut
	}
	if strings.TrimSpace(s.OCRShortcut) == "" {
		s.OCRShortcut = def.OCRShortcut
	}
	if strings.TrimSpace(s.FreezeShortcut) == "" {
		s.FreezeShortcut = def.FreezeShortcut
	}
	if strings.TrimSpace(s.PinShortcut) == "" {
		s.PinShortcut = def.PinShortcut
	}
	if strings.TrimSpace(s.HistoryShortcut) == "" {
		s.HistoryShortcut = def.HistoryShortcut
	}
	if strings.TrimSpace(s.DelayShortcut) == "" {
		s.DelayShortcut = def.DelayShortcut
	}
	if strings.TrimSpace(s.QuickSaveDir) == "" {
		s.QuickSaveDir = def.QuickSaveDir
	}
	if s.DelaySeconds != 3 && s.DelaySeconds != 5 && s.DelaySeconds != 10 {
		s.DelaySeconds = 3
	}
	s.SkippedVersion = strings.TrimSpace(s.SkippedVersion)
	switch strings.ToLower(strings.TrimSpace(s.ThemeAppearance)) {
	case "light", "dark", "system":
		s.ThemeAppearance = strings.ToLower(strings.TrimSpace(s.ThemeAppearance))
	default:
		s.ThemeAppearance = "system"
	}
	switch strings.ToLower(strings.TrimSpace(s.GlassLevel)) {
	case "clear", "regular", "frosted":
		s.GlassLevel = strings.ToLower(strings.TrimSpace(s.GlassLevel))
	default:
		s.GlassLevel = "regular"
	}
	switch strings.ToLower(strings.TrimSpace(s.AccentColor)) {
	case "system", "blue", "purple", "pink", "orange", "green", "teal", "graphite":
		s.AccentColor = strings.ToLower(strings.TrimSpace(s.AccentColor))
	default:
		s.AccentColor = "system"
	}
	if strings.TrimSpace(s.ColorPickerShortcut) == "" {
		s.ColorPickerShortcut = def.ColorPickerShortcut
	} else {
		s.ColorPickerShortcut = strings.TrimSpace(s.ColorPickerShortcut)
	}
	switch strings.ToLower(strings.TrimSpace(s.ColorFormat)) {
	case "hex", "rgb", "oklch", "tailwind":
		s.ColorFormat = strings.ToLower(strings.TrimSpace(s.ColorFormat))
	default:
		s.ColorFormat = "hex"
	}
}

// Load 从 baseDir/settings.json 读取配置；文件不存在或损坏时返回默认配置。
// 需要感知损坏/读取错误时使用 LoadChecked。
func Load(baseDir, defaultPicturesDir string) Settings {
	cfg, _ := LoadChecked(baseDir, defaultPicturesDir)
	return cfg
}

// LoadChecked 与 Load 相同，但会返回问题说明：
//   - 文件不存在：返回默认配置与 nil；
//   - 文件无法解析：先把原文件改名为 settings.json.bad-<时间戳> 以免下次保存时被覆盖，返回默认配置与非 nil error；
//   - 其他读取失败：返回默认配置与非 nil error（不动原文件）。
func LoadChecked(baseDir, defaultPicturesDir string) (Settings, error) {
	cfg := Default(defaultPicturesDir)
	if baseDir == "" {
		return cfg, nil
	}
	path := filepath.Join(baseDir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取设置失败: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	loaded := cfg
	if err := json.Unmarshal(data, &loaded); err != nil {
		bad := fmt.Sprintf("%s.bad-%s", path, time.Now().Format("20060102-150405"))
		if rerr := os.Rename(path, bad); rerr != nil {
			return cfg, fmt.Errorf("设置文件损坏且备份失败: %v (%w)", rerr, err)
		}
		return cfg, fmt.Errorf("设置文件损坏，已备份到 %s: %w", filepath.Base(bad), err)
	}
	loaded.Normalize(defaultPicturesDir)
	return loaded, nil
}

// Save 原子写入配置到 baseDir/settings.json。
func Save(baseDir string, cfg Settings) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dst := filepath.Join(baseDir, "settings.json")
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
