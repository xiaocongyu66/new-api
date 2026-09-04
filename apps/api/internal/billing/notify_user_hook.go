package billing

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// OnNotifyUser delivers a user notification. The ops domain owns notification
// delivery and imports this package for the payment-compliance and pricing-sync
// settings, so it registers the sender from its own init() instead of this
// domain importing ops. Unregistered means no notification was sent, which the
// quota-notify callers already tolerate (they only log a delivery failure).
var OnNotifyUser func(userId int, userEmail string, userSetting dto.UserSetting, data dto.Notify) error

func notifyUser(userId int, userEmail string, userSetting dto.UserSetting, data dto.Notify) error {
	if OnNotifyUser == nil {
		return nil
	}
	return OnNotifyUser(userId, userEmail, userSetting, data)
}
