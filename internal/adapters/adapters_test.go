package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestReadOnlyAdaptersRunConcurrentlyAndMergeInRegistryOrder(t *testing.T) {
	original := registry
	t.Cleanup(func() { registry = original })

	started := make(chan string, 2)
	release := make(chan struct{})
	makeAdapter := func(name string) adapter {
		return adapter{
			name: name, label: name, area: plan.AreaApps, action: "update", attention: plan.Blocked,
			run: func(c *Context, action Action) int {
				started <- name
				<-release
				addPlan(c.Plan, plan.Item{
					Area: plan.AreaApps, Action: "update", Handling: plan.Automatic,
					Ownership: "declared", Target: name,
				})
				c.Log(Current, name)
				return 0
			},
		}
	}
	registry = []adapter{makeAdapter("first"), makeAdapter("second")}

	value := plan.New()
	var observed []string
	done := make(chan Result, 1)
	go func() {
		done <- runRegistry(context.Background(), platform.NewEnvironment(nil), Plan, nil, false, value, nil,
			func(label string, _ []Event, _ int) { observed = append(observed, label) })
	}()

	for count := 0; count < 2; count++ {
		select {
		case <-started:
		case <-time.After(250 * time.Millisecond):
			close(release)
			<-done
			t.Fatal("read-only adapters ran serially")
		}
	}
	close(release)
	result := <-done

	if result.Code != 0 {
		t.Fatalf("result code = %d", result.Code)
	}
	if got := []string{value.Items()[0].Target, value.Items()[1].Target}; got[0] != "first" || got[1] != "second" {
		t.Fatalf("plan order = %v", got)
	}
	if len(observed) != 2 || observed[0] != "first" || observed[1] != "second" {
		t.Fatalf("observer order = %v", observed)
	}
	if len(result.Events) != 2 || result.Events[0].Message != "first" || result.Events[1].Message != "second" {
		t.Fatalf("event order = %#v", result.Events)
	}
}

func TestApplyAdaptersRemainSerial(t *testing.T) {
	original := registry
	t.Cleanup(func() { registry = original })

	firstFinished := false
	registry = []adapter{
		{name: "first", label: "first", run: func(_ *Context, _ Action) int {
			firstFinished = true
			return 0
		}},
		{name: "second", label: "second", run: func(_ *Context, _ Action) int {
			if !firstFinished {
				t.Error("second adapter started before the first completed")
			}
			return 0
		}},
	}

	result := runRegistry(context.Background(), platform.NewEnvironment(nil), Apply, nil, false, nil, nil, nil)
	if result.Code != 0 {
		t.Fatalf("result code = %d", result.Code)
	}
}

func TestReadOnlyCommandsAreBoundedToFour(t *testing.T) {
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	context := &Context{Context: context.Background(), commands: make(chan struct{}, 4)}
	done := make(chan struct{})
	go func() {
		parallelReadOnly(context, cap(entered), func(_ int) {
			limitedRun(context, func() platform.Result {
				entered <- struct{}{}
				<-release
				return platform.Result{}
			})
		})
		close(done)
	}()

	for range 4 {
		select {
		case <-entered:
		case <-time.After(250 * time.Millisecond):
			close(release)
			<-done
			t.Fatal("four read-only command slots were not used")
		}
	}
	select {
	case <-entered:
		close(release)
		<-done
		t.Fatal("more than four read-only commands ran concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
}
