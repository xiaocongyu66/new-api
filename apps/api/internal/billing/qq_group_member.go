package billing

import (
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// 成员状态机：active（在群且未警告）-> warned（已发警告，宽限期内）->
// removed（已踢出）-> exempt（管理员/机器人/白名单，永不清理）
const (
	MemberStatusActive  = "active"
	MemberStatusWarned  = "warned"
	MemberStatusRemoved = "removed"
	MemberStatusExempt  = "exempt"
)

// QQGroupMember 群成员活跃档案
//
// 官方 bot 只能通过 webhook 事件观察群成员的发言，没有拉取成员列表的
// 权限（GET /v2/groups/{id}/members 返回 11253 白名单限制），所以
// 「在群里的是谁」只能靠事件累积。每条 GROUP_MESSAGE_CREATE 命中时
// upsert 本表，记录最后发言时间；长期不更新的行即为潜水候选。
type QQGroupMember struct {
	Id           int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupOpenID  string `json:"group_open_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_qq_group_member"`
	MemberOpenID string `json:"member_open_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_qq_group_member"`
	Username     string `json:"username" gorm:"type:varchar(255)"`
	// QQNumber 为 NapCat 侧桥接得到的真实 QQ 号；未桥接前为 0。
	// 官方 webhook 拿不到真实 QQ 号，只有 member_openid。
	QQNumber     int64 `json:"qq_number" gorm:"default:0"`
	LastActiveAt int64 `json:"last_active_at" gorm:"bigint;not null"`
	// WarnedAt 最近一次发送清理警告的时间，0 表示未警告
	WarnedAt  int64  `json:"warned_at" gorm:"bigint;default:0"`
	Status    string `json:"status" gorm:"type:varchar(16);not null;default:active;index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

func (QQGroupMember) TableName() string {
	return "qq_group_members"
}

// TouchGroupMember 记录一次群成员发言，不存在则建档。
//
// 写入走「更新优先」的乐观路径：绝大多数消息命中的是已有行，一次
// Updates 即可；只有首条消息才 Create，避免每条消息都走插入 + 唯一
// 索引冲突的重试。状态为 removed 的成员重新发言时恢复为 active，
// 因为此时他确实又在群里活跃了。
func TouchGroupMember(groupOpenID, memberOpenID, username string, ts int64) error {
	if groupOpenID == "" || memberOpenID == "" {
		return nil
	}
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	updates := map[string]any{
		"last_active_at": ts,
		"updated_at":     time.Now().Unix(),
	}
	if username != "" {
		updates["username"] = username
	}
	// 已移除的成员再次发言，说明又变活跃，回到 active
	updates["status"] = MemberStatusActive

	res := dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND member_open_id = ?", groupOpenID, memberOpenID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}

	// 首次出现的成员；并发时唯一索引会让后写者失败，忽略即可
	member := &QQGroupMember{
		GroupOpenID:  groupOpenID,
		MemberOpenID: memberOpenID,
		Username:     username,
		LastActiveAt: ts,
		Status:       MemberStatusActive,
		CreatedAt:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}
	if err := dbx.DB.Create(member).Error; err != nil {
		// 并发去重：另一个 goroutine 已建好，回退到更新
		return dbx.DB.Model(&QQGroupMember{}).
			Where("group_open_id = ? AND member_open_id = ?", groupOpenID, memberOpenID).
			Updates(updates).Error
	}
	return nil
}

// ListInactiveMembers 列出指定群内超过阈值未发言且未被豁免的成员。
//
// 只查 active 与 warned 两种状态：removed 的成员已经不在群里，
// exempt 的成员按策略永不清理。结果按最后活跃时间升序，最老的潜水
// 户排在最前面，方便分批处理时优先清理。
func ListInactiveMembers(groupOpenID string, beforeTs int64, limit int) ([]QQGroupMember, error) {
	var members []QQGroupMember
	q := dbx.DB.Where(
		"group_open_id = ? AND last_active_at < ? AND status IN ?",
		groupOpenID, beforeTs, []string{MemberStatusActive, MemberStatusWarned},
	).Order("last_active_at ASC")
	// GORM 的 Limit(0) 生成的是 LIMIT 0（返回空集）而不是「不限制」，
	// 这里的 0 含义是不限制条数，必须只在指定了正数上限时才追加 Limit。
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&members).Error
	return members, err
}

// SetMemberWarned 标记成员已发送清理警告。
func SetMemberWarned(groupOpenID, memberOpenID string, warnedAt int64) error {
	return dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND member_open_id = ?", groupOpenID, memberOpenID).
		Updates(map[string]any{
			"warned_at":  warnedAt,
			"status":     MemberStatusWarned,
			"updated_at": time.Now().Unix(),
		}).Error
}

// SetMemberRemoved 标记成员已被踢出。
func SetMemberRemoved(groupOpenID, memberOpenID string) error {
	return dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND member_open_id = ?", groupOpenID, memberOpenID).
		Updates(map[string]any{
			"status":     MemberStatusRemoved,
			"updated_at": time.Now().Unix(),
		}).Error
}

// SetMemberQQNumber 记录 @ 桥接得到的真实 QQ 号。
func SetMemberQQNumber(groupOpenID, memberOpenID string, qqNumber int64) error {
	return dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND member_open_id = ?", groupOpenID, memberOpenID).
		Updates(map[string]any{
			"qq_number":  qqNumber,
			"updated_at": time.Now().Unix(),
		}).Error
}

// GetGroupMemberStats 返回群里各状态的成员计数，供管理台展示。
func GetGroupMemberStats(groupOpenID string, beforeTs int64) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	err := dbx.DB.Model(&QQGroupMember{}).
		Select("status, count(*) as count").
		Where("group_open_id = ?", groupOpenID).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	stats := make(map[string]int64)
	for _, r := range rows {
		stats[r.Status] = r.Count
	}
	// 单独统计潜水候选：活跃但超过阈值未发言
	var inactive int64
	if err := dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND last_active_at < ? AND status = ?",
			groupOpenID, beforeTs, MemberStatusActive).
		Count(&inactive).Error; err != nil {
		return nil, err
	}
	stats["inactive_candidate"] = inactive
	return stats, nil
}
