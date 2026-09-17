package identity

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// QQBinding QQ openid 与 New API 用户 ID 的绑定关系
type QQBinding struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int    `json:"user_id" gorm:"not null;uniqueIndex:idx_qq_binding_user"`
	OpenID      string `json:"open_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_qq_binding_openid"`
	UnionOpenID string `json:"union_open_id" gorm:"type:varchar(128)"`
	Username    string `json:"username" gorm:"type:varchar(64)"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
}

func (QQBinding) TableName() string {
	return "qq_bindings"
}

// QQBindCode 用户在网页端生成的绑定验证码
type QQBindCode struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Code      string `json:"code" gorm:"type:varchar(16);not null;uniqueIndex:idx_qq_bind_code"`
	UserId    int    `json:"user_id" gorm:"not null;index"`
	ExpiredAt int64  `json:"expired_at" gorm:"bigint;index"`
	Used      bool   `json:"used" gorm:"default:false"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func (QQBindCode) TableName() string {
	return "qq_bind_codes"
}

// QQUnbindRecord 记录用户最后一次解绑 QQ 的时间，用于冷却期校验。
type QQUnbindRecord struct {
	UserId    int64 `json:"user_id" gorm:"primaryKey"`
	UnbindAt  int64 `json:"unbind_at" gorm:"bigint;not null"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
}

func (QQUnbindRecord) TableName() string {
	return "qq_unbind_records"
}

// RecordQQUnbind 记录用户解绑时间，供后续绑定校验冷却期。
func RecordQQUnbind(tx *gorm.DB, userId int) error {
	now := time.Now()
	return tx.Save(&QQUnbindRecord{
		UserId:    int64(userId),
		UnbindAt:  now.Unix(),
		CreatedAt: now.Unix(),
	}).Error
}

// GetQQUnbindRecord 查询用户最后一次解绑记录。
func GetQQUnbindRecord(userId int) (*QQUnbindRecord, error) {
	var record QQUnbindRecord
	err := dbx.DB.Where("user_id = ?", userId).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// QQBindCodeTTL 验证码有效期：五分钟
// 用户要在网页和 QQ 客户端之间来回切换才能完成绑定，3 分钟常常切不过来，
// 导致大量验证码未使用即过期（线上已有近 12% 的码因此作废）。
const QQBindCodeTTL = 5 * time.Minute

// bindCodeAlphabet 六位区分大小写的字母与数字
// 去掉容易混淆的字符（0/O/o、1/l/I）以降低误输入率
const bindCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"

// generateBindCode 生成六位随机验证码
func generateBindCode() (string, error) {
	var sb strings.Builder
	max := big.NewInt(int64(len(bindCodeAlphabet)))
	for i := 0; i < 6; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(bindCodeAlphabet[n.Int64()])
	}
	return sb.String(), nil
}

// GetQQBindingByUserId 根据用户 ID 查询绑定关系
func GetQQBindingByUserId(userId int) (*QQBinding, error) {
	var binding QQBinding
	err := dbx.DB.Where("user_id = ?", userId).First(&binding).Error
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// GetQQBindingByOpenID 根据 openid 查询绑定关系
func GetQQBindingByOpenID(openID string) (*QQBinding, error) {
	if openID == "" {
		return nil, errors.New("openid 为空")
	}
	var binding QQBinding
	err := dbx.DB.Where("open_id = ?", openID).First(&binding).Error
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// IsQQBoundByUserId 判断 userId 是否已绑定 QQ 账号
func IsQQBoundByUserId(userId int) (string, bool) {
	if userId <= 0 {
		return "", false
	}
	var openID string
	err := dbx.DB.Model(&QQBinding{}).Where("user_id = ?", userId).
		Select("open_id").Scan(&openID).Error
	if err != nil || openID == "" {
		return "", false
	}
	return openID, true
}

// IsQQBound 判断 openid 是否已绑定用户
func IsQQBound(openID string) (int, bool) {
	binding, err := GetQQBindingByOpenID(openID)
	if err != nil || binding == nil {
		return 0, false
	}
	return binding.UserId, true
}

// qqRebindCooldownSeconds 由测试或启动后的配置加载流程写入；<=0 表示关闭冷却。
var qqRebindCooldownSeconds int

// SetQQRebindCooldownSeconds 仅在测试或配置加载时调用，生产代码默认由
// qq_bot_setting.rebind_cooldown_seconds 通过 billing.GetQQBotSetting 同步。
func SetQQRebindCooldownSeconds(seconds int) {
	qqRebindCooldownSeconds = seconds
}

// RejectQQBindIfInCooldown 根据当前冷却配置拒绝绑定。
// 返回剩余等待秒数，0 表示不在冷却中可直接绑定。
func RejectQQBindIfInCooldown(userId int) (int64, error) {
	if qqRebindCooldownSeconds <= 0 {
		return 0, nil
	}
	record, err := GetQQUnbindRecord(userId)
	if err != nil || record == nil {
		return 0, nil
	}
	allowedAt := record.UnbindAt + int64(qqRebindCooldownSeconds)
	now := time.Now().Unix()
	if now < allowedAt {
		return allowedAt - now, errors.New("解绑后需等待冷却时间才能重新绑定")
	}
	return 0, nil
}

// CreateQQBindCode 为用户生成一个新的绑定验证码
// 同一用户重复调用会作废之前尚未使用的验证码
func CreateQQBindCode(userId int) (*QQBindCode, error) {
	// 已绑定的用户不再生成验证码
	if _, err := GetQQBindingByUserId(userId); err == nil {
		return nil, errors.New("当前账号已绑定 QQ，请先解绑")
	}
	if wait, err := RejectQQBindIfInCooldown(userId); err != nil {
		return nil, err
	} else if wait > 0 {
		return nil, errors.New("解绑后需等待冷却时间才能重新绑定")
	}

	// 作废该用户此前未使用的验证码
	dbx.DB.Model(&QQBindCode{}).
		Where("user_id = ? AND used = ?", userId, false).
		Update("used", true)

	now := time.Now()

	// 生成不冲突的验证码，最多重试 8 次
	var code string
	for i := 0; i < 8; i++ {
		candidate, err := generateBindCode()
		if err != nil {
			return nil, err
		}
		var count int64
		// 仅与尚在有效期内且未使用的验证码比较，过期码可以复用
		prefixed := "#" + candidate
		dbx.DB.Model(&QQBindCode{}).
			Where("code = ? AND used = ? AND expired_at > ?", prefixed, false, now.Unix()).
			Count(&count)
		if count == 0 {
			code = candidate
			break
		}
	}
	if code == "" {
		return nil, errors.New("验证码生成失败，请稍后重试")
	}

	bindCode := &QQBindCode{
		Code:      "#" + code,
		UserId:    userId,
		ExpiredAt: now.Add(QQBindCodeTTL).Unix(),
		Used:      false,
		CreatedAt: now.Unix(),
	}
	if err := dbx.DB.Create(bindCode).Error; err != nil {
		return nil, err
	}
	return bindCode, nil
}

// ConsumeQQBindCode 校验验证码并建立绑定关系
// 返回绑定成功的用户 ID
func ConsumeQQBindCode(code, openID, unionOpenID, username string) (int, error) {
	code = strings.TrimSpace(code)
	code = strings.TrimPrefix(code, "#")
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, errors.New("验证码为空")
	}
	code = "#" + code
	if openID == "" {
		return 0, errors.New("openid 为空")
	}

	// openid 已绑定则直接拒绝
	if userId, bound := IsQQBound(openID); bound {
		return userId, errors.New("该 QQ 已绑定其他账号")
	}

	var bindCode QQBindCode
	// 验证码区分大小写，使用二进制比较避免数据库大小写不敏感排序规则的影响
	err := dbx.DB.Where("code = ? AND used = ?", code, false).First(&bindCode).Error
	if err != nil {
		return 0, errors.New("验证码无效")
	}
	// 二次确认大小写完全一致
	if bindCode.Code != code {
		return 0, errors.New("验证码无效")
	}
	if time.Now().Unix() > bindCode.ExpiredAt {
		return 0, errors.New("验证码已过期，请重新获取")
	}

	// 目标用户是否已绑定其他 QQ
	if _, err := GetQQBindingByUserId(bindCode.UserId); err == nil {
		return 0, errors.New("该账号已绑定其他 QQ")
	}
	if wait, err := RejectQQBindIfInCooldown(bindCode.UserId); err != nil {
		return 0, err
	} else if wait > 0 {
		return 0, errors.New("解绑后需等待冷却时间才能重新绑定")
	}

	binding := &QQBinding{
		UserId:      bindCode.UserId,
		OpenID:      openID,
		UnionOpenID: unionOpenID,
		Username:    username,
		CreatedAt:   time.Now().Unix(),
	}
	if err := dbx.DB.Create(binding).Error; err != nil {
		return 0, errors.New("绑定失败，请稍后重试")
	}

	// 标记验证码已使用
	dbx.DB.Model(&QQBindCode{}).Where("id = ?", bindCode.Id).Update("used", true)

	return bindCode.UserId, nil
}

// DeleteQQBinding 解绑用户的 QQ，供用户自助解绑与管理员清除绑定调用。
//
// 连未使用的验证码一起作废：解绑后那些码还能被 ConsumeQQBindCode 重新绑回来
// （它只按 code 查，不看用户当前是否已解绑），管理员的清除绑定就白做了。
func DeleteQQBinding(userId int) error {
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		return DeleteQQBindingWithTx(tx, userId)
	})
}

// DeleteQQBindingWithTx 在给定事务内解绑用户的 QQ。
//
// 供用户删除/注销流程和 DeleteQQBinding 调用：qq_bindings.open_id 是唯一索引，绑定行不跟着
// 用户一起清掉，这个 QQ 号就永久占位，换新账号后再也绑不上（线上已发生）。
// 同时清掉该用户尚未使用的绑定验证码 —— ConsumeQQBindCode 只按 code 查，
// 不校验用户是否仍然存在，留着等于给已注销账号留了个可用的绑定凭证。
//
// 用 Unscoped 是因为这两张表都没有软删除字段，显式声明意图，避免日后
// 加上 gorm.DeletedAt 时这里悄悄退化成软删除、唯一索引继续被占。
func DeleteQQBindingWithTx(tx *gorm.DB, userId int) error {
	if userId == 0 {
		return errors.New("id 为空！")
	}
	if err := tx.Unscoped().Where("user_id = ?", userId).Delete(&QQBinding{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("user_id = ?", userId).Delete(&QQBindCode{}).Error; err != nil {
		return err
	}
	return RecordQQUnbind(tx, userId)
}

// CleanExpiredQQBindCodes 清理过期验证码，供定时任务调用
func CleanExpiredQQBindCodes() error {
	// 保留一天内的记录便于排查，更早的过期码直接删除
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	return dbx.DB.Where("expired_at < ?", cutoff).Delete(&QQBindCode{}).Error
}
