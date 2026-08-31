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

// dropLegacySchedulingColumns retires the pre-route-unit scheduling columns:
// channels.priority, channels.weight, abilities.priority and abilities.weight.
//
// It must run BEFORE AutoMigrate. The Channel and Ability structs no longer carry
// these fields, so once AutoMigrate has run GORM cannot read the old values back —
// the columns would linger with the operator's configured weights stranded inside
// them. Ordering the carry-over first is what makes the cleanup non-destructive.
func dropLegacySchedulingColumns() error {
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
// dropColumnIfExists removes a column on every supported database.
//
// The GORM SQLite migrator is deliberately bypassed: glebarez/sqlite implements
// DropColumn by recreating the whole table, and that path panics on a table whose
// DDL it cannot round-trip (observed on channels). SQLite has supported native
// ALTER TABLE ... DROP COLUMN since 3.35 and the bundled driver is newer, so plain
// DDL works on all three engines. The columnExists guard keeps it idempotent
// across restarts.
func dropColumnIfExists(table, column string) error {
	if !columnExists(table, column) {
		return nil
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
