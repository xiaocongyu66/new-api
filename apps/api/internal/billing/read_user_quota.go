package billing

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/model"
)

func GetUserUsedQuota(id int) (quota int, err error) {
	err = dbx.DB.Model(&model.User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}
