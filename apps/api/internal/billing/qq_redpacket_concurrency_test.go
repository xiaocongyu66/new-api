package billing

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRedPacketConcurrencyTestDB opens an in-memory sqlite and migrates the red packet
// tables. Returns a cleanup func the caller should defer.
func setupRedPacketConcurrencyTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB, previousRedis := dbx.DB, dbx.LogDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&QQRedPacket{}, &QQRedPacketGrab{}, &identity.User{}))
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	return func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	}
}

// createTestUser creates a test user with prefunded quota.
func createTestUser(t *testing.T, id int, quota int) *identity.User {
	t.Helper()
	user := &identity.User{
		Id:       id,
		Username: "test-user",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	return user
}

// TestGrabQQRedPacketConcurrent 并发抢同一个红包，确保正确性：
// 成功抢到次数等于红包个数，总金额等于红包总额，失败请求收到 ErrRedPacketFinished，
// 红包最终状态为 RemainingAmount=0, RemainingCount=0, Status=Finished。
func TestGrabQQRedPacketConcurrent(t *testing.T) {
	defer setupRedPacketConcurrencyTestDB(t)()

	const totalAmount = 5000
	const totalCount = 8
	const workers = 16

	// 创建发送者并预存足够额度
	sender := createTestUser(t, 1, totalAmount)

	// 通过 CreateQQRedPacket 创建红包（会扣除发送者额度）
	packet, err := CreateQQRedPacket(&QQRedPacketParams{
		SenderUserId:  sender.Id,
		SenderOpenID:  "sender-openid",
		GroupOpenID:   "test-group",
		Blessing:      "测试红包",
		TotalAmount:   totalAmount,
		TotalCount:    totalCount,
		ExpireSeconds: 86400,
		DailyLimit:    -1, // 不限制每日发红包次数
	})
	require.NoError(t, err)

	var grabCount int64
	var failures int64

	var wg sync.WaitGroup
	gate := make(chan struct{})

	// Launch 16 workers with distinct user IDs
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerId int) {
			defer wg.Done()
			<-gate
			userId := workerId + 100 // distinct user ids starting from 100

			_, _, err := GrabQQRedPacket(packet.Id, userId, "worker-openid", false)
			if err != nil {
				// Expected errors: ErrRedPacketFinished (packet exhausted), ErrRedPacketAlreadyGrabbed (if same user retries)
				if err == ErrRedPacketFinished || err == ErrRedPacketAlreadyGrabbed {
					atomic.AddInt64(&failures, 1)
				} else {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				atomic.AddInt64(&grabCount, 1)
			}
		}(i)
	}
	close(gate)
	wg.Wait()

	// Verify counts
	assert.Equal(t, int64(totalCount), atomic.LoadInt64(&grabCount), "应该成功抢到 %d 次", totalCount)
	assert.Equal(t, int64(workers-totalCount), atomic.LoadInt64(&failures), "其余请求应全部收到红包抢完错误")

	// Verify final packet state
	var finalPacket QQRedPacket
	require.NoError(t, dbx.DB.First(&finalPacket, packet.Id).Error)
	assert.Equal(t, RedPacketStatusFinished, finalPacket.Status, "红包状态应为已完成")
	assert.Equal(t, 0, finalPacket.RemainingAmount, "剩余金额应为 0")
	assert.Equal(t, 0, finalPacket.RemainingCount, "剩余个数应为 0")

	// Verify total grabbed amount equals packet total
	var totalGrabbed int64
	require.NoError(t, dbx.DB.Model(&QQRedPacketGrab{}).
		Where("packet_id = ?", packet.Id).
		Select("COALESCE(SUM(amount), 0)").Scan(&totalGrabbed).Error)
	assert.Equal(t, int64(totalAmount), totalGrabbed, "抢到总额必须等于红包总额，不能超发")

	// Verify grab record count matches totalCount
	var grabCountDB int64
	require.NoError(t, dbx.DB.Model(&QQRedPacketGrab{}).
		Where("packet_id = ?", packet.Id).Count(&grabCountDB).Error)
	assert.Equal(t, int64(totalCount), grabCountDB, "数据库中应该有 %d 条抢红包记录", totalCount)

	// Verify sender quota was correctly deducted
	var finalSender identity.User
	require.NoError(t, dbx.DB.First(&finalSender, sender.Id).Error)
	assert.Equal(t, sender.Quota-totalAmount, finalSender.Quota, "发送者额度应该被正确扣除")
}

// TestCreateQQRedPacketConcurrent 并发发红包，确保每日限额和余额检查在 SQLite 下也是原子的。
func TestCreateQQRedPacketConcurrent(t *testing.T) {
	defer setupRedPacketConcurrencyTestDB(t)()

	const totalAmount = 1000
	const totalCount = 2
	const workers = 10
	const dailyLimit = 3

	// 创建发送者并预存足够额度
	sender := createTestUser(t, 999, workers*totalAmount)

	var successCount int64
	var limitErrors int64
	var otherErrors int64

	var wg sync.WaitGroup
	gate := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate

			_, err := CreateQQRedPacket(&QQRedPacketParams{
				SenderUserId:  sender.Id,
				SenderOpenID:  "sender-openid",
				GroupOpenID:   "test-group",
				Blessing:      "并发测试红包",
				TotalAmount:   totalAmount,
				TotalCount:    totalCount,
				ExpireSeconds: 3600,
				DailyLimit:    dailyLimit,
			})
			if err != nil {
				if err == ErrRedPacketDailyLimit {
					atomic.AddInt64(&limitErrors, 1)
				} else {
					atomic.AddInt64(&otherErrors, 1)
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}
	close(gate)
	wg.Wait()

	// Exactly dailyLimit packets should succeed
	assert.Equal(t, int64(dailyLimit), atomic.LoadInt64(&successCount), "恰好 %d 个红包应该创建成功", dailyLimit)
	assert.Equal(t, int64(workers-dailyLimit), atomic.LoadInt64(&limitErrors), "其余应收到每日上限错误")
	assert.Equal(t, int64(0), atomic.LoadInt64(&otherErrors), "不应出现其他错误")

	// Verify sender quota deduction: successCount * totalAmount
	var finalSender identity.User
	require.NoError(t, dbx.DB.First(&finalSender, sender.Id).Error)
	assert.Equal(t, sender.Quota-int(successCount)*totalAmount, finalSender.Quota, "发送者额度扣减应与成功创建数匹配")
}
