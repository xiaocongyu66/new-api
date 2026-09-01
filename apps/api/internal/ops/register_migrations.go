package ops

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
)

// Records owned by this domain register themselves for AutoMigrate.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &dbinfra.Setup{}, Name: "Setup"},
		dbx.Migration{Model: &SystemInstance{}, Name: "SystemInstance"},
		dbx.Migration{Model: &SystemTask{}, Name: "SystemTask"},
		dbx.Migration{Model: &SystemTaskLock{}, Name: "SystemTaskLock"},
	)
}
