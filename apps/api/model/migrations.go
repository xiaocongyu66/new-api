package model

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this package register themselves for AutoMigrate here, so the
// bootstrap no longer needs a hardcoded list naming every domain's types. A
// record moving into its own domain takes its registration with it.
//
// Registration order is preserved and matches the previous literal list.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Channel{}, Name: "Channel"},
		dbx.Migration{Model: &Option{}, Name: "Option"},
		dbx.Migration{Model: &Redemption{}, Name: "Redemption"},
		dbx.Migration{Model: &Ability{}, Name: "Ability"},
		dbx.Migration{Model: &Log{}, Name: "Log"},
		dbx.Migration{Model: &Midjourney{}, Name: "Midjourney"},
		dbx.Migration{Model: &TopUp{}, Name: "TopUp"},
		dbx.Migration{Model: &QuotaData{}, Name: "QuotaData"},
		dbx.Migration{Model: &Task{}, Name: "Task"},
		dbx.Migration{Model: &Model{}, Name: "Model"},
		dbx.Migration{Model: &Vendor{}, Name: "Vendor"},
		dbx.Migration{Model: &PrefillGroup{}, Name: "PrefillGroup"},
		dbx.Migration{Model: &Setup{}, Name: "Setup"},
		dbx.Migration{Model: &Checkin{}, Name: "Checkin"},
		dbx.Migration{Model: &SubscriptionOrder{}, Name: "SubscriptionOrder"},
		dbx.Migration{Model: &UserSubscription{}, Name: "UserSubscription"},
		dbx.Migration{Model: &SubscriptionPreConsumeRecord{}, Name: "SubscriptionPreConsumeRecord"},
		dbx.Migration{Model: &PerfMetric{}, Name: "PerfMetric"},
		dbx.Migration{Model: &SystemInstance{}, Name: "SystemInstance"},
		dbx.Migration{Model: &SystemTask{}, Name: "SystemTask"},
		dbx.Migration{Model: &SystemTaskLock{}, Name: "SystemTaskLock"},
		dbx.Migration{Model: &ProxyNode{}, Name: "ProxyNode"},
		dbx.Migration{Model: &GatewayConfigRevision{}, Name: "GatewayConfigRevision"},
		dbx.Migration{Model: &GatewayConfigOutbox{}, Name: "GatewayConfigOutbox"},
		dbx.Migration{Model: &ChannelModelHealth{}, Name: "ChannelModelHealth"},
	)
	dbx.RegisterLogMigrations(dbx.Migration{Model: &Log{}, Name: "Log"})
}

// migrationModels flattens the registered set for AutoMigrate, which takes
// variadic models rather than a slice of pairs.
func migrationModels() []any {
	registered := dbx.Migrations()
	models := make([]any, 0, len(registered))
	for _, migration := range registered {
		models = append(models, migration.Model)
	}
	return models
}

// logMigrationModels does the same for the log database.
func logMigrationModels() []any {
	registered := dbx.LogMigrations()
	models := make([]any, 0, len(registered))
	for _, migration := range registered {
		models = append(models, migration.Model)
	}
	return models
}
