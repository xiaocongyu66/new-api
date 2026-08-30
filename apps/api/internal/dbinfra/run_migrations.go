package dbinfra

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this package register themselves for AutoMigrate here, so the
// bootstrap no longer needs a hardcoded list naming every domain's types. A
// record moving into its own domain takes its registration with it.
//
// Registration order is preserved and matches the previous literal list.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Option{}, Name: "Option"},
	)

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
