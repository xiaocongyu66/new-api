package billing

import (
	"github.com/QuantumNous/new-api/model"
)

func GetUserUsedQuota(id int) (quota int, err error) {
	err = model.DB.Model(&model.User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}
