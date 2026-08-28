package service

import (
	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	if pay_subscription.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return pay_subscription.CustomCallbackAddress
}
