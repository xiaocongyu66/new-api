package billing

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/usage"
	"gorm.io/gorm"
)

// 红包状态
const (
	RedPacketStatusActive   = 1 // 进行中
	RedPacketStatusFinished = 2 // 已抢完
	RedPacketStatusExpired  = 3 // 已过期（余额已退回发送者）
)

// QQRedPacket 群红包
//
// 采用「发红包时一次性扣款、抢红包时逐笔入账」的模型：
// 发出瞬间钱就离开发送者账户，避免发完红包再把余额花掉导致无法兑付。
// 过期未抢完的部分由 ExpireQQRedPackets 退回发送者。
type QQRedPacket struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	SenderUserId int    `json:"sender_user_id" gorm:"not null;index:idx_qq_rp_sender_date,priority:1"`
	SendDate     string `json:"send_date" gorm:"type:varchar(10);not null;index:idx_qq_rp_sender_date,priority:2"`

	GroupOpenID  string `json:"group_open_id" gorm:"type:varchar(128);index"`
	SenderOpenID string `json:"sender_open_id" gorm:"type:varchar(128)"`
	Blessing     string `json:"blessing" gorm:"type:varchar(128)"`

	TotalAmount int `json:"total_amount" gorm:"not null"` // 红包总额
	TotalCount  int `json:"total_count" gorm:"not null"`  // 红包个数

	// 剩余额度与剩余个数，抢一次递减一次
	RemainingAmount int `json:"remaining_amount" gorm:"not null"`
	RemainingCount  int `json:"remaining_count" gorm:"not null"`

	Status    int   `json:"status" gorm:"not null;default:1;index"`
	ExpireAt  int64 `json:"expire_at" gorm:"bigint;index"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
}

func (QQRedPacket) TableName() string {
	return "qq_red_packets"
}

// QQRedPacketGrab 抢红包记录
//
// (packet_id, user_id) 唯一索引是「每人每个红包只能抢一次」的唯一保证，
// 应用层的判重只是为了给出友好提示，真正防并发靠这个索引。
type QQRedPacketGrab struct {
	Id       int    `json:"id" gorm:"primaryKey;autoIncrement"`
	PacketId int    `json:"packet_id" gorm:"not null;uniqueIndex:idx_qq_rp_grab_unique,priority:1"`
	UserId   int    `json:"user_id" gorm:"not null;uniqueIndex:idx_qq_rp_grab_unique,priority:2"`
	OpenID   string `json:"open_id" gorm:"type:varchar(128)"`

	Amount    int   `json:"amount" gorm:"not null"`
	IsLuckic  bool  `json:"is_luckiest" gorm:"column:is_luckiest;default:false"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
}

func (QQRedPacketGrab) TableName() string {
	return "qq_red_packet_grabs"
}

var (
	// ErrRedPacketNotFound 红包不存在
	ErrRedPacketNotFound = errors.New("红包不存在")
	// ErrRedPacketFinished 红包已被抢完
	ErrRedPacketFinished = errors.New("红包已被抢完")
	// ErrRedPacketExpired 红包已过期
	ErrRedPacketExpired = errors.New("红包已过期")
	// ErrRedPacketAlreadyGrabbed 已经抢过了
	ErrRedPacketAlreadyGrabbed = errors.New("你已经抢过这个红包了")
	// ErrRedPacketOwnGrab 不能抢自己的红包
	ErrRedPacketOwnGrab = errors.New("不能抢自己发的红包")
	// ErrRedPacketInsufficient 发红包时余额不足
	ErrRedPacketInsufficient = errors.New("余额不足")
	// ErrRedPacketDailyLimit 今日发红包次数已用完
	ErrRedPacketDailyLimit = errors.New("今日发红包次数已用完")
)

// CountQQRedPacketsToday 统计用户今日发出的红包个数
func CountQQRedPacketsToday(userId int) (int, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := dbx.DB.Model(&QQRedPacket{}).
		Where("sender_user_id = ? AND send_date = ?", userId, today).
		Count(&count).Error
	return int(count), err
}

// GetQQRedPacket 按 ID 读取红包
func GetQQRedPacket(id int) (*QQRedPacket, error) {
	var packet QQRedPacket
	if err := dbx.DB.Where("id = ?", id).First(&packet).Error; err != nil {
		return nil, ErrRedPacketNotFound
	}
	return &packet, nil
}

// GetQQRedPacketGrabs 读取某个红包的全部抢取记录（按时间正序）
func GetQQRedPacketGrabs(packetId int) ([]QQRedPacketGrab, error) {
	var grabs []QQRedPacketGrab
	err := dbx.DB.Where("packet_id = ?", packetId).
		Order("created_at ASC, id ASC").
		Find(&grabs).Error
	return grabs, err
}

// QQRedPacketParams 发红包入参
type QQRedPacketParams struct {
	SenderUserId int
	SenderOpenID string
	GroupOpenID  string
	Blessing     string

	TotalAmount   int
	TotalCount    int
	ExpireSeconds int
	DailyLimit    int
}

// CreateQQRedPacket 发红包：扣除发送者额度并生成红包
func CreateQQRedPacket(p *QQRedPacketParams) (*QQRedPacket, error) {
	if p.TotalAmount <= 0 {
		return nil, errors.New("红包金额必须为正数")
	}
	if p.TotalCount <= 0 {
		return nil, errors.New("红包个数必须为正数")
	}
	// 每个红包至少要能分到 1 个额度，否则后面的拆分会出现 0 元红包
	if p.TotalAmount < p.TotalCount {
		return nil, errors.New("红包金额太小，不够分给这么多人")
	}

	expire := p.ExpireSeconds
	if expire <= 0 {
		expire = 24 * 3600
	}
	now := time.Now()

	packet := &QQRedPacket{
		SenderUserId:    p.SenderUserId,
		SendDate:        now.Format("2006-01-02"),
		GroupOpenID:     p.GroupOpenID,
		SenderOpenID:    p.SenderOpenID,
		Blessing:        p.Blessing,
		TotalAmount:     p.TotalAmount,
		TotalCount:      p.TotalCount,
		RemainingAmount: p.TotalAmount,
		RemainingCount:  p.TotalCount,
		Status:          RedPacketStatusActive,
		ExpireAt:        now.Add(time.Duration(expire) * time.Second).Unix(),
		CreatedAt:       now.Unix(),
	}

	create := func(tx *gorm.DB) error {
		if p.DailyLimit > 0 {
			var count int64
			if err := tx.Model(&QQRedPacket{}).
				Where("sender_user_id = ? AND send_date = ?", p.SenderUserId, packet.SendDate).
				Count(&count).Error; err != nil {
				return err
			}
			if int(count) >= p.DailyLimit {
				return ErrRedPacketDailyLimit
			}
		}

		// 条件更新保证余额检查与扣减原子完成，并发下扣不成负数
		res := tx.Model(&identity.User{}).
			Where("id = ? AND quota >= ?", p.SenderUserId, p.TotalAmount).
			Update("quota", gorm.Expr("quota - ?", p.TotalAmount))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrRedPacketInsufficient
		}
		return tx.Create(packet).Error
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := create(dbx.DB); err != nil {
			return nil, err
		}
	} else if err := dbx.DB.Transaction(create); err != nil {
		return nil, err
	}

	go func() {
		_ = quotacache.DecrUser(p.SenderUserId, int64(p.TotalAmount))
	}()
	return packet, nil
}

// splitRedPacketAmount 二倍均值法拆分单个红包
//
// 每次可抢区间为 [1, 剩余均值*2]，这样期望值恒等于剩余均值，
// 既保证随机性又不会出现前面的人把钱抢光、后面的人只能拿 1 的情况。
// 最后一个人直接拿走全部剩余，避免累计舍入误差留下零头。
func splitRedPacketAmount(remainingAmount, remainingCount int) int {
	if remainingCount <= 1 {
		return remainingAmount
	}
	// 给后面每人预留至少 1
	maxPick := remainingAmount - (remainingCount - 1)
	if maxPick < 1 {
		return 1
	}
	// 二倍均值上界
	avgTwice := remainingAmount / remainingCount * 2
	if avgTwice < 1 {
		avgTwice = 1
	}
	if maxPick > avgTwice {
		maxPick = avgTwice
	}
	if maxPick <= 1 {
		return 1
	}
	return 1 + rand.Intn(maxPick)
}

// GrabQQRedPacket 抢红包
//
// 返回抢到的记录与红包最新状态。allowOwnGrab 为 false 时禁止抢自己的红包。
func GrabQQRedPacket(packetId, userId int, openID string, allowOwnGrab bool) (*QQRedPacketGrab, *QQRedPacket, error) {
	var grab *QQRedPacketGrab
	var packet *QQRedPacket

	work := func(tx *gorm.DB) error {
		var p QQRedPacket
		q := dbx.LockForUpdate(tx).Where("id = ?", packetId)
		if err := q.First(&p).Error; err != nil {
			return ErrRedPacketNotFound
		}

		if !allowOwnGrab && p.SenderUserId == userId {
			return ErrRedPacketOwnGrab
		}
		if p.Status == RedPacketStatusExpired || time.Now().Unix() > p.ExpireAt {
			return ErrRedPacketExpired
		}
		if p.Status != RedPacketStatusActive || p.RemainingCount <= 0 {
			return ErrRedPacketFinished
		}

		var exists int64
		if err := tx.Model(&QQRedPacketGrab{}).
			Where("packet_id = ? AND user_id = ?", packetId, userId).
			Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			return ErrRedPacketAlreadyGrabbed
		}

		amount := splitRedPacketAmount(p.RemainingAmount, p.RemainingCount)
		g := &QQRedPacketGrab{
			PacketId:  packetId,
			UserId:    userId,
			OpenID:    openID,
			Amount:    amount,
			CreatedAt: time.Now().Unix(),
		}
		if err := tx.Create(g).Error; err != nil {
			// 唯一索引冲突说明并发下已经抢过一次
			return ErrRedPacketAlreadyGrabbed
		}

		p.RemainingAmount -= amount
		p.RemainingCount--
		if p.RemainingCount <= 0 {
			p.Status = RedPacketStatusFinished
		}
		if err := tx.Model(&QQRedPacket{}).Where("id = ?", packetId).
			Updates(map[string]any{
				"remaining_amount": p.RemainingAmount,
				"remaining_count":  p.RemainingCount,
				"status":           p.Status,
			}).Error; err != nil {
			return err
		}

		if err := tx.Model(&identity.User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", amount)).Error; err != nil {
			return err
		}

		grab = g
		packet = &p
		return nil
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := work(dbx.DB); err != nil {
			return nil, nil, err
		}
	} else if err := dbx.DB.Transaction(work); err != nil {
		return nil, nil, err
	}

	go func() {
		_ = quotacache.IncrUser(userId, int64(grab.Amount))
	}()
	return grab, packet, nil
}

// MarkLuckiestGrab 红包抢完后标记「运气王」（金额最大者）
func MarkLuckiestGrab(packetId int) error {
	grabs, err := GetQQRedPacketGrabs(packetId)
	if err != nil || len(grabs) == 0 {
		return err
	}
	best := grabs[0]
	for _, g := range grabs[1:] {
		if g.Amount > best.Amount {
			best = g
		}
	}
	return dbx.DB.Model(&QQRedPacketGrab{}).Where("id = ?", best.Id).
		Update("is_luckiest", true).Error
}

// ExpireQQRedPackets 处理过期红包：把未抢完的余额退回发送者
//
// 幂等：只处理 status=active 且已过期的记录，退款后置为 expired。
// 返回处理的红包数量。
func ExpireQQRedPackets() (int, error) {
	var packets []QQRedPacket
	now := time.Now().Unix()
	if err := dbx.DB.Where("status = ? AND expire_at < ?", RedPacketStatusActive, now).
		Limit(200).Find(&packets).Error; err != nil {
		return 0, err
	}

	handled := 0
	for i := range packets {
		p := packets[i]
		refund := p.RemainingAmount
		err := dbx.DB.Transaction(func(tx *gorm.DB) error {
			// 再次带 status 条件更新，避免与并发的抢红包/其他实例重复退款
			res := tx.Model(&QQRedPacket{}).
				Where("id = ? AND status = ?", p.Id, RedPacketStatusActive).
				Updates(map[string]any{"status": RedPacketStatusExpired})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // 已被其他流程处理
			}
			if refund <= 0 {
				return nil
			}
			return tx.Model(&identity.User{}).Where("id = ?", p.SenderUserId).
				Update("quota", gorm.Expr("quota + ?", refund)).Error
		})
		if err != nil {
			common.SysError("退回过期红包失败: " + err.Error())
			continue
		}
		if refund > 0 {
			_ = quotacache.IncrUser(p.SenderUserId, int64(refund))
			usage.RecordLog(p.SenderUserId, usage.LogTypeSystem,
				"红包过期，退回未领取的额度")
		}
		handled++
	}
	return handled, nil
}

// GetUserQQRedPacketStats 用户红包统计：发出笔数、发出总额、抢到笔数、抢到总额
func GetUserQQRedPacketStats(userId int) (sentCount int64, sentAmount int64, grabCount int64, grabAmount int64) {
	dbx.DB.Model(&QQRedPacket{}).Where("sender_user_id = ?", userId).Count(&sentCount)
	dbx.DB.Model(&QQRedPacket{}).Where("sender_user_id = ?", userId).
		Select("COALESCE(SUM(total_amount), 0)").Scan(&sentAmount)
	dbx.DB.Model(&QQRedPacketGrab{}).Where("user_id = ?", userId).Count(&grabCount)
	dbx.DB.Model(&QQRedPacketGrab{}).Where("user_id = ?", userId).
		Select("COALESCE(SUM(amount), 0)").Scan(&grabAmount)
	return
}

// MaintainQQRedPackets 后台循环：定期退回过期红包
//
// 每分钟扫一次。ExpireQQRedPackets 内部用带 status 条件的更新保证幂等，
// 多实例同时跑也不会重复退款。
func MaintainQQRedPackets() {
	for {
		time.Sleep(time.Minute)
		if n, err := ExpireQQRedPackets(); err != nil {
			common.SysError("处理过期红包失败: " + err.Error())
		} else if n > 0 {
			common.SysLog(fmt.Sprintf("已退回 %d 个过期红包", n))
		}
	}
}
