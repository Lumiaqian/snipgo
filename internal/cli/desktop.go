package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"snipgo/internal/agentapi"
	"snipgo/internal/agentipc"
)

func (f *Failure) Error() string { return f.Message }

func callDesktop(ctx context.Context, cmd string, args []string) (any, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	// Read consent without invoking the GUI's config repair or migration path.
	data, err := os.ReadFile(filepath.Join(dir, "SnipGo", "settings.json"))
	var cfg struct {
		Enabled bool `json:"allow_agent_ipc"`
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if json.Unmarshal(data, &cfg) != nil || !cfg.Enabled {
		return nil, &Failure{Code: "AGENT_IPC_DISABLED", Message: "请在 SnipGo 设置中开启本机 Agent 桌面与历史权限"}
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	params := map[string]any{}
	var out, redactDir string
	var redact, ocr bool
	switch cmd {
	case "status":
		err = f.Parse(args)
	case "history":
		f.StringVar(&redactDir, "redact-out", "", "")
		latest := f.Bool("latest", false, "")
		limit := f.Int("limit", 5, "")
		if err = f.Parse(args); err == nil {
			params["latest"] = *latest
			params["limit"] = *limit
		}
	case "capture", "record":
		window := f.Uint("window", 0, "")
		rect := f.String("rect", "", "")
		display := f.Int64("display", 0, "")
		f.StringVar(&out, "out", "", "")
		if cmd == "capture" {
			f.BoolVar(&redact, "redact", false, "")
			f.BoolVar(&ocr, "ocr", false, "")
		} else {
			duration := f.Int("duration", 0, "")
			system := f.Bool("system-audio", false, "")
			width := f.Int("width", 960, "")
			fps := f.Int("fps", 15, "")

			err = f.Parse(args)
			if err != nil || *duration < 1 || *duration > 60 || *display != 0 || *width < 1 || *width > 4096 || *fps < 1 || *fps > 60 {
				return nil, errArgs
			}
			params["duration"] = *duration
			params["system_audio"] = *system
			params["width"] = *width
			params["fps"] = *fps
			params["extension"] = filepath.Ext(out)
		}
		if cmd == "capture" {
			err = f.Parse(args)
		}
		count := 0
		for _, present := range []bool{*window != 0, *rect != "", *display != 0} {
			if present {
				count++
			}
		}
		if count != 1 || out == "" {
			return nil, errArgs
		}
		params["window"] = *window
		params["rect"] = *rect
		params["display"] = *display
	case "windows":
		app := f.String("app", "", "")
		if err = f.Parse(args); err == nil {
			params["app"] = *app
		}
	case "pin":
		text := f.String("text", "", "")
		color := f.String("color", "", "")
		if err = f.Parse(args); err == nil {
			params["text"] = *text
			params["color"] = *color
		}
	default:
		return nil, errArgs
	}
	if cmd == "pin" && f.NArg() == 1 && params["color"] == "" && params["text"] == "" {
		file, err := os.Open(f.Arg(0))
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 16<<20+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 16<<20 {
			return nil, errArgs
		}
		params["image"] = data
	} else if err != nil || f.NArg() != 0 {
		return nil, errArgs
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 85*time.Second)
	defer cancel()
	var reply agentipc.Response
	err = agentipc.Call(ctx, filepath.Join(dir, "app.snipgo", "agent.sock"), agentipc.Request{ID: rand.Text(), Method: cmd, Params: raw}, &reply)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Failure{Code: "CANCELED", Message: "request canceled"}
		}
		return nil, &Failure{Code: "HOST_NOT_RUNNING", Message: "SnipGo 桌面宿主未连接"}
	}
	if !reply.OK {
		return nil, &Failure{Code: reply.ErrorCode, Message: reply.Message}
	}
	if redactDir != "" {
		data, ok := reply.Data.(map[string]any)
		if !ok {
			return nil, errors.New("invalid history reply")
		}
		entries, _ := data["entries"].([]any)
		if err = os.MkdirAll(redactDir, 0700); err != nil {
			return nil, err
		}
		for _, item := range entries {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, errArgs
			}
			src, _ := entry["path"].(string)
			dst := filepath.Join(redactDir, "redacted-"+filepath.Base(src))
			if _, err := agentapi.ProcessImage(ctx, []string{src}, agentapi.ImageOptions{Command: "redact", Style: "erase", Out: dst}); err != nil {
				return nil, err
			}
			entry["redacted_path"] = dst
		}
	}
	return saveDesktopMedia(ctx, reply, out, redact, ocr)
}
func saveDesktopMedia(ctx context.Context, reply agentipc.Response, out string, redact, ocr bool) (any, error) {
	if out == "" {
		return reply.Data, nil
	}
	obj, ok := reply.Data.(map[string]any)
	if !ok {
		return nil, errors.New("malformed media reply")
	}
	encoded, ok := obj["media"].(string)
	if !ok {
		return nil, errors.New("missing media")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(out), ".snipgo-agent-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(raw)
	cerr := temp.Close()
	if err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	if redact {
		if _, err := agentapi.ProcessImage(ctx, []string{temp.Name()}, agentapi.ImageOptions{Command: "redact", Out: temp.Name(), Style: "erase"}); err != nil {
			return nil, err
		}
	}
	if ocr {
		result, err := agentapi.ProcessImage(ctx, []string{temp.Name()}, agentapi.ImageOptions{Command: "ocr", Format: "text"})
		if err != nil {
			return nil, err
		}
		obj["ocr"] = result
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.Rename(temp.Name(), out); err != nil {
		return nil, fmt.Errorf("save output: %w", err)
	}
	delete(obj, "media")
	obj["out"] = out
	return obj, nil
}
