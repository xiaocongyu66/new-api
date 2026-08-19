package model

import (
	rootmodel "github.com/QuantumNous/new-api/model"
)

func init() {
	rootmodel.RegisterEntities(
		&Redemption{},
		&TopUp{},
		&Checkin{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&SubscriptionPlan{},
	)
}
