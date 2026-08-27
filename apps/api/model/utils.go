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

// Each record file registers the flusher for the queue that writes its own rows,
// so no file needs another's unexported persistence helper. This one covers the
// channel queue; token and user live beside their records.
func init() {
	dbx.RegisterChannelUsedQuotaFlusher(func(deltas map[int]int) {
		for id, delta := range deltas {
			updateChannelUsedQuota(id, delta)
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
