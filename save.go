package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// SavePNG 根据偏好设置执行免弹窗一键快存（或弹出另存为对话框），并可选自动同时复制到剪贴板。
func (a *app) SavePNG(data []byte) {
	a.savePNGWithMode(data, !a.cfg.QuickSave)
}

func (a *app) SavePNGAs(data []byte) {
	a.savePNGWithMode(data, true)
}

func (a *app) savePNGWithMode(data []byte, forceDialog bool) {
	if len(data) == 0 {
		return
	}
	if a.savePNGHook != nil {
		a.savePNGHook(data, forceDialog)
		return
	}
	if a.cfg.CopyOnSave {
		_ = mygo.Clipboard.WriteImage(data)
	}
	name := "SnipGo-" + time.Now().Format("2006-01-02-150405") + ".png"
	targetDir := a.cfg.QuickSaveDir
	if targetDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			targetDir = filepath.Join(home, "Desktop")
		}
	}

	if !forceDialog && targetDir != "" {
		go func() {
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				a.savePNGDialog(data, filepath.Join(targetDir, name))
				return
			}
			full, err := writeUniqueFile(targetDir, name, data)
			if err != nil {
				a.savePNGDialog(data, filepath.Join(targetDir, name))
				return
			}
			saved := filepath.Base(full)
			mygo.RunOnMain(func() {
				a.sessionStatus("已快存: " + saved)
				a.showActionToast("已快存 " + saved)
			})
		}()
		return
	}
	go a.savePNGDialog(data, filepath.Join(targetDir, name))
}

// writeUniqueFile 以 O_EXCL 写入 dir/name，重名时在扩展名前追加 -1、-2… 后缀，返回实际路径。
func writeUniqueFile(dir, name string, data []byte) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		full := filepath.Join(dir, n)
		f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(full)
			return "", werr
		}
		return full, nil
	}
	return "", fmt.Errorf("文件名冲突过多: %s", name)
}

func (a *app) savePNGDialog(data []byte, defaultPath string) {
	mygo.App.Focus()
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:             "保存截图",
		DefaultPath:       defaultPath,
		CreateDirectories: true,
		Filters: []mygo.FileFilter{
			{Name: "PNG 图像", Extensions: []string{"png"}},
		},
	})
	if err != nil {
		log.Printf("保存对话框失败: %v", err)
		return
	}
	if path == "" {
		return
	}
	// 用户在系统对话框中已确认覆盖，这里允许直接写入
	if err := os.WriteFile(path, data, 0o644); err != nil {
		mygo.RunOnMain(func() { a.sessionStatus("保存失败: " + err.Error()) })
	}
}
