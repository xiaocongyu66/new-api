package usage

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate: a record
// moving into its own domain takes its registration with it.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Log{}, Name: "Log"},
		dbx.Migration{Model: &QuotaData{}, Name: "QuotaData"},
		dbx.Migration{Model: &UserInsightProfile{}, Name: "UserInsightProfile"},
		dbx.Migration{Model: &UserInsightSample{}, Name: "UserInsightSample"},
	)
	dbx.RegisterLogMigrations(dbx.Migration{Model: &Log{}, Name: "Log"})
}
