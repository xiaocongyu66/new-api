package model

import (
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"

	"github.com/bytedance/gopkg/util/gopool"
)

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

// The batch queues live in dbx; each domain registers the writer for its own
// records so no package needs another's unexported persistence helpers.
func init() {
	dbx.RegisterTokenQuotaFlusher(func(deltas map[int]int) {
		for id, delta := range deltas {
			if err := increaseTokenQuota(id, delta); err != nil {
				common.SysLog("failed to batch update token quota: " + err.Error())
			}
		}
	})
	dbx.RegisterChannelUsedQuotaFlusher(func(deltas map[int]int) {
		for id, delta := range deltas {
			updateChannelUsedQuota(id, delta)
		}
	})
	dbx.RegisterUserFlusher(func(quota, usedQuota, requestCount map[int]int) {
		ids := make(map[int]struct{}, len(quota)+len(usedQuota)+len(requestCount))
		for id := range quota {
			ids[id] = struct{}{}
		}
		for id := range usedQuota {
			ids[id] = struct{}{}
		}
		for id := range requestCount {
			ids[id] = struct{}{}
		}
		for id := range ids {
			updateUserQuotaUsedQuotaAndRequestCount(id, quota[id], usedQuota[id], requestCount[id])
		}
	})
}

func batchUpdate() {
	if !dbx.BatchQueuesPending() {
		return
	}
	common.SysLog("batch update started")
	dbx.FlushBatchQueues()
	common.SysLog("batch update finished")
}
