package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// KarmadaConfig stores the Karmada kubeconfig used by the admin-only Karmada
// proxy endpoints. The table holds a single row keyed by Id == 1; every
// successful POST /api/karmada/config upserts that row. The plaintext
// kubeconfig never leaves the controller layer — only the encrypted blob is
// persisted, and read endpoints never return it.
type KarmadaConfig struct {
	Id                  uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Server              string `json:"server" gorm:"size:512;not null"`
	EncryptedKubeconfig string `json:"-" gorm:"type:text;not null"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint"`
}

const karmadaConfigSingletonId uint = 1

// GetKarmadaConfig returns the singleton Karmada config row or (nil, nil)
// when nothing has been configured yet.
func GetKarmadaConfig() (*KarmadaConfig, error) {
	var cfg KarmadaConfig
	if err := DB.Where("id = ?", karmadaConfigSingletonId).First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

// SaveKarmadaConfig upserts the singleton Karmada config row. The caller is
// responsible for encrypting the kubeconfig (see common.EncryptSecret) before
// passing it in — this function does not look at the plaintext.
func SaveKarmadaConfig(server, encryptedKubeconfig string) error {
	now := time.Now().Unix()
	var existing KarmadaConfig
	err := DB.Where("id = ?", karmadaConfigSingletonId).First(&existing).Error
	if err == nil {
		existing.Server = server
		existing.EncryptedKubeconfig = encryptedKubeconfig
		existing.UpdatedAt = now
		return DB.Save(&existing).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cfg := KarmadaConfig{
		Id:                  karmadaConfigSingletonId,
		Server:              server,
		EncryptedKubeconfig: encryptedKubeconfig,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return DB.Save(&cfg).Error
}

// DeleteKarmadaConfig removes the singleton Karmada config row. Deleting a
// missing row is not an error.
func DeleteKarmadaConfig() error {
	return DB.Where("id = ?", karmadaConfigSingletonId).Delete(&KarmadaConfig{}).Error
}
