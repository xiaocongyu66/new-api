package dbinfra

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

func batchUpdate() {
	if !dbx.BatchQueuesPending() {
		return
	}
	common.SysLog("batch update started")
	dbx.FlushBatchQueues()
	common.SysLog("batch update finished")
}
