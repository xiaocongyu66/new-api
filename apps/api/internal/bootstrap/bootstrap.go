package bootstrap

import (
	"context"
	"fmt"
	"time"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	routestats "github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/relay"
	"github.com/QuantumNous/new-api/internal/task"
)

// WireTaskAdaptorFactory connects the async-task capability to the relay
// adaptors. It breaks the task -> relay import cycle: task declares the factory
// and the port, relay owns the adaptors, and bootstrap joins them.
//
// Order matters. GetTaskProviderFuncBinding closes over GetTaskAdaptorFunc, so
// the factory must be assigned first; a binding built before it resolves nothing.
func WireTaskAdaptorFactory() {
	task.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) task.TaskPollingAdaptor {
		a := relay.GetTaskAdaptor(platform)
		if a == nil {
			return nil
		}
		return a
	}
	task.GetTaskProviderFunc = task.GetTaskProviderFuncBinding()
}

// RunRouteStatsSweep evicts stale EWMA entries and orphaned share pools on every
// tick, and returns when ctx is cancelled.
//
// The ticker is held in a variable so it can be stopped. Building it inline in
// the range clause (`for range time.NewTicker(tick).C`) leaves the *time.Ticker
// unreachable, so its runtime timer lives for the life of the process and the
// loop has no way to exit.
//
// One sweep panic must not take the process down: the recover is inside the loop
// body so the next tick still runs.
func RunRouteStatsSweep(ctx context.Context, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysError(fmt.Sprintf("route stats sweep panic: %v", r))
				}
			}()
			if removed := routestats.SweepTTL(); removed > 0 {
				common.SysLog(fmt.Sprintf("route stats sweep: removed %d stale entries", removed))
			}
			keep := catalog.GetActiveRouteStatsPoolKeys()
			if removed := routestats.SweepSharePools(keep); removed > 0 {
				common.SysLog(fmt.Sprintf("route stats sweep: removed %d orphaned share pools", removed))
			}
		}()
	}
}
