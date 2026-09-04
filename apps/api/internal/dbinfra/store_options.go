package dbinfra

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/settings"
	"gorm.io/gorm"
)

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
	err = dbx.DB.Find(&options).Error
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
		if MutateGatewayRoutingFn == nil {
			return errGatewayRoutingMutatorUnwired
		}
		if _, err := MutateGatewayRoutingFn(func(tx *gorm.DB) error {
			return UpdateOptionWithTx(tx, key, value)
		}); err != nil {
			return err
		}
		return settings.ApplyOption(key, value)
	}
	if err := dbx.DB.Transaction(func(tx *gorm.DB) error {
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
		if MutateGatewayRoutingFn == nil {
			return errGatewayRoutingMutatorUnwired
		}
		if _, err := MutateGatewayRoutingFn(mutate); err != nil {
			return err
		}
	} else if err := dbx.DB.Transaction(mutate); err != nil {
		return err
	}
	for key, value := range values {
		if err := settings.ApplyOption(key, value); err != nil {
			return err
		}
	}
	return nil
}

// MutateGatewayRoutingFn is wired by internal/catalog, which owns the gateway
// routing revision. dbinfra must not import catalog (catalog imports dbinfra),
// so the mutation entry point arrives as a hook at startup.
var MutateGatewayRoutingFn func(mutate func(tx *gorm.DB) error) (int64, error)

// errGatewayRoutingMutatorUnwired fails a gateway-routing option write that
// arrives before the hook is installed. Falling back to a plain write would
// persist the option without bumping the gateway config revision, so the
// published snapshot would keep serving the superseded routing.
var errGatewayRoutingMutatorUnwired = errors.New("gateway routing mutator not wired")
