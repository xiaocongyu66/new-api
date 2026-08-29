package ops

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Setup{}, Name: "Setup"},
		dbx.Migration{Model: &SystemInstance{}, Name: "SystemInstance"},
		dbx.Migration{Model: &SystemTask{}, Name: "SystemTask"},
		dbx.Migration{Model: &SystemTaskLock{}, Name: "SystemTaskLock"},
	)
}
