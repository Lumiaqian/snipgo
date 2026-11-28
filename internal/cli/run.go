package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/gif"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"snipgo/internal/agentapi"
	"snipgo/internal/beautify"
)

var commands = []string{"status", "version", "help", "ocr", "redact", "cutout", "template", "diff", "gif"}

func Known(name string) bool { return slices.Contains(commands, name) }

type GIFExporter func(context.Context, string, string, float64, float64, int, int) error
type fields = map[string]any

func Run(ctx context.Context, args []string, version string, export GIFExporter, stdout, stderr io.Writer) int {
	cmd := args[0]
	jsonMode := slices.Contains(args[1:], "--json")
	args = slices.DeleteFunc(slices.Clone(args[1:]), func(s string) bool { return s == "--json" })
	data, err := execute(ctx, cmd, args, version, export)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	var fail *Failure
	if err != nil {
		code := "PROCESS_FAILED"
		switch {
		case errors.Is(err, context.Canceled):
			code = "CANCELED"
		case errors.Is(err, errArgs), errors.Is(err, os.ErrNotExist):
			code = "INVALID_ARGS"
		case errors.Is(err, os.ErrPermission):
			code = "PERMISSION_DENIED"
		}
		fail = &Failure{Code: code, Message: err.Error()}
	}
	return writeResponse(stdout, stderr, cmd, data, fail, jsonMode)
}

var errArgs = agentapi.ErrInvalidArgs

func execute(ctx context.Context, cmd string, args []string, version string, export GIFExporter) (any, error) {
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var out, format, style, kind, padding, mode, title, caption string
	var list, date bool
	var theme, trim, threshold, width, fps int
	var ratio, limit, scale, start, end float64
	switch cmd {
	case "ocr":
		f.StringVar(&format, "format", "text", "")
	case "redact":
		f.StringVar(&style, "style", "erase", "")
	case "cutout":
		f.IntVar(&trim, "trim-pad", -1, "")
	case "template":
		f.BoolVar(&list, "list", false, "")
		f.StringVar(&kind, "kind", "sticky_note", "")
		f.StringVar(&padding, "padding", "normal", "")
		f.IntVar(&theme, "theme", 0, "")
		f.StringVar(&title, "title", "", "")
		f.StringVar(&caption, "caption", "", "")
		f.BoolVar(&date, "date", true, "")
		f.Float64Var(&scale, "scale", 1, "")
	case "diff":
		f.StringVar(&mode, "mode", "heatmap", "")
		f.IntVar(&threshold, "threshold", 12, "")
		f.Float64Var(&ratio, "ratio", 0.5, "")
		f.Float64Var(&limit, "max-diff-percent", 100, "")
	case "gif":
		f.IntVar(&width, "width", 960, "")
		f.IntVar(&fps, "fps", 15, "")
		f.Float64Var(&start, "start", 0, "")
		f.Float64Var(&end, "end", 0, "")
	}
	writes := slices.Contains([]string{"redact", "cutout", "template", "diff", "gif"}, cmd)
	if writes {
		f.StringVar(&out, "out", "", "")
	}
	var paths []string
	for len(args) > 0 {
		if args[0] == "--" {
			paths = append(paths, args[1:]...)
			break
		}
		if args[0] == "-" || !strings.HasPrefix(args[0], "-") {
			paths = append(paths, args[0])
			args = args[1:]
			continue
		}
		if err := f.Parse(args); err != nil {
			return nil, fmt.Errorf("%w: %v", errArgs, err)
		}
		args = f.Args()
	}
	switch cmd {
	case "help", "version", "status":
		if len(paths) != 0 {
			return nil, errArgs
		}
		switch cmd {
		case "help":
			return fields{"commands": commands}, nil
		case "status":
			dir, err := os.UserConfigDir()
			if err != nil {
				return nil, err
			}
			var running any
			if runtime.GOOS == "darwin" {
				result, err := exec.CommandContext(ctx, "/usr/bin/lsappinfo", "find", "bundleid=app.snipgo").Output()
				if err == nil {
					running = strings.TrimSpace(string(result)) != ""
				}
			}
			return fields{"version": version, "host_running": running, "socket_path": filepath.Join(dir, "app.snipgo", "agent.sock"), "screen_capture_authorized": nil, "tier1_ready": true, "tier2_ready": false, "agent_ipc_enabled": false}, nil
		}
		return fields{"version": version}, nil
	case "template":
		if list {
			if len(paths) != 0 || out != "" {
				return nil, errArgs
			}
			var entries []fields
			for _, m := range beautify.AllTemplates() {
				var themes []string
				for _, t := range beautify.TemplateThemes(m.Kind) {
					themes = append(themes, t.Name)
				}
				entries = append(entries, fields{"kind": beautify.TemplateKindKey(m.Kind), "name": m.Name, "themes": themes})
			}
			return fields{"templates": entries}, nil
		}
	}
	n := 1
	if cmd == "diff" {
		n = 2
	}
	if len(paths) != n || writes && out == "" {
		return nil, errArgs
	}
	if cmd == "gif" {
		if width <= 0 || width > 4096 || fps < 1 || fps > 60 || !(start >= 0 && (end == 0 || end > start)) || math.IsInf(start, 0) || math.IsInf(end, 0) {
			return nil, errArgs
		}
		if _, err := os.Stat(paths[0]); err != nil {
			return nil, err
		}
		if err := export(ctx, paths[0], out, start, end, width, fps); err != nil {
			return nil, err
		}
		info, err := os.Stat(out)
		if err != nil {
			return nil, err
		}
		file, err := os.Open(out)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		size, err := gif.DecodeConfig(file)
		if err != nil {
			return nil, err
		}
		return fields{"out": out, "width": size.Width, "height": size.Height, "bytes": info.Size(), "fps": fps, "over_github_10mb": info.Size() > 10<<20}, nil
	}
	return agentapi.ProcessImage(ctx, paths, agentapi.ImageOptions{Command: cmd, Out: out, Format: format, Style: style, Kind: kind, Padding: padding, Mode: mode, Title: title, Caption: caption, Date: date, Theme: theme, Trim: trim, Threshold: threshold, Ratio: ratio, Limit: limit, Scale: scale})
}
