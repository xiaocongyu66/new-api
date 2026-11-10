package billing

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/settlecore"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// FundingSource — 资金来源接口（钱包 or 订阅）
// ---------------------------------------------------------------------------

// FundingSource 抽象了预扣费的资金来源。
type FundingSource interface {
	// Source 返回资金来源标识："wallet" 或 "subscription"
	Source() string
	// PreConsume 从该资金来源预扣 amount 额度
	PreConsume(amount int) error
	// Settle 根据差额调整资金来源（正数补扣，负数退还）
	Settle(delta int) error
	// Refund 退还所有预扣费
	Refund() error
}

// ---------------------------------------------------------------------------
// WalletFunding — 钱包资金来源实现
// ---------------------------------------------------------------------------

// ErrInsufficientWalletQuota 钱包原子预扣失败（余额不足），未发生任何扣减。
// BillingSession 据此映射为 ErrorCodeInsufficientUserQuota，
// 使 wallet_first 等计费偏好可以回退到订阅。
var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

type WalletFunding struct {
	userId   int
	consumed int // 实际预扣的用户额度
}

func (w *WalletFunding) Source() string { return settlecore.BillingSourceWallet }

func (w *WalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	reserved, err := TryReserveUserQuota(w.userId, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	w.consumed = amount
	return nil
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return identity.DecreaseUserQuota(w.userId, delta, false)
	}
	return identity.IncreaseUserQuota(w.userId, -delta, false)
}

func (w *WalletFunding) Refund() error {
	if w.consumed <= 0 {
		return nil
	}
	// IncreaseUserQuota 是 quota += N 的非幂等操作，不能重试，否则会多退额度。
	// 订阅的 RefundSubscriptionPreConsume 有 requestId 幂等保护所以可以重试。
	return identity.IncreaseUserQuota(w.userId, w.consumed, false)
}

// ---------------------------------------------------------------------------
// SubscriptionFunding — 订阅资金来源实现
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId      string
	userId         int
	usingGroup     string
	modelName      string
	amount         int64 // 预扣的订阅额度（subConsume）
	subscriptionId int
	preConsumed    int64
	// 以下字段在 PreConsume 成功后填充，供 RelayInfo 同步使用
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
	// WalletOverflow 记录交付后结算时超出订阅剩余额度、被记入钱包欠费的部分。
	WalletOverflow int64
	// requestCounterUsedBefore 非 0 表示该请求由 request 型订阅服务并已预留
	// 一次计数（计数是准入与展示账本，扣费走标准额度腿）。
	requestCounterUsedBefore int64
	// counterId 是预扣命中的权益行（scoped 订阅），0 = 走共享额度池。
	counterId int
	// requestKind 标记本次请求由 request 型订阅服务（计数腿已预留）。
	requestKind bool
}

func (s *SubscriptionFunding) Source() string { return settlecore.BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(_ int) error {
	// amount 参数被忽略，使用内部 s.amount（已在构造时根据 preConsumedQuota 计算）
	res, err := PreConsumeUserSubscription(s.requestId, s.userId, s.usingGroup, s.modelName, 0, s.amount)
	if err != nil {
		return err
	}
	s.subscriptionId = res.UserSubscriptionId
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	s.requestCounterUsedBefore = res.RequestCounterUsedBefore
	s.counterId = res.RequestCounterId
	s.requestKind = res.RequestKind
	if s.requestKind {
		if err := AttributeSubscriptionCounterQuota(s.requestId, res.PreConsumed); err != nil {
			common.SysLog(fmt.Sprintf("request counter pre-consume attribution failed (requestId=%s): %s", s.requestId, err.Error()))
		}
	}
	// 获取订阅计划信息
	if planInfo, err := GetSubscriptionPlanInfoByUserSubscriptionId(res.UserSubscriptionId); err == nil && planInfo != nil {
		s.PlanId = planInfo.PlanId
		s.PlanTitle = planInfo.PlanTitle
	}
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	// Quota-measured entitlement rows are the pool themselves: settle against
	// the row before touching the shared legs.
	if handled, err := s.settleQuotaCounter(delta); err != nil {
		return err
	} else if handled {
		return nil
	}
	if delta < 0 {
		if err := PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta)); err != nil {
			return err
		}
		// Settlement returned quota to the pool; un-attribute it from the
		// request counter this request reserved (request-kind rows only).
		return AttributeSubscriptionCounterQuota(s.requestId, int64(delta))
	}
	// 交付完成后的结算补扣:订阅先吃满剩余额度,装不下的溢出部分记入
	// 钱包欠费(余额可为负),与 WalletFunding.Settle 的欠费语义一致。
	// 严格版整笔拒绝只属于交付前的预扣/追加预扣,否则实际用量会被
	// 静默免单。
	overflow, err := PostConsumeUserSubscriptionDeltaCapped(s.subscriptionId, int64(delta))
	if err != nil {
		return err
	}
	// The portion the subscription absorbed is attributed to this request's
	// counter (request-kind rows only; amount-kind records are a no-op).
	if absorbed := int64(delta) - overflow; absorbed > 0 {
		if err := AttributeSubscriptionCounterQuota(s.requestId, absorbed); err != nil {
			common.SysLog(fmt.Sprintf("request counter quota attribution failed (requestId=%s): %s", s.requestId, err.Error()))
		}
	}
	s.WalletOverflow = overflow
	if overflow > 0 {
		common.SysError(fmt.Sprintf(
			"subscription settle overage charged to wallet: userId=%d subscriptionId=%d overflow=%d",
			s.userId, s.subscriptionId, overflow,
		))
		return identity.DecreaseUserQuota(s.userId, int(overflow), false)
	}
	return nil
}

// settleQuotaCounter settles against a quota-measured entitlement row, which
// is the pool for quota-scoped plans: the delta lands on the row capped by
// what it has left, and only the overflow reaches the wallet. Count rows keep
// the standard amount legs, so the helper reports handled=false for them.
func (s *SubscriptionFunding) settleQuotaCounter(delta int) (bool, error) {
	if s.counterId <= 0 {
		return false, nil
	}
	// The pre-consume record holds the release marker: waive/refund zero the
	// counter id, so a settled delta must not touch the row a second time.
	var record SubscriptionPreConsumeRecord
	if err := dbx.DB.Where("request_id = ?", s.requestId).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if record.RequestCounterId == 0 {
		return true, nil
	}
	var probe SubscriptionRequestCounter
	if err := dbx.DB.Where("id = ?", s.counterId).First(&probe).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if probe.Measure != SubscriptionMeasureQuota {
		return false, nil
	}
	var overflow int64
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var row SubscriptionRequestCounter
		if err := dbx.LockForUpdate(tx).Where("id = ?", s.counterId).First(&row).Error; err != nil {
			return err
		}
		if delta < 0 {
			release := -int64(delta)
			if release > row.Used {
				release = row.Used
			}
			row.Used -= release
			return tx.Save(&row).Error
		}
		absorbed := int64(delta)
		if row.Total > 0 {
			if remain := row.Total - row.Used; absorbed > remain {
				absorbed = remain
			}
		}
		row.Used += absorbed
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		overflow = int64(delta) - absorbed
		return nil
	})
	if err != nil {
		return true, err
	}
	s.WalletOverflow = overflow
	if overflow > 0 {
		// Charge outside the row transaction: a second connection writing while
		// this transaction holds the row lock deadlocks on SQLite.
		common.SysError(fmt.Sprintf(
			"quota-counter settle overage charged to wallet: userId=%d subscriptionId=%d counter=%d overflow=%d",
			s.userId, s.subscriptionId, s.counterId, overflow,
		))
		if err := identity.DecreaseUserQuota(s.userId, int(overflow), false); err != nil {
			return true, err
		}
	}
	return true, nil
}

// ReserveExtra extends a funding reservation for an already-open request
// (tiered paid-group switch). Scoped plans reserve on their entitlement row so
// settlement sees the same budget; shared-pool plans keep the amount legs.
func (s *SubscriptionFunding) ReserveExtra(delta int) error {
	if delta <= 0 {
		return nil
	}
	if s.counterId > 0 {
		var probe SubscriptionRequestCounter
		if err := dbx.DB.Where("id = ?", s.counterId).First(&probe).Error; err == nil &&
			probe.Measure == SubscriptionMeasureQuota {
			return dbx.DB.Transaction(func(tx *gorm.DB) error {
				var row SubscriptionRequestCounter
				if err := dbx.LockForUpdate(tx).Where("id = ?", s.counterId).First(&row).Error; err != nil {
					return err
				}
				if row.Total > 0 && row.Used+int64(delta) > row.Total {
					return fmt.Errorf("entitlement row has %d left, need %d", row.Total-row.Used, delta)
				}
				row.Used += int64(delta)
				return tx.Save(&row).Error
			})
		}
	}
	return PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta))
}

// ReleaseExtra undoes ReserveExtra when the reserve is rolled back.
func (s *SubscriptionFunding) ReleaseExtra(delta int) error {
	if delta <= 0 {
		return nil
	}
	if s.counterId > 0 {
		var probe SubscriptionRequestCounter
		if err := dbx.DB.Where("id = ?", s.counterId).First(&probe).Error; err == nil &&
			probe.Measure == SubscriptionMeasureQuota {
			return dbx.DB.Transaction(func(tx *gorm.DB) error {
				var row SubscriptionRequestCounter
				if err := dbx.LockForUpdate(tx).Where("id = ?", s.counterId).First(&row).Error; err != nil {
					return err
				}
				release := int64(delta)
				if release > row.Used {
					release = row.Used
				}
				row.Used -= release
				return tx.Save(&row).Error
			})
		}
	}
	return PostConsumeUserSubscriptionDelta(s.subscriptionId, -int64(delta))
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 {
		return nil
	}
	return refundWithRetry(func() error {
		return RefundSubscriptionPreConsume(s.requestId)
	})
}

// refundWithRetry 尝试多次执行退款操作以提高成功率，只能用于基于事务的退款函数！！！！！！
// try to refund with retries, only for refund functions based on transactions!!!
func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
