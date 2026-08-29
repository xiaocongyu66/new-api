package task

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate: a record
// moving into its own domain takes its registration with it.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Midjourney{}, Name: "Midjourney"},
		dbx.Migration{Model: &Task{}, Name: "Task"},
	)
}
