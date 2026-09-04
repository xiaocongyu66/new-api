package main

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/task"
)

// task.GetTaskProviderFunc is the port every async-task consumer goes through:
// RunTaskPollingOnce returns immediately when it is nil (so no Suno/video task
// ever reaches a terminal state and no completion billing settles), and the
// video proxy calls it with no nil guard at all.
//
// main sets it from task.GetTaskProviderFuncBinding(). This test runs the same
// binding expression and checks it actually resolves an adaptor, which fails if
// the binding stops being reachable or GetTaskAdaptorFunc is left unset.
func TestTaskProviderBindingResolvesAdaptor(t *testing.T) {
	previousAdaptor := task.GetTaskAdaptorFunc
	previousProvider := task.GetTaskProviderFunc
	t.Cleanup(func() {
		task.GetTaskAdaptorFunc = previousAdaptor
		task.GetTaskProviderFunc = previousProvider
	})

	// main.go wires GetTaskAdaptorFunc first; the binding closes over it, so a
	// binding built before that assignment would resolve nothing.
	wireTaskAdaptorFactory()
	task.GetTaskProviderFunc = task.GetTaskProviderFuncBinding()

	if task.GetTaskProviderFunc == nil {
		t.Fatal("task.GetTaskProviderFunc is nil: polling would no-op and the video proxy would panic")
	}
	if provider := task.GetTaskProviderFunc(string(constant.TaskPlatformSuno)); provider == nil {
		t.Fatalf("binding resolved no provider for platform %q; polling and video proxying cannot reach any adaptor",
			constant.TaskPlatformSuno)
	}
	// An unknown platform must resolve to nil rather than a non-nil wrapper around
	// a nil adaptor, otherwise every consumer's nil check silently stops working.
	if provider := task.GetTaskProviderFunc("definitely-not-a-platform"); provider != nil {
		t.Error("binding returned a non-nil provider for an unknown platform; consumer nil checks would never trip")
	}
}
