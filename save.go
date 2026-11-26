package main

import (
	"errors"
	"fmt"
	"io/fs"

	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// SavePNG 根据偏好设置执行免弹窗一键快存（或弹出另存为对话框），并可选自动同时复制到剪贴板。
func (a *app) SavePNG(data []byte) {
	a.savePNGWithMode(data, !a.cfg.QuickSave, nil)
}

func (a *app) SavePNGAs(data []byte) {
	a.savePNGWithMode(data, true, nil)
}

func (a *app) savePNGWithMode(data []byte, forceDialog bool, completed func(string, error)) {
	if len(data) == 0 {
		if completed != nil {
			completed("", errors.New("截图为空"))
		}
		return
	}
	if a.savePNGHook != nil {
		a.savePNGHook(data, forceDialog, completed)
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

	go func() {
		path, quick, err := writePNG(data, targetDir, name, forceDialog, choosePNGPath)
		runOnMain(func() {
			if completed != nil {
				completed(path, err)
			}
			if err != nil {
				a.sessionStatus("保存失败: " + err.Error())
			} else if quick {
				saved := filepath.Base(path)
				a.sessionStatus("已快存: " + saved)
				a.showActionToast("已快存 " + saved)
			}
		})
	}()
}

func writePNG(data []byte, dir, name string, forceDialog bool, choose func(string) (string, error)) (path string, quick bool, err error) {
	if !forceDialog && dir != "" {
		if err := os.MkdirAll(dir, 0755); err == nil {
			if path, err := writeUniqueFile(dir, name, data); err == nil {
				return path, true, nil
			}
		}
	}
	path, err = choose(filepath.Join(dir, name))
	if err != nil || path == "" {
		return "", false, err
	}
	return path, false, os.WriteFile(path, data, 0644)
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

func choosePNGPath(defaultPath string) (string, error) {
	mygo.App.Focus()
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:             "保存截图",
		DefaultPath:       defaultPath,
		CreateDirectories: true,
		Filters: []mygo.FileFilter{
			{Name: "PNG 图像", Extensions: []string{"png"}},
		},
	})
}
