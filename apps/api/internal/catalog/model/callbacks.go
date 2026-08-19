package model

import (
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func init() {
	model.SetGatewayRoutingMutator(func(fn func(tx *gorm.DB) error) error {
		_, err := MutateGatewayRouting(fn)
		return err
	})
	model.SetPricingCacheInvalidator(InvalidatePricingCache)
}
