package billing

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// QQTransfer 群内用户之间的额度转账记录
//
// 手续费不进入任何账户，直接销毁：这是回收额度的通缩设计，
// 若要改成进站长账户，在 transferWithTransaction 里给目标用户加上 Fee 即可。
type QQTransfer struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	FromUserId   int    `json:"from_user_id" gorm:"not null;index:idx_qq_transfer_from_date,priority:1"`
	ToUserId     int    `json:"to_user_id" gorm:"not null;index:idx_qq_transfer_to_date,priority:1"`
	TransferDate string `json:"transfer_date" gorm:"type:varchar(10);not null;index:idx_qq_transfer_from_date,priority:2;index:idx_qq_transfer_to_date,priority:2"`

	// Amount 为发送方扣除的总额，Fee 为手续费，Received = Amount - Fee
	Amount   int `json:"amount" gorm:"not null"`
	Fee      int `json:"fee" gorm:"not null"`
	Received int `json:"received" gorm:"not null"`

	FromOpenID  string `json:"from_open_id" gorm:"type:varchar(128)"`
	ToOpenID    string `json:"to_open_id" gorm:"type:varchar(128)"`
	GroupOpenID string `json:"group_open_id" gorm:"type:varchar(128)"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index"`
}

func (QQTransfer) TableName() string {
	return "qq_transfers"
}

var (
	// ErrTransferLimitSelf 发起方今日次数已用完
	ErrTransferLimitSelf = errors.New("你今日的转账次数已用完")
	// ErrTransferLimitPeer 接收方今日次数已用完
	ErrTransferLimitPeer = errors.New("对方今日的转账次数已用完")
	// ErrTransferInsufficient 余额不足
	ErrTransferInsufficient = errors.New("余额不足")
	// ErrTransferSelf 不能转给自己
	ErrTransferSelf = errors.New("不能转账给自己")
)

// CountQQTransfersToday 统计用户今日参与的转账次数
//
// 收发双向都计入：A 转给 B，A 和 B 今天各消耗一次额度。
// 这样可以防止用户找一群小号做中转来绕过次数限制。
func CountQQTransfersToday(userId int) (int, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := dbx.DB.Model(&QQTransfer{}).
		Where("transfer_date = ? AND (from_user_id = ? OR to_user_id = ?)", today, userId, userId).
		Count(&count).Error
	return int(count), err
}

// countTransfersTodayTx 事务内统计，避免并发绕过次数上限
func countTransfersTodayTx(tx *gorm.DB, userId int, date string) (int, error) {
	var count int64
	err := tx.Model(&QQTransfer{}).
		Where("transfer_date = ? AND (from_user_id = ? OR to_user_id = ?)", date, userId, userId).
		Count(&count).Error
	return int(count), err
}

// GetUserQQTransfers 查询用户参与过的转账记录（收发合并，按时间倒序）
func GetUserQQTransfers(userId int, limit int) ([]QQTransfer, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []QQTransfer
	err := dbx.DB.Where("from_user_id = ? OR to_user_id = ?", userId, userId).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

// QQTransferParams 转账入参
type QQTransferParams struct {
	FromUserId  int
	ToUserId    int
	FromOpenID  string
	ToOpenID    string
	GroupOpenID string

	// Amount 发送方实际扣除的总额（含手续费）
	Amount int
	// Fee 手续费，由调用方按累进费率算好后传入
	Fee int
	// DailyLimit 每人每日可参与的转账次数，<=0 为不限
	DailyLimit int
}

// DoQQTransfer 执行一次转账
//
// 全流程放在一个事务里：次数校验、余额校验、双方额度变更、记录落库。
// 任一步失败整体回滚，不会出现「扣了没到账」或「刷穿次数上限」。
func DoQQTransfer(p *QQTransferParams) (*QQTransfer, error) {
	if p.FromUserId == p.ToUserId {
		return nil, ErrTransferSelf
	}
	if p.Amount <= 0 {
		return nil, errors.New("转账金额必须为正数")
	}
	if p.Fee < 0 || p.Fee >= p.Amount {
		return nil, errors.New("手续费计算异常")
	}

	transfer := &QQTransfer{
		FromUserId:   p.FromUserId,
		ToUserId:     p.ToUserId,
		TransferDate: time.Now().Format("2006-01-02"),
		Amount:       p.Amount,
		Fee:          p.Fee,
		Received:     p.Amount - p.Fee,
		FromOpenID:   p.FromOpenID,
		ToOpenID:     p.ToOpenID,
		GroupOpenID:  p.GroupOpenID,
		CreatedAt:    time.Now().Unix(),
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return transferWithoutTransaction(transfer, p)
	}
	return transferWithTransaction(transfer, p)
}

// transferWithTransaction MySQL / PostgreSQL 事务路径
func transferWithTransaction(transfer *QQTransfer, p *QQTransferParams) (*QQTransfer, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if p.DailyLimit > 0 {
			fromCount, err := countTransfersTodayTx(tx, p.FromUserId, transfer.TransferDate)
			if err != nil {
				return err
			}
			if fromCount >= p.DailyLimit {
				return ErrTransferLimitSelf
			}
			toCount, err := countTransfersTodayTx(tx, p.ToUserId, transfer.TransferDate)
			if err != nil {
				return err
			}
			if toCount >= p.DailyLimit {
				return ErrTransferLimitPeer
			}
		}

		// 条件更新 + RowsAffected 判定，把余额检查与扣减做成一个原子操作。
		// 先查余额再扣会留下竞态窗口，可能把余额扣成负数。
		res := tx.Model(&identity.User{}).
			Where("id = ? AND quota >= ?", p.FromUserId, p.Amount).
			Update("quota", gorm.Expr("quota - ?", p.Amount))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrTransferInsufficient
		}

		if err := tx.Model(&identity.User{}).Where("id = ?", p.ToUserId).
			Update("quota", gorm.Expr("quota + ?", transfer.Received)).Error; err != nil {
			return err
		}

		return tx.Create(transfer).Error
	})
	if err != nil {
		return nil, err
	}

	go func() {
		_ = quotacache.IncrUser(p.FromUserId, -int64(p.Amount))
		_ = quotacache.IncrUser(p.ToUserId, int64(transfer.Received))
	}()
	return transfer, nil
}

// transferWithoutTransaction SQLite 路径
//
// 没有事务保护，因此按「先扣后加」排序：中途失败时把已扣的额度退回，
// 宁可让发送方看到一次失败，也不能出现凭空增发。
func transferWithoutTransaction(transfer *QQTransfer, p *QQTransferParams) (*QQTransfer, error) {
	if p.DailyLimit > 0 {
		fromCount, err := CountQQTransfersToday(p.FromUserId)
		if err != nil {
			return nil, err
		}
		if fromCount >= p.DailyLimit {
			return nil, ErrTransferLimitSelf
		}
		toCount, err := CountQQTransfersToday(p.ToUserId)
		if err != nil {
			return nil, err
		}
		if toCount >= p.DailyLimit {
			return nil, ErrTransferLimitPeer
		}
	}

	res := dbx.DB.Model(&identity.User{}).
		Where("id = ? AND quota >= ?", p.FromUserId, p.Amount).
		Update("quota", gorm.Expr("quota - ?", p.Amount))
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTransferInsufficient
	}

	rollback := func() {
		_ = identity.IncreaseUserQuota(p.FromUserId, p.Amount, true)
	}

	if err := identity.IncreaseUserQuota(p.ToUserId, transfer.Received, true); err != nil {
		rollback()
		return nil, err
	}
	if err := dbx.DB.Create(transfer).Error; err != nil {
		// 记录写不进去就把双方额度都还原，避免出现无凭证的额度流动
		_ = identity.DecreaseUserQuota(p.ToUserId, transfer.Received, true)
		rollback()
		return nil, err
	}

	_ = quotacache.DecrUser(p.FromUserId, int64(p.Amount))
	_ = quotacache.IncrUser(p.ToUserId, int64(transfer.Received))
	return transfer, nil
}
