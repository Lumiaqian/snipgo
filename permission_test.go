package main

import (
	"errors"
	"testing"

	"snipgo/internal/capture"
)

func TestAllowCaptureDecision(t *testing.T) {
	oldHas, oldProbe, oldPrompt := hasScreenPermission, probeScreenCapture, promptPermissionAsync
	defer func() { hasScreenPermission, probeScreenCapture, promptPermissionAsync = oldHas, oldProbe, oldPrompt }()

	var probes, prompts int
	promptPermissionAsync = func(*app) { prompts++ }
	a := &app{}

	cases := []struct {
		name      string
		has       bool
		probeErr  error
		wantOK    bool
		wantMsg   string
		wantProbe int
		wantPromt int
	}{
		{"预检通过", true, capture.ErrPermissionDenied, true, "", 0, 0},
		{"预检失败且探测被拒", false, capture.ErrPermissionDenied, false, capture.ErrPermissionDenied.Error(), 1, 1},
		{"预检假阴性探测成功", false, nil, true, "", 1, 0},
		{"预检失败探测其他错误", false, errors.New("选区不在屏幕上"), true, "", 1, 0},
	}
	for _, c := range cases {
		probes, prompts = 0, 0
		hasScreenPermission = func() bool { return c.has }
		probeScreenCapture = func(*app) error { probes++; return c.probeErr }
		ok, msg := a.allowCapture()
		if ok != c.wantOK || msg != c.wantMsg || probes != c.wantProbe || prompts != c.wantPromt {
			t.Fatalf("%s: ok=%v msg=%q probes=%d prompts=%d", c.name, ok, msg, probes, prompts)
		}
	}
}
