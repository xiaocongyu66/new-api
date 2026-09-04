package dbx

import "sync"

// Migration pairs a GORM model with the name used in migration logs.
type Migration struct {
	Model any
	Name  string
}

var (
	migrationsMu   sync.Mutex
	mainMigrations []Migration
	logMigrations  []Migration
)

// RegisterMigrations adds models to the main-database AutoMigrate set.
//
// Registration inverts what used to be a single hardcoded list in the record
// package. That list named every domain's types, so it forced the bootstrap file
// to import all of them — which blocks moving any record into its own domain.
// Each domain now registers its own models from an init, and the bootstrap only
// asks for the accumulated set.
//
// Order is preserved, so a table whose migration must follow another can rely on
// registration order within the same init.
func RegisterMigrations(migrations ...Migration) {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	mainMigrations = append(mainMigrations, migrations...)
}

// RegisterLogMigrations adds models to the log-database AutoMigrate set, which
// may target a different engine than the main database.
func RegisterLogMigrations(migrations ...Migration) {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	logMigrations = append(logMigrations, migrations...)
}

// Migrations returns a copy of the registered main-database migrations.
func Migrations() []Migration {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	return append([]Migration(nil), mainMigrations...)
}

// LogMigrations returns a copy of the registered log-database migrations.
func LogMigrations() []Migration {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	return append([]Migration(nil), logMigrations...)
}

var postMigrations []func() error

// RegisterPostMigration adds a step that runs after AutoMigrate, for backfills a
// schema change alone cannot express (seeding a new non-null column from existing
// rows, for example). Steps run in registration order.
func RegisterPostMigration(steps ...func() error) {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	postMigrations = append(postMigrations, steps...)
}

// RunPostMigrations executes the registered backfills, stopping at the first
// error.
func RunPostMigrations() error {
	migrationsMu.Lock()
	steps := append([]func() error(nil), postMigrations...)
	migrationsMu.Unlock()
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
