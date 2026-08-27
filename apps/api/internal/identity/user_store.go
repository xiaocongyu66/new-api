package identity

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
)

// resolveUserStoreSort mirrors model.resolveUserSortOptions for the store
// entry points below; the sort type and its validation stay in model so the
// GORM method can keep using the column whitelist.
func resolveUserStoreSort(sortOptions []model.UserSortOptions) model.UserSortOptions {
	if len(sortOptions) == 0 {
		return model.NewUserSortOptions("", "")
	}
	return sortOptions[0]
}

func listAllUsers(pageInfo *common.PageInfo, sortOptions ...model.UserSortOptions) (users []*model.User, total int64, err error) {
	// Start transaction
	tx := model.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get total count within transaction
	err = tx.Unscoped().Model(&model.User{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated users within same transaction
	order := resolveUserStoreSort(sortOptions)
	err = order.Apply(tx.Unscoped()).Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password", "access_token").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

func GetMaxUserId() int {
	var user model.User
	model.DB.Unscoped().Last(&user)
	return user.Id
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := model.User{Id: id}
	return user.Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := model.User{Id: id}
	return user.HardDelete()
}

func GetUserIdByAffCode(affCode string) (int, error) {
	if affCode == "" {
		return 0, errors.New("affCode 为空！")
	}
	var user model.User
	err := model.DB.Select("id").First(&user, "aff_code = ?", affCode).Error
	return user.Id, err
}

// CheckUserExistOrDeleted check if user exist or deleted, if not exist, return false, nil, if deleted or exist, return true, nil
func CheckUserExistOrDeleted(username string, email string) (bool, error) {
	var user model.User

	var err error
	email = model.NormalizeEmail(email)
	if email == "" {
		err = model.DB.Unscoped().First(&user, "username = ?", username).Error
	} else {
		err = model.DB.Unscoped().First(&user, "username = ? or LOWER(email) = ?", username, email).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// not exist, return false, nil
			return false, nil
		}
		// other error, return false, err
		return false, err
	}
	// exist, return true, nil
	return true, nil
}

// UpdateUserAccessToken rotates a dashboard personal access token without
// writing a stale user snapshot back over concurrently updated fields.
func UpdateUserAccessToken(id int, token string) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	result := model.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", token)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// userBindColumns 允许通过 UpdateUserBindColumn 更新的第三方账号绑定列白名单。
// 列名只可能来自代码内部的 provider 实现，白名单是防御纵深，不依赖调用方自律。
var userBindColumns = map[string]bool{
	"github_id":   true,
	"discord_id":  true,
	"oidc_id":     true,
	"linux_do_id": true,
	"wechat_id":   true,
}

// UpdateUserBindColumn 第三方账号绑定字段的专用更新。
// 绑定操作必须只写绑定列：若改为“读取完整用户 → 改一个字段 → 整体更新”，
// 读快照期间并发发生的封禁、降权或分组变更会被旧快照覆盖恢复。
// 角色、状态、分组只允许通过各自带锁/CAS 的专用方法修改。
func UpdateUserBindColumn(userId int, column string, value string) error {
	if userId <= 0 {
		return errors.New("id 为空！")
	}
	if !userBindColumns[column] {
		return fmt.Errorf("invalid user bind column: %s", column)
	}
	return model.DB.Model(&model.User{}).Where("id = ?", userId).Update(column, value).Error
}

func UpdateUserLastLoginAt(id int) {
	if err := model.DB.Model(&model.User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

func GetUniqueUserByEmail(email string) (*model.User, error) {
	email = model.NormalizeEmail(email)
	if email == "" {
		return nil, model.ErrEmailNotFound
	}
	var users []model.User
	if err := model.DB.Where("LOWER(email) = ?", email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, model.ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, model.ErrEmailAmbiguous
	}
}

func ResetUserPasswordByEmail(email string, password string) error {
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	user, err := GetUniqueUserByEmail(email)
	if err != nil {
		return err
	}
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	if err = model.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := model.IncrementUserAuthVersionWithTx(tx, user.Id); err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", user.Id).Update("password", hashedPassword).Error
	}); err != nil {
		return err
	}
	if err := model.PublishUserAuthCache(user.Id); err != nil {
		return err
	}
	_, err = model.RevokeAllUserSessions(user.Id, "password_reset")
	return err
}

func IsWeChatIdAlreadyTaken(wechatId string) bool {
	return model.DB.Unscoped().Where("wechat_id = ?", wechatId).Find(&model.User{}).RowsAffected == 1
}
