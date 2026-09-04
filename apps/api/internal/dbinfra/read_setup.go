package dbinfra

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

const legacySetupVersion = "legacy-unknown"

type Setup struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Version       string `json:"version" gorm:"type:varchar(50);not null;default:legacy-unknown"`
	InitializedAt int64  `json:"initialized_at" gorm:"type:bigint;not null;default:0"`
}

func GetSetup() *Setup {
	var setup Setup
	err := dbx.DB.First(&setup).Error
	if err != nil {
		return nil
	}
	return &setup
}

// RepairLegacySetups gives rows from the former ops.Setup schema a nonzero
// migration timestamp; their original initialization time and app version were
// never persisted, so legacy-unknown remains an explicit marker.
func RepairLegacySetups() error {
	result := dbx.DB.Model(&Setup{}).
		Where("version = ? AND initialized_at = ?", legacySetupVersion, 0).
		Update("initialized_at", time.Now().Unix())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		common.SysError(fmt.Sprintf("repaired %d legacy setup record(s)", result.RowsAffected))
	}
	return nil
}
