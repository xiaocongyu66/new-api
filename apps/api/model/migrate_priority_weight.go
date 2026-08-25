package model

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

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
			result := tx.Model(&ChannelModelRoute{}).
				Where("channel_id = ? AND static_weight = ?", row.Id, defaultRouteStaticWeight).
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

func columnExists(table, column string) bool {
	if !DB.Migrator().HasTable(table) {
		return false
	}
	return DB.Migrator().HasColumn(table, column)
}

// dropColumnIfExists removes a column on every supported database.
//
// The GORM SQLite migrator is deliberately bypassed: glebarez/sqlite implements
// DropColumn by recreating the whole table, and that path panics on a table whose
// DDL it cannot round-trip (observed on channels). SQLite has supported native
// ALTER TABLE ... DROP COLUMN since 3.35 and the bundled driver is newer, so plain
// DDL works on all three engines. The HasColumn guard is what keeps it idempotent
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
