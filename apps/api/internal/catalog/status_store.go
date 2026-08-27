package channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var (
	channelStatusLock   sync.Mutex
	channelPollingLocks sync.Map
)

// GetChannelPollingLock returns or creates a mutex for the given channel ID.
// Exported for controller and other capability-internal callers.
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist.
// This is optional and can be called periodically to prevent memory leaks.
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	dbx.DB.Model(&model.Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

// init registers the polling-lock and status-mutation implementations with the
// model package so model-internal callers (GetNextEnabledKey, legacy
// UpdateChannelStatus fallback) can delegate without importing this package.
func init() {
	model.RegisterPollingLockFunc(GetChannelPollingLock)
	model.RegisterUpdateChannelStatusFunc(UpdateChannelStatus)
}

// UpdateChannelStatus updates the status of a channel.
// Returns true if the status was actually changed, false on no-op or error.
// This is the public capability API (catalog.UpdateChannelStatus).
func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	// ChannelInfo stores both multi-key status and the polling cursor. Hold the
	// same per-channel lock from the first read through persistence so neither
	// writer can save a stale JSON snapshot over the other.
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	ok, err := updateChannelStatusWithTx(dbx.DB, channelId, usingKey, status, reason)
	if err != nil || !ok {
		return false
	}

	// Refresh cache only after the mutation has committed. Use the committed
	// channel row as the source of truth so a rolled-back transaction never
	// poisons the in-memory status.
	if common.MemoryCacheEnabled {
		committed, loadErr := model.GetChannelById(channelId, true)
		if loadErr != nil || committed == nil {
			return true
		}
		if committed.ChannelInfo.IsMultiKey {
			model.CacheUpdateChannelStatus(channelId, committed.Status)
		} else {
			if committed.Status == status {
				// Non-multi-key path requested a specific status; reflect it.
				model.CacheUpdateChannelStatus(channelId, status)
			} else {
				model.CacheUpdateChannelStatus(channelId, committed.Status)
			}
		}
	}
	return true
}

// updateChannelStatusWithTx performs the DB channel status mutation plus the
// ability enabled-state mutation inside one MutateGatewayRouting transaction.
// It returns (false, nil) without writing when the stored status already equals
// the requested status (no-op), and (true, nil) after a successful commit.
// The caller owns cache refresh.
func updateChannelStatusWithTx(_ *gorm.DB, channelId int, usingKey string, status int, reason string) (bool, error) {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil || channel == nil {
		return false, err
	}
	if channel.Status == status {
		return false, nil
	}

	// Mutate the in-memory channel the same way the legacy flow did, then
	// decide whether the ability enabled-state must move with it. MutateGatewayRouting
	// owns the outer transaction so the channel row, the ability rows and the
	// routing revision bump commit together.
	shouldUpdateAbilities := false
	if channel.ChannelInfo.IsMultiKey {
		beforeStatus := channel.Status
		model.HandlerMultiKeyUpdate(channel, usingKey, status, reason)
		if beforeStatus != channel.Status {
			shouldUpdateAbilities = true
		}
	} else {
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		channel.Status = status
		shouldUpdateAbilities = true
	}

	_, mutateErr := model.MutateGatewayRouting(func(tx *gorm.DB) error {
		if err := channel.SaveStatusStateWithTx(tx); err != nil {
			return err
		}
		if shouldUpdateAbilities {
			enabled := channel.Status == common.ChannelStatusEnabled
			if err := model.UpdateAbilityStatusWithTx(tx, channelId, enabled); err != nil {
				return err
			}
		}
		return nil
	})
	if mutateErr != nil {
		common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, mutateErr))
		return false, mutateErr
	}
	return true, nil
}
