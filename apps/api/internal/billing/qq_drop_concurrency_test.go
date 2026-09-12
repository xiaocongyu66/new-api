package billing

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkMessageSeenConcurrent 事件按 goroutine 并发派发（manage_qqbot.go 的
// go handleQQBotEvent），markMessageSeen 必须在共享状态上加锁，
// 否则就是 concurrent map read and map write 级别的进程崩溃。
// 用 go test -race 执行时，未加锁的实现会被竞态检测器直接钉死。
func TestMarkMessageSeenConcurrent(t *testing.T) {
	const workers = 16

	var wg sync.WaitGroup
	gate := make(chan struct{})
	dup := make([]bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-gate
			// 先写入各自唯一 ID，制造并发写 map/slice 的窗口
			markMessageSeen(fmt.Sprintf("concurrent-unique-%d", idx))
			dup[idx] = markMessageSeen("concurrent-shared")
		}(i)
	}
	close(gate)
	wg.Wait()

	dups := 0
	for _, seen := range dup {
		if seen {
			dups++
		}
	}
	assert.Equal(t, workers-1, dups, "共享 ID 只允许第一次未见过")
	for i := 0; i < workers; i++ {
		assert.True(t, markMessageSeen(fmt.Sprintf("concurrent-unique-%d", i)),
			"各 worker 写入的唯一 ID 应已记录")
	}
}

// TestAwardQQDropDailyLimitConcurrent 同一用户并发触发掉落时，
// 「先计数后插入」必须原子：N 个 worker 抢 dailyLimit 次名额，
// 成功发放的次数不允许超过上限，且余额增量与发放次数严格一致。
func TestAwardQQDropDailyLimitConcurrent(t *testing.T) {
	require.NoError(t, dbx.DB.AutoMigrate(&QQDrop{}))
	user := &identity.User{
		Username: "drop-race-user",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		// users.aff_code 有唯一索引，共享测试库里必须给独立值，
		// 空串会撞掉后续以默认 aff_code 建号的用例。
		AffCode: "drop-race-aff",
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	const (
		limit   = 3
		workers = 10
		quota   = 100
	)

	var awarded, limited int64
	otherErrs := make(chan error, workers)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := AwardQQDrop(user.Id, "openid-race", "group-race", quota, limit)
			switch {
			case err == nil:
				atomic.AddInt64(&awarded, 1)
			case IsQQDropLimitReached(err):
				atomic.AddInt64(&limited, 1)
			default:
				otherErrs <- err
			}
		}()
	}
	close(gate)
	wg.Wait()
	close(otherErrs)
	for err := range otherErrs {
		require.NoError(t, err, "并发发放不应产生上限以外的错误")
	}

	assert.Equal(t, int64(limit), awarded, "成功发放次数必须恰好等于上限")
	assert.Equal(t, int64(workers-limit), limited, "其余请求必须全部收到上限拒绝")

	var drops int64
	require.NoError(t, dbx.DB.Model(&QQDrop{}).
		Where("user_id = ?", user.Id).Count(&drops).Error)
	assert.Equal(t, int64(limit), drops, "掉落记录不得多于上限")

	var got identity.User
	require.NoError(t, dbx.DB.First(&got, user.Id).Error)
	assert.Equal(t, limit*quota, got.Quota, "余额增量必须与发放次数一一对应")
}
