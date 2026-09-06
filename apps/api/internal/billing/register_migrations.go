package billing

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate: a record
// moving into its own domain takes its registration with it.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Redemption{}, Name: "Redemption"},
		dbx.Migration{Model: &TopUp{}, Name: "TopUp"},
		dbx.Migration{Model: &Checkin{}, Name: "Checkin"},
		dbx.Migration{Model: &SubscriptionOrder{}, Name: "SubscriptionOrder"},
		dbx.Migration{Model: &UserSubscription{}, Name: "UserSubscription"},
		dbx.Migration{Model: &SubscriptionPreConsumeRecord{}, Name: "SubscriptionPreConsumeRecord"},
		dbx.Migration{Model: &QQCheckin{}, Name: "QQCheckin"},
		dbx.Migration{Model: &QQDrop{}, Name: "QQDrop"},
		dbx.Migration{Model: &QQTransfer{}, Name: "QQTransfer"},
		dbx.Migration{Model: &QQRedPacket{}, Name: "QQRedPacket"},
		dbx.Migration{Model: &QQRedPacketGrab{}, Name: "QQRedPacketGrab"},
	)
}
