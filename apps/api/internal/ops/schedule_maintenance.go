package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	systemTaskRunnerIdleInterval = 15 * time.Second
	systemTaskLockTTL            = 60 * time.Second
	logCleanupBatchSize          = 100
	systemTaskSchedulerInterval  = 15 * time.Second
	systemTaskStaleLockInterval  = 30 * time.Second
	systemInstanceReportInterval = 30 * time.Second
)

// SystemTaskHandler executes a claimed task of a specific type. Run owns the
// task lifecycle from claim to terminal state: it MUST call
// ops.FinishSystemTask (succeeded/failed) before returning and MUST honor
// ctx cancellation, which the runner triggers if the per-type lock is lost.
type SystemTaskHandler interface {
	Type() string
	Run(ctx context.Context, task *model.SystemTask, runnerID string)
}

// ScheduledSystemTaskHandler is a SystemTaskHandler that the scheduler also
// creates periodically when enabled and the configured interval has elapsed
// since the last run.
type ScheduledSystemTaskHandler interface {
	SystemTaskHandler
	Enabled() bool
	Interval() time.Duration
	NewPayload() any
}

var (
	systemTaskHandlersMu sync.RWMutex
	systemTaskHandlers   = map[string]SystemTaskHandler{}
)

// RegisterSystemTaskHandler registers a handler keyed by its Type(). It must be
// called before StartSystemTaskRunner (or any time, since the runner snapshots
// the registry every pass). Re-registering a type replaces the previous handler.
func RegisterSystemTaskHandler(h SystemTaskHandler) {
	if h == nil {
		return
	}
	systemTaskHandlersMu.Lock()
	defer systemTaskHandlersMu.Unlock()
	systemTaskHandlers[h.Type()] = h
}

func registeredSystemTaskHandlers() []SystemTaskHandler {
	systemTaskHandlersMu.RLock()
	defer systemTaskHandlersMu.RUnlock()
	handlers := make([]SystemTaskHandler, 0, len(systemTaskHandlers))
	for _, h := range systemTaskHandlers {
		handlers = append(handlers, h)
	}
	return handlers
}

// logCleanupHandler wraps the existing on-demand log cleanup task as a
// registered (non-scheduled) handler. It is created via StartLogCleanupTask.
type logCleanupHandler struct{}

func (logCleanupHandler) Type() string { return model.SystemTaskTypeLogCleanup }

func (logCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runLogCleanupTask(ctx, task, runnerID)
}

func init() {
	RegisterSystemTaskHandler(logCleanupHandler{})
}

type LogCleanupPayload struct {
	TargetTimestamp int64 `json:"target_timestamp"`
	BatchSize       int   `json:"batch_size"`
}

type LogCleanupState struct {
	Total     int64 `json:"total"`
	Processed int64 `json:"processed"`
	Progress  int   `json:"progress"`
	Remaining int64 `json:"remaining"`
}

type LogCleanupResult struct {
	DeletedCount int64 `json:"deleted_count"`
}

var (
	systemTaskRunnerOnce sync.Once
	systemTaskWakeup     = make(chan struct{}, 1)
)

// notifySystemTaskRunner wakes the runner without blocking. If a wakeup is
// already pending it is a no-op, which is fine since one pass drains all work.
func notifySystemTaskRunner() {
	select {
	case systemTaskWakeup <- struct{}{}:
	default:
	}
}

// StartSystemTaskRunner starts the background system task runner on master nodes.
func StartSystemTaskRunner() {
	systemTaskRunnerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		runnerID := fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(8))
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("system task runner started: runner=%s idle_interval=%s", runnerID, systemTaskRunnerIdleInterval))

			ticker := time.NewTicker(systemTaskRunnerIdleInterval)
			defer ticker.Stop()

			var lastScheduler time.Time
			var lastStaleLockCleanup time.Time
			runPass := func() {
				now := time.Now()
				if now.Sub(lastStaleLockCleanup) >= systemTaskStaleLockInterval {
					lastStaleLockCleanup = now
					if err := ExpireStaleSystemTaskLocks(common.GetTimestamp()); err != nil {
						logger.LogWarn(context.Background(), fmt.Sprintf("system task stale lock cleanup failed: %v", err))
					}
				}
				if now.Sub(lastScheduler) >= systemTaskSchedulerInterval {
					lastScheduler = now
					runSystemTaskScheduler()
				}
				runSystemTaskClaimPass(runnerID)
			}

			runPass()
			for {
				select {
				case <-ticker.C:
				case <-systemTaskWakeup:
				}
				runPass()
			}
		})
	})
}

// StartLogCleanupTask creates an on-demand log cleanup task.
func StartLogCleanupTask(targetTimestamp int64) (*model.SystemTask, error) {
	if targetTimestamp <= 0 {
		return nil, errors.New("target timestamp is required")
	}

	activeTask, err := GetActiveSystemTask(model.SystemTaskTypeLogCleanup)
	if err != nil {
		return nil, err
	}
	if activeTask != nil {
		return activeTask, nil
	}

	payload := LogCleanupPayload{
		TargetTimestamp: targetTimestamp,
		BatchSize:       logCleanupBatchSize,
	}
	state := LogCleanupState{}
	task, err := CreateSystemTask(model.SystemTaskTypeLogCleanup, payload, state)
	if err != nil {
		activeTask, activeErr := GetActiveSystemTask(model.SystemTaskTypeLogCleanup)
		if activeErr == nil && activeTask != nil {
			return activeTask, nil
		}
		return nil, err
	}
	notifySystemTaskRunner()
	return task, nil
}

// EnqueueSystemTask creates an on-demand task of the given type.
func EnqueueSystemTask(taskType string, payload any) (*model.SystemTask, bool, error) {
	activeTask, err := GetActiveSystemTask(taskType)
	if err != nil {
		return nil, false, err
	}
	if activeTask != nil {
		return activeTask, false, nil
	}

	task, err := CreateSystemTask(taskType, payload, nil)
	if err != nil {
		activeTask, activeErr := GetActiveSystemTask(taskType)
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

// runSystemTaskClaimPass tries to claim one pending task per registered type
// and dispatches each claimed task in its own goroutine so a long-running
// handler (e.g. channel test) never blocks another type (e.g. log cleanup).
func runSystemTaskClaimPass(runnerID string) {
	handlers := registeredSystemTaskHandlers()
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		taskTypes = append(taskTypes, handler.Type())
	}
	pendingTasks, err := FindEarliestPendingSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task runner query failed: %v", err))
		return
	}
	for _, handler := range handlers {
		task := pendingTasks[handler.Type()]
		if task == nil {
			continue
		}
		claimedTask, claimed, err := ClaimSystemTask(task.ID, handler.Type(), runnerID, systemTaskLockUntil())
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("system task claim failed: %v", err))
			continue
		}
		if !claimed {
			continue
		}
		dispatchHandler := handler
		dispatchTask := claimedTask
		gopool.Go(func() {
			runWithLeaseHeartbeat(dispatchTask, runnerID, func(ctx context.Context) {
				dispatchHandler.Run(ctx, dispatchTask, runnerID)
			})
		})
	}
}

// runSystemTaskScheduler creates a new task row for each enabled scheduled
// handler whose interval has elapsed since its last run and that has no active
// row. The task active_key unique index deduplicates concurrent creation while
// the per-type lock guarantees only one runner executes the task.
func runSystemTaskScheduler() {
	now := common.GetTimestamp()
	handlers := registeredSystemTaskHandlers()
	scheduledHandlers := make([]ScheduledSystemTaskHandler, 0, len(handlers))
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		scheduled, ok := handler.(ScheduledSystemTaskHandler)
		if !ok || !scheduled.Enabled() {
			continue
		}
		scheduledHandlers = append(scheduledHandlers, scheduled)
		taskTypes = append(taskTypes, scheduled.Type())
	}
	latestTasks, err := GetLatestSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler query failed: %v", err))
		return
	}
	for _, scheduled := range scheduledHandlers {
		latest := latestTasks[scheduled.Type()]
		if latest != nil {
			if latest.Status == model.SystemTaskStatusPending || latest.Status == model.SystemTaskStatusRunning {
				continue
			}
			if now-latest.UpdatedAt < int64(scheduled.Interval().Seconds()) {
				continue
			}
		}
		if _, err := CreateSystemTask(scheduled.Type(), scheduled.NewPayload(), nil); err != nil {
			activeTask, activeErr := GetActiveSystemTask(scheduled.Type())
			if activeErr == nil && activeTask != nil {
				continue
			}
			if activeErr != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler active lookup failed: type=%s err=%v", scheduled.Type(), activeErr))
			}
			logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler create failed: type=%s err=%v", scheduled.Type(), err))
			continue
		}
	}
}

// runWithLeaseHeartbeat renews the per-type lock on a background ticker while
// fn runs. The TTL is a crash-detection window, not a task time limit: an
// arbitrarily long handler stays alive as long as the heartbeat succeeds.
func runWithLeaseHeartbeat(task *model.SystemTask, runnerID string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := systemTaskLockTTL / 3
	if interval <= 0 {
		interval = systemTaskLockTTL
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := RenewSystemTaskLock(task.TaskID, runnerID, systemTaskLockUntil()); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	fn(ctx)
	close(done)
}

func runLogCleanupTask(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := LogCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if payload.TargetTimestamp <= 0 {
		failSystemTask(task, runnerID, errors.New("target timestamp is required"))
		return
	}
	if payload.BatchSize <= 0 {
		payload.BatchSize = logCleanupBatchSize
	}

	state := LogCleanupState{}
	if err := task.DecodeState(&state); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}

	for {
		remaining, err := usage.CountOldLog(ctx, payload.TargetTimestamp)
		if err != nil {
			failSystemTask(task, runnerID, err)
			return
		}
		syncLogCleanupStateFromRemaining(&state, remaining)
		if err := UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			logSystemTaskLockError(ctx, task, err)
			return
		}
		if state.Remaining == 0 {
			break
		}

		progressed := false
		for state.Remaining > 0 {
			rowsAffected, err := usage.DeleteOldLogBatch(ctx, payload.TargetTimestamp, payload.BatchSize)
			if err != nil {
				failSystemTask(task, runnerID, err)
				return
			}
			if rowsAffected == 0 {
				break
			}
			progressed = true

			state.Processed += rowsAffected
			if state.Total < state.Processed {
				state.Total = state.Processed
			}
			if state.Remaining > rowsAffected {
				state.Remaining -= rowsAffected
			} else {
				state.Remaining = 0
			}
			state.Progress = logCleanupProgress(state.Processed, state.Total)

			if err := UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
				logSystemTaskLockError(ctx, task, err)
				return
			}
		}

		if !progressed {
			failSystemTask(task, runnerID, errors.New("no log rows were deleted"))
			return
		}
	}

	state.Remaining = 0
	state.Progress = 100
	if state.Total < state.Processed {
		state.Total = state.Processed
	}
	if err := UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}

	result := LogCleanupResult{DeletedCount: state.Processed}
	if err := FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func syncLogCleanupStateFromRemaining(state *LogCleanupState, remaining int64) {
	if state.Total <= 0 {
		state.Total = remaining
		state.Processed = 0
	} else {
		processedFromRemaining := state.Total - remaining
		if processedFromRemaining > state.Processed {
			state.Processed = processedFromRemaining
		}
	}
	if state.Processed < 0 {
		state.Processed = 0
	}
	state.Remaining = remaining
	state.Progress = logCleanupProgress(state.Processed, state.Total)
}

func logCleanupProgress(processed int64, total int64) int {
	if total <= 0 {
		return 100
	}
	if processed <= 0 {
		return 0
	}
	if processed >= total {
		return 100
	}
	return int(processed * 100 / total)
}

func systemTaskLockUntil() int64 {
	return common.GetTimestamp() + int64(systemTaskLockTTL.Seconds())
}

// SystemTaskProgress is the state shape used by handlers that report percentage
// progress (channel test, model update). The frontend reads the progress field
// (0-100) to render a per-task progress indicator.
type SystemTaskProgress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Progress  int `json:"progress"`
}

// NewSystemTaskProgressReporter returns a throttled progress callback bound to a
// running task. Handlers call it with (processed, total) as they iterate work;
// it persists a {processed,total,progress} state at most once every ~2s, always
// emitting the first update and the final 100%.
// Lock-loss errors are ignored: the lease heartbeat cancels the handler ctx on
// loss, so progress writes are best-effort and never abort the run themselves.
// The returned func is single-goroutine only (call it from the handler loop).
func NewSystemTaskProgressReporter(task *model.SystemTask, runnerID string) func(processed, total int) {
	const minWriteInterval = 2 * time.Second
	var (
		lastWriteAt  time.Time
		lastProgress = -1
	)
	return func(processed, total int) {
		progress := 100
		if total > 0 {
			progress = processed * 100 / total
		}
		if progress < 0 {
			progress = 0
		} else if progress > 100 {
			progress = 100
		}

		if progress < 100 {
			if progress == lastProgress {
				return
			}
			if !lastWriteAt.IsZero() && time.Since(lastWriteAt) < minWriteInterval {
				return
			}
		}
		lastProgress = progress
		lastWriteAt = time.Now()

		state := SystemTaskProgress{Total: total, Processed: processed, Progress: progress}
		_ = UpdateSystemTaskState(task.TaskID, runnerID, state)
	}
}

func failSystemTask(task *model.SystemTask, runnerID string, err error) {
	logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed: %v", task.TaskID, err))
	if finishErr := FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error()); finishErr != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed to save failure state: %v", task.TaskID, finishErr))
	}
}

func logSystemTaskLockError(ctx context.Context, task *model.SystemTask, err error) {
	if errors.Is(err, model.ErrSystemTaskLockLost) {
		logger.LogWarn(ctx, fmt.Sprintf("system task %s lock lost", task.TaskID))
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("system task %s update failed: %v", task.TaskID, err))
}

// ---- System Instance Reporter ----

var systemInstanceReporterOnce sync.Once

// StartSystemInstanceReporter starts the background system instance reporter.
func StartSystemInstanceReporter() {
	systemInstanceReporterOnce.Do(func() {
		gopool.Go(func() {
			reportSystemInstanceWithLog()

			ticker := time.NewTicker(systemInstanceReportInterval)
			defer ticker.Stop()
			for range ticker.C {
				reportSystemInstanceWithLog()
			}
		})
	})
}

// ReportCurrentSystemInstance reports the current system instance info to DB.
func ReportCurrentSystemInstance() error {
	identity := common.GetNodeIdentity()
	hostname, hostnameErr := os.Hostname()
	if strings.TrimSpace(identity.Name) == "" {
		if hostnameErr != nil || strings.TrimSpace(hostname) == "" {
			return fmt.Errorf("system instance node name is empty")
		}
		identity.Name = hostname
		identity.Source = common.NodeNameSourceHostname
		identity.ManuallyConfigured = false
		identity.ShouldConfigureManually = true
	}
	systemStatus := common.GetSystemStatus()
	diskInfo := common.GetDiskSpaceInfo()
	info := SystemInstanceInfo{
		SchemaVersion: 1,
		Node:          identity,
		Role: SystemInstanceRoleInfo{
			IsMaster: common.IsMasterNode,
		},
		Runtime: SystemInstanceRuntimeInfo{
			Version:   common.Version,
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			StartedAt: common.StartTime,
		},
		Host: SystemInstanceHostInfo{
			Hostname: hostname,
		},
		Resources: SystemInstanceResources{
			CPU: SystemInstanceResourceUsage{
				UsagePercent: systemStatus.CPUUsage,
			},
			Memory: SystemInstanceResourceUsage{
				UsagePercent: systemStatus.MemoryUsage,
			},
			Storage: SystemInstanceStorageMetrics{
				TotalBytes:  diskInfo.Total,
				UsedBytes:   diskInfo.Used,
				FreeBytes:   diskInfo.Free,
				UsedPercent: diskInfo.UsedPercent,
			},
		},
	}
	return UpsertSystemInstance(identity.Name, info, common.StartTime, common.GetTimestamp())
}

func reportSystemInstanceWithLog() {
	if err := ReportCurrentSystemInstance(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system instance report failed: %v", err))
	}
}

// SystemInstanceInfo matches service.SystemInstanceInfo for reporting.
type SystemInstanceInfo struct {
	SchemaVersion int                       `json:"schema_version"`
	Node          common.NodeIdentity       `json:"node"`
	Role          SystemInstanceRoleInfo    `json:"role"`
	Runtime       SystemInstanceRuntimeInfo `json:"runtime"`
	Host          SystemInstanceHostInfo    `json:"host"`
	Resources     SystemInstanceResources   `json:"resources,omitempty"`
	Extra         map[string]any            `json:"extra,omitempty"`
}

type SystemInstanceRoleInfo struct {
	IsMaster bool `json:"is_master"`
}

type SystemInstanceRuntimeInfo struct {
	Version   string `json:"version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	StartedAt int64  `json:"started_at"`
}

type SystemInstanceHostInfo struct {
	Hostname string `json:"hostname"`
}

type SystemInstanceResources struct {
	CPU     SystemInstanceResourceUsage  `json:"cpu"`
	Memory  SystemInstanceResourceUsage  `json:"memory"`
	Storage SystemInstanceStorageMetrics `json:"storage"`
}

type SystemInstanceResourceUsage struct {
	UsagePercent float64 `json:"usage_percent"`
}

type SystemInstanceStorageMetrics struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}
