package adapters

import (
	"context"
	"io"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestNonInteractiveApplyContinuesPastAttendedGate(t *testing.T) {
	original := registry
	defer func() { registry = original }()
	runAutomatic := false
	registry = []adapter{
		{
			label: "Attended setup",
			run: func(c *Context, action Action) int {
				c.Log(Attention, "human action required")
				return 2
			},
			blocksOnAttention: true,
		},
		{
			label: "Independent automatic setup",
			run: func(_ *Context, _ Action) int {
				runAutomatic = true
				return 0
			},
		},
	}
	env := platform.NewEnvironment([]string{"USERLAND_NON_INTERACTIVE=1"})
	result := runSerialRegistry(context.Background(), env, Apply, nil, io.Discard, false, nil, nil, nil, nil, nil)
	if result.Code != 0 || !result.Attention || !runAutomatic {
		t.Fatalf("result = %#v, automatic step ran = %t", result, runAutomatic)
	}
}
