package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/egoist/mygo"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"snipgo/internal/agentipc"
	"snipgo/internal/beautify"
	"snipgo/internal/capture"
	"strings"
)

func (a *app) stopAgentServer() {
	if a.agentServer != nil {
		a.agentServer.Close()
		a.agentServer = nil
	}
}
func (a *app) syncAgentServer() {
	a.stopAgentServer()
	if !a.cfg.AllowAgentIPC || a.userDataDir == "" {
		return
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	server, err := agentipc.Listen(filepath.Join(dir, "app.snipgo"), a.agentRequest)
	if err != nil {
		log.Printf("agent socket: %v", err)
		return
	}
	a.agentServer = server
}

type agentParams struct {
	Limit                                    int
	Latest                                   bool
	App, Text, Color, Image, Rect, Extension string
	Window                                   uint32
	Display                                  int64
	Duration, Width, FPS                     int
	SystemAudio                              bool `json:"system_audio"`
}

func (a *app) agentRequest(ctx context.Context, r agentipc.Request) agentipc.Response {
	var params agentParams
	decoder := json.NewDecoder(bytes.NewReader(r.Params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return agentipc.Response{ErrorCode: "INVALID_ARGS", Message: "invalid request parameters"}
	}
	done := make(chan agentipc.Response, 1)
	mygo.RunOnMain(func() {
		if ctx.Err() != nil || !a.cfg.AllowAgentIPC {
			done <- agentipc.Response{ErrorCode: "AGENT_IPC_DISABLED", Message: "desktop access is disabled"}
			return
		}
		if r.Method == "capture" || r.Method == "record" {
			a.beginAgentMedia(ctx, r.Method, params, done)
			return
		}
		done <- a.agentUIRequest(r.Method, params)
	})
	select {
	case res := <-done:
		return res
	case <-ctx.Done():
		return agentipc.Response{ErrorCode: "CANCELED", Message: "request canceled"}
	}
}
func (a *app) agentUIRequest(method string, p agentParams) agentipc.Response {
	switch method {
	case "status":
		return agentipc.Response{OK: true, Data: map[string]any{"screen_capture_authorized": capture.HasPermission()}}
	case "history":
		if p.Latest {
			p.Limit = 1
		}
		if p.Limit < 1 || p.Limit > 100 {
			return agentipc.Response{ErrorCode: "INVALID_ARGS", Message: "limit must be 1–100"}
		}
		entries := []map[string]any{}
		if a.history != nil {
			list := a.history.List()
			for _, e := range list[:min(p.Limit, len(list))] {
				w, h := e.DipSize()
				entries = append(entries, map[string]any{"id": e.ID, "path": e.FilePath, "created_at": e.CreatedAt, "width": e.Width, "height": e.Height, "dip_width": w, "dip_height": h})
			}
		}
		return agentipc.Response{OK: true, Data: map[string]any{"entries": entries}}
	case "windows":
		displays, err := a.currentDisplays()
		if err != nil {
			return agentipc.Response{ErrorCode: "PROCESS_FAILED", Message: err.Error()}
		}
		cur := mygo.Screen.CursorScreenPoint()
		front, wins := capture.VisibleWindowInfos(image.Pt(cur.X, cur.Y), displays, overlayTitle)
		windows := []map[string]any{}
		for _, w := range wins {
			if p.App != "" && !strings.EqualFold(p.App, w.App) {
				continue
			}
			var displayID int64
			bestArea := 0
			for _, d := range displays {
				overlap := d.Bounds.Intersect(w.Bounds)
				area := overlap.Dx() * overlap.Dy()
				if area > bestArea {
					displayID = d.ID
					bestArea = area
				}
			}
			windows = append(windows, map[string]any{"id": w.ID, "app": w.App, "title": w.Title, "display_id": displayID, "frontmost": front.ID == w.ID, "bounds": map[string]int{"x": w.Bounds.Min.X, "y": w.Bounds.Min.Y, "w": w.Bounds.Dx(), "h": w.Bounds.Dy()}})
		}
		return agentipc.Response{OK: true, Data: map[string]any{"windows": windows}}
	case "pin":
		var img *image.NRGBA
		if p.Image != "" && p.Text == "" && p.Color == "" {
			raw, err := base64.StdEncoding.DecodeString(p.Image)
			if err != nil {
				return agentError("INVALID_ARGS", "invalid image encoding")
			}
			decoded, err := decodeImageLimited(raw)
			if err != nil {
				return agentipc.Response{ErrorCode: "INVALID_ARGS", Message: "invalid image"}
			}
			img = toNRGBA(decoded)
		} else if p.Text != "" && p.Color == "" {
			img = beautify.RenderTextPin(p.Text, 1)
		} else if c, ok := beautify.ParseColor(p.Color); ok && p.Text == "" {
			img = beautify.RenderPaletteCard([]color.NRGBA{c}, 1)
		}
		if img == nil {
			return agentipc.Response{ErrorCode: "INVALID_ARGS", Message: "provide text or a color"}
		}
		cur := mygo.Screen.CursorScreenPoint()
		a.ShowPin(img, nil, image.Pt(cur.X, cur.Y), img.Bounds().Dx(), img.Bounds().Dy())
		return agentipc.Response{OK: true, Data: map[string]bool{"pinned": true}}
	default:
		return agentipc.Response{ErrorCode: "INVALID_ARGS", Message: "unknown desktop command"}
	}
}
