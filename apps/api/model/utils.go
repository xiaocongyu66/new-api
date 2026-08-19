package model

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var BatchUpdateStores []map[int]int
var BatchUpdateLocks []sync.Mutex

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		BatchUpdateStores = append(BatchUpdateStores, make(map[int]int))
		BatchUpdateLocks = append(BatchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			BatchUpdate()
		}
	})
}

func AddNewRecord(type_ int, id int, value int) {
	BatchUpdateLocks[type_].Lock()
	defer BatchUpdateLocks[type_].Unlock()
	if _, ok := BatchUpdateStores[type_][id]; !ok {
		BatchUpdateStores[type_][id] = value
	} else {
		BatchUpdateStores[type_][id] += value
	}
}

func BatchUpdate() {
	// check if there's any data to update
	hasData := false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		BatchUpdateLocks[i].Lock()
		if len(BatchUpdateStores[i]) > 0 {
			hasData = true
			BatchUpdateLocks[i].Unlock()
			break
		}
		BatchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog("batch update started")
	stores := make([]map[int]int, BatchUpdateTypeCount)
	for i := 0; i < BatchUpdateTypeCount; i++ {
		BatchUpdateLocks[i].Lock()
		stores[i] = BatchUpdateStores[i]
		BatchUpdateStores[i] = make(map[int]int)
		BatchUpdateLocks[i].Unlock()
	}

	updater := BatchUpdaterOf()
	if updater == nil {
		common.SysLog("batch update skipped: no BatchUpdater registered")
	} else {
		for i, store := range stores {
			if i == BatchUpdateTypeUserQuota || i == BatchUpdateTypeUsedQuota || i == BatchUpdateTypeRequestCount {
				continue
			}
			for key, value := range store {
				switch i {
				case BatchUpdateTypeTokenQuota:
					if err := updater.IncreaseTokenQuota(key, value); err != nil {
						common.SysLog("failed to batch update token quota: " + err.Error())
					}
				case BatchUpdateTypeChannelUsedQuota:
					updater.UpdateChannelUsedQuota(key, value)
				}
			}
		}

		userQuotaStore := stores[BatchUpdateTypeUserQuota]
		usedQuotaStore := stores[BatchUpdateTypeUsedQuota]
		requestCountStore := stores[BatchUpdateTypeRequestCount]

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
			if err := updater.UpdateUserQuotaUsedQuotaAndRequestCount(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key]); err != nil {
				common.SysLog("failed to batch update user quota: " + err.Error())
			}
		}
	}
	common.SysLog("batch update finished")
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func ShouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
