package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"
	"gorm.io/gorm"
)

func init() {
	// billing_setting.* tiered config changes must invalidate the pricing
	// cache owned by this module; wired here to keep internal/settings free
	// of a dependency on model.
	settings.OnBillingSettingChanged = InvalidatePricingCache
}

// GatewayRoutingOptionKeys is deliberately explicit. New settings must be
// reviewed before they become part of the gateway snapshot contract.
var GatewayRoutingOptionKeys = settings.GatewayRoutingOptionKeys

func IsGatewayRoutingOptionKey(key string) bool {
	return settings.IsGatewayRoutingOptionKey(key)
}

func GatewayRoutingOptionKeyList() []string {
	return settings.GatewayRoutingOptionKeyList()
}

func UpdateOptionWithTx(tx *gorm.DB, key, value string) error {
	if tx == nil || strings.TrimSpace(key) == "" {
		return gorm.ErrInvalidDB
	}
	if err := validateOptionValue(key, value); err != nil {
		return err
	}
	option := Option{Key: key}
	if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
		return err
	}
	option.Value = value
	return tx.Save(&option).Error
}

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	settings.SeedOptionMap()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, _ := AllOption()
	for _, option := range options {
		err := settings.ApplyOption(option.Key, option.Value)
		if err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func validateOptionValue(key string, value string) error {
	return settings.ValidateOptionValue(key, value)
}

func UpdateOption(key string, value string) error {
	if err := validateOptionValue(key, value); err != nil {
		return err
	}
	if IsGatewayRoutingOptionKey(key) {
		if _, err := MutateGatewayRouting(func(tx *gorm.DB) error {
			return UpdateOptionWithTx(tx, key, value)
		}); err != nil {
			return err
		}
		return settings.ApplyOption(key, value)
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return UpdateOptionWithTx(tx, key, value)
	}); err != nil {
		return err
	}
	return settings.ApplyOption(key, value)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then dispatches them through settings.ApplyOption in one pass.
// If any DB write fails the whole transaction rolls back and no in-memory
// state is touched — safe for callers that must commit a set of related
// options atomically (e.g. payment gateway binding).
func UpdateOptionsBulk(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	for key, value := range values {
		if err := validateOptionValue(key, value); err != nil {
			return err
		}
	}
	mutate := func(tx *gorm.DB) error {
		for key, value := range values {
			if err := UpdateOptionWithTx(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	}
	if hasGatewayOption := func() bool {
		for key := range values {
			if IsGatewayRoutingOptionKey(key) {
				return true
			}
		}
		return false
	}(); hasGatewayOption {
		if _, err := MutateGatewayRouting(mutate); err != nil {
			return err
		}
	} else if err := DB.Transaction(mutate); err != nil {
		return err
	}
	for key, value := range values {
		if err := settings.ApplyOption(key, value); err != nil {
			return err
		}
	}
	return nil
}
