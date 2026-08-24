package service

import (
	"context"

	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/model"
)

// SystemTaskHandler executes a claimed task of a specific type. Run owns the
// task lifecycle from claim to terminal state: it MUST call
// model.FinishSystemTask (succeeded/failed) before returning and MUST honor
// ctx cancellation, which the runner triggers if the per-type lock is lost.
type SystemTaskHandler = administration.SystemTaskHandler

// ScheduledSystemTaskHandler is a SystemTaskHandler that the scheduler also
// creates periodically when enabled and the configured interval has elapsed
// since the last run.
type ScheduledSystemTaskHandler = administration.ScheduledSystemTaskHandler

// LogCleanupPayload is the payload for log cleanup tasks.
type LogCleanupPayload = administration.LogCleanupPayload

// LogCleanupState is the state for log cleanup tasks.
type LogCleanupState = administration.LogCleanupState

// LogCleanupResult is the result for log cleanup tasks.
type LogCleanupResult = administration.LogCleanupResult

// SystemTaskProgress is the state shape used by handlers that report percentage
// progress (channel test, model update). The frontend reads the progress field
// (0-100) to render a per-task progress indicator.
type SystemTaskProgress = administration.SystemTaskProgress

// RegisterSystemTaskHandler registers a handler keyed by its Type(). It must be
// called before StartSystemTaskRunner (or any time, since the runner snapshots
// the registry every pass). Re-registering a type replaces the previous handler.
func RegisterSystemTaskHandler(h SystemTaskHandler) {
	administration.RegisterSystemTaskHandler(h)
}

// StartSystemTaskRunner starts the background system task runner on master nodes.
func StartSystemTaskRunner() {
	administration.StartSystemTaskRunner()
}

// StartLogCleanupTask creates an on-demand log cleanup task.
func StartLogCleanupTask(targetTimestamp int64) (*model.SystemTask, error) {
	return administration.StartLogCleanupTask(targetTimestamp)
}

// EnqueueSystemTask creates an on-demand task of the given type.
func EnqueueSystemTask(taskType string, payload any) (*model.SystemTask, bool, error) {
	return administration.EnqueueSystemTask(taskType, payload)
}

// NewSystemTaskProgressReporter returns a throttled progress callback bound to a
// running task. Handlers call it with (processed, total) as they iterate work;
// it persists a {processed,total,progress} state at most once every ~2s, always
// emitting the first update and the final 100%.
// Lock-loss errors are ignored: the lease heartbeat cancels the handler ctx on
// loss, so progress writes are best-effort and never abort the run themselves.
// The returned func is single-goroutine only (call it from the handler loop).
func NewSystemTaskProgressReporter(task *model.SystemTask, runnerID string) func(processed, total int) {
	return administration.NewSystemTaskProgressReporter(task, runnerID)
}

var _ = context.Background
