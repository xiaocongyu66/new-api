package service

import (
	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/egress/fetch_url"
)

func GetCallbackAddress() string {
	if pay_subscription.CustomCallbackAddress == "" {
		return fetch_url.ServerAddress
	}
	return pay_subscription.CustomCallbackAddress
}
