package dbx

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
)

// legacyRouteSeedWeight is the static_weight a freshly expanded route unit
// starts at. It mirrors the catalog domain's seed constant: this package is
// database infrastructure and must not import a business domain, and the
// migration below deliberately addresses tables by name rather than by Go type
// (see columnExists for the same reasoning), so the value is restated here.
// Changing the catalog seed requires changing this constant with it.
const legacyRouteSeedWeight = 100

// DropLegacySchedulingColumns retires the pre-route-unit scheduling columns:
// channels.priority, channels.weight, abilities.priority and abilities.weight.
//
// It is exported and called from the bootstrap rather than self-registered
// through RegisterPostMigration, because its position is a hard ordering
// constraint that registration order cannot express: this package is imported BY
// catalog, so its init always runs first and would register this step ahead of
// the route seed it must follow.
//
// The required slot is after AutoMigrate and after SeedChannelModelRoutes:
//
//   - Reading the legacy column does not need to precede AutoMigrate. AutoMigrate
//     only adds columns and indexes, it never drops one, so a column the structs
//     no longer declare survives it untouched and stays readable by raw SQL —
//     which is how this migration reads it anyway (see columnExists).
//   - The carry-over positively REQUIRES the later slot. Its target,
//     channel_model_routes, is created by AutoMigrate and populated by the route
//     seed. Running earlier finds no rows to carry onto and silently drops the
//     operator's tuned weights.
//
// The carry-over runs first within this step, which is what makes the cleanup
// non-destructive: a tuned channel weight is recorded nowhere else, so it has to
// reach the route rows before the column holding it disappears.
func DropLegacySchedulingColumns() error {
	if err := carryOverChannelWeightToRoutes(); err != nil {
		return err
	}
	for _, target := range []struct {
		table  string
		column string
	}{
		{"channels", "priority"},
		{"channels", "weight"},
		{"abilities", "priority"},
		{"abilities", "weight"},
	} {
		if err := dropColumnIfExists(target.table, target.column); err != nil {
			return fmt.Errorf("failed to drop %s.%s: %w", target.table, target.column, err)
		}
	}
	return nil
}

// carryOverChannelWeightToRoutes copies a channel's configured weight onto its
// route unit rows before the column disappears.
//
// The two settings were never linked: ExpandChannelModelRoutes seeds every route
// with static_weight 100 and has never read channel.Weight. An operator who tuned
// channel weights therefore has that intent recorded only in the column being
// dropped, and dropping it silently would reset their distribution to uniform.
//
// Only rows still sitting at the seed default are rewritten. A route whose weight
// was already tuned in the route unit UI expresses a newer intent than the legacy
// channel column, so it wins.
func carryOverChannelWeightToRoutes() error {
	if !columnExists("channels", "weight") {
		return nil
	}
	// The carry-over runs before AutoMigrate has created channel_model_routes on
	// a fresh install. Treat a missing target table as a no-op so the legacy
	// column can still be dropped; nothing to migrate anyway.
	if !tableExists("channel_model_routes") {
		return nil
	}
	type legacyWeight struct {
		Id     int
		Weight *uint
	}
	var rows []legacyWeight
	if err := DB.Table("channels").
		Select("id", "weight").
		Where("weight IS NOT NULL AND weight > 0").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("failed to read legacy channel weights: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	migrated := 0
	if err := DB.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if row.Weight == nil || *row.Weight == 0 {
				continue
			}
			result := tx.Table("channel_model_routes").
				Where("channel_id = ? AND static_weight = ?", row.Id, legacyRouteSeedWeight).
				Update("static_weight", int(*row.Weight))
			if result.Error != nil {
				return result.Error
			}
			migrated += int(result.RowsAffected)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to carry channel weights onto route units: %w", err)
	}
	if migrated > 0 {
		common.SysLog(fmt.Sprintf("migrated legacy channel weight onto %d route unit rows", migrated))
	}
	return nil
}

// columnExists probes the live schema without going through GORM's migrator.
//
// Migrator().HasColumn with a string table name panics on MySQL: RunWithValue
// cannot resolve a statement schema from a bare name, and the MySQL migrator then
// dereferences the nil schema (gorm migrator.go:398). Passing a model struct would
// avoid it, but these columns have already been removed from the structs, which is
// the whole reason this migration exists. Querying the catalogue directly keeps the
// probe independent of any Go type.
func columnExists(table, column string) bool {
	var count int64
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		// SQLite has no information_schema; pragma_table_info is the table-valued
		// equivalent and returns no rows for a table that does not exist.
		if err := DB.Raw(
			"SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", table, column,
		).Scan(&count).Error; err != nil {
			return false
		}
		return count > 0
	}
	// The current-schema function differs: MySQL spells it DATABASE(), PostgreSQL
	// current_schema(). Scoping the lookup matters either way, because a shared
	// server can hold same-named tables in other schemas.
	schemaFunc := "DATABASE()"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		schemaFunc = "current_schema()"
	}
	if err := DB.Raw(
		"SELECT count(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ? AND table_schema = "+schemaFunc,
		table, column,
	).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// tableExists probes the live schema for a table without going through GORM.
// Mirrors columnExists so carry-over can no-op when its target table has not
// been AutoMigrate'd yet (e.g. fresh installs that still carry legacy
// channels.weight from a previous deployment).
func tableExists(table string) bool {
	var count int64
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := DB.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table,
		).Scan(&count).Error; err != nil {
			return false
		}
		return count > 0
	}
	schemaFunc := "DATABASE()"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		schemaFunc = "current_schema()"
	}
	if err := DB.Raw(
		"SELECT count(*) FROM information_schema.tables WHERE table_name = ? AND table_schema = "+schemaFunc,
		table,
	).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// dropColumnIfExists removes a column on every supported database.
//
// The GORM SQLite migrator is deliberately bypassed: glebarez/sqlite implements
// DropColumn by recreating the whole table, and that path panics on a table whose
// DDL it cannot round-trip (observed on channels). SQLite has supported native
// ALTER TABLE ... DROP COLUMN since 3.35 and the bundled driver is newer, so plain
// DDL works on all three engines. The columnExists guard keeps it idempotent
// across restarts.
//
// Indexes on the column are dropped first. abilities.priority and abilities.weight
// each carried a gorm `index` tag, and SQLite refuses the drop while the index
// survives: "error in index idx_abilities_weight after drop column: no such
// column: weight". MySQL silently rewrites a single-column index and PostgreSQL
// drops it, but doing it explicitly keeps all three engines on one path.
func dropColumnIfExists(table, column string) error {
	if !columnExists(table, column) {
		return nil
	}
	if err := dropIndexesOnColumn(table, column); err != nil {
		return err
	}
	quoted := "`" + column + "`"
	quotedTable := "`" + table + "`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		quoted = `"` + column + `"`
		quotedTable = `"` + table + `"`
	}
	if err := DB.Exec("ALTER TABLE " + quotedTable + " DROP COLUMN " + quoted).Error; err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("dropped legacy column %s.%s", table, column))
	return nil
}

// dropIndexesOnColumn removes every index that references column, so the column
// drop cannot fail on a surviving index. Index names are read from the live
// schema rather than assumed: GORM's generated name (idx_abilities_weight) is
// only the default, and an operator may have added their own.
func dropIndexesOnColumn(table, column string) error {
	var names []string
	switch {
	case common.UsingMainDatabase(common.DatabaseTypeSQLite):
		// pragma_index_list gives the table's indexes; pragma_index_info the
		// columns of each. Partial/expression indexes report no column name and
		// are simply absent from the join, which is correct: they cannot be
		// single-column indexes on this column.
		if err := DB.Raw(`SELECT il.name FROM pragma_index_list(?) il
			JOIN pragma_index_info(il.name) ii
			WHERE ii.name = ?`, table, column).Scan(&names).Error; err != nil {
			return err
		}
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		if err := DB.Raw(`SELECT DISTINCT index_name FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ? AND index_name <> 'PRIMARY'`,
			table, column).Scan(&names).Error; err != nil {
			return err
		}
	default:
		if err := DB.Raw(`SELECT DISTINCT i.relname FROM pg_index x
			JOIN pg_class i ON i.oid = x.indexrelid
			JOIN pg_class t ON t.oid = x.indrelid
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(x.indkey)
			WHERE t.relname = ? AND a.attname = ? AND NOT x.indisprimary`,
			table, column).Scan(&names).Error; err != nil {
			return err
		}
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		quoted := "`" + name + "`"
		if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
			quoted = `"` + name + `"`
		}
		stmt := "DROP INDEX " + quoted
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
			// MySQL has no bare DROP INDEX; it must name the table.
			stmt = "ALTER TABLE `" + table + "` DROP INDEX " + quoted
		}
		if err := DB.Exec(stmt).Error; err != nil {
			return fmt.Errorf("failed to drop index %s on %s.%s: %w", name, table, column, err)
		}
		common.SysLog(fmt.Sprintf("dropped legacy index %s on %s.%s", name, table, column))
	}
	return nil
}
