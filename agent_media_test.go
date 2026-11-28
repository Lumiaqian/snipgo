package main

import (
	"context"
	"errors"
	"image"
	"snipgo/internal/capture"
	"snipgo/internal/videocap"
	"testing"
)

func TestAgentSelectionRequiresOneTarget(t *testing.T) {
	ds := []capture.Display{{ID: 3, Bounds: image.Rect(0, 0, 800, 600), Scale: 1}}
	for _, p := range []agentParams{{}, {Display: 3, Rect: "0,0,20,20"}, {Rect: "1,2,-2,5"}, {Rect: "0,0,32768,32768"}} {
		_, failure := selectionForAgent(p, ds)
		if failure.ErrorCode != "INVALID_ARGS" {
			t.Errorf("%v: %v", p, failure)
		}
	}
	got, failure := selectionForAgent(agentParams{Display: 3}, ds)
	if got != ds[0].Bounds || failure.ErrorCode != "" {
		t.Fatal(got, failure)
	}
	_, failure = selectionForAgent(agentParams{Display: 99}, ds)
	if failure.ErrorCode != "NOT_FOUND" {
		t.Fatal(failure)
	}
}

type audioScopeProbe struct {
	videocap.Recorder
	requested bool
}

func (p *audioScopeProbe) StartWindow(_ context.Context, _ string, _ uint32, _ int64, _ float64, audio bool) error {
	p.requested = audio
	return errors.New("window audio scope unavailable")
}

func TestRecordWindowAudioIsExplicit(t *testing.T) {
	for _, audio := range []bool{false, true} {
		probe := &audioScopeProbe{}
		got := runAgentRecording(context.Background(), &regionRecording{stream: probe}, agentParams{Window: 42, SystemAudio: audio})
		if probe.requested != audio || got.OK || got.Message != "window audio scope unavailable" {
			t.Fatalf("requested %v: probe=%v response=%+v", audio, probe.requested, got)
		}
	}
}
