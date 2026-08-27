package model

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

type Setup struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Version       string `json:"version" gorm:"type:varchar(50);not null"`
	InitializedAt int64  `json:"initialized_at" gorm:"type:bigint;not null"`
}

func GetSetup() *Setup {
	var setup Setup
	err := dbx.DB.First(&setup).Error
	if err != nil {
		return nil
	}
	return &setup
}
