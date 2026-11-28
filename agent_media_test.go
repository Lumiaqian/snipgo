package main

import (
	"image"
	"snipgo/internal/capture"
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
