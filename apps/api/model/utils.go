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

// batchUpdate flushes the queues dbx accumulates. The queues live in dbx because
// several domains enqueue into them; the writes stay here because they need the
// record types.
func batchUpdate() {
	if !dbx.BatchQueuesPending() {
		return
	}

	common.SysLog("batch update started")
	stores := dbx.DrainBatchQueues()

	for i, store := range stores {
		if i == dbx.BatchUpdateTypeUserQuota || i == dbx.BatchUpdateTypeUsedQuota || i == dbx.BatchUpdateTypeRequestCount {
			continue
		}
		for key, value := range store {
			switch i {
			case dbx.BatchUpdateTypeTokenQuota:
				err := increaseTokenQuota(key, value)
				if err != nil {
					common.SysLog("failed to batch update token quota: " + err.Error())
				}
			case dbx.BatchUpdateTypeChannelUsedQuota:
				updateChannelUsedQuota(key, value)
			}
		}
	}

	userQuotaStore := stores[dbx.BatchUpdateTypeUserQuota]
	usedQuotaStore := stores[dbx.BatchUpdateTypeUsedQuota]
	requestCountStore := stores[dbx.BatchUpdateTypeRequestCount]

	userIDs := make(map[int]struct{}, len(userQuotaStore)+len(usedQuotaStore)+len(requestCountStore))
	for key := range userQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range usedQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range requestCountStore {
		userIDs[key] = struct{}{}
	}
	for key := range userIDs {
		updateUserQuotaUsedQuotaAndRequestCount(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key])
	}
	common.SysLog("batch update finished")
}
