// Package dbx holds the process-wide GORM handles and the dialect-dependent SQL
// fragments that every persistence layer needs.
//
// It exists so a domain can own its own records without depending on a shared
// record package. While the handle lived in package model alongside every record
// type, extracting one domain's records produced an import cycle: the moved
// records still needed the handle, and the records left behind still needed the
// moved types.
//
// This package deliberately knows nothing about any record. Handles, the
// reserved-word column names, and the FOR UPDATE helper — nothing that would
// make it depend on a domain.
package dbx

import (
	"github.com/QuantumNous/new-api/internal/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DB is the main database handle and LogDB the (possibly separate) log database
// handle. Both are installed during startup by the database bootstrap, and
// reassigned directly by tests that run against a scratch database.
var (
	DB    *gorm.DB
	LogDB *gorm.DB
)

// Reserved-word column names, quoted for the active dialect. PostgreSQL wants
// "col" while MySQL and SQLite want `col`, so raw SQL touching group or key must
// go through these instead of hardcoding either form.
var (
	groupCol string
	keyCol   string
	trueVal  string
	falseVal string

	logGroupCol string
	logKeyCol   string
)

// GroupCol returns the quoted name of the reserved "group" column.
//
// These are assigned by InitColumns during database setup, so callers must read
// them through the accessors — a copy taken at package-init time is still empty.
func GroupCol() string { return groupCol }

// KeyCol returns the quoted name of the reserved "key" column.
func KeyCol() string { return keyCol }

// TrueVal returns the dialect's true literal: PostgreSQL accepts true/false
// while MySQL and SQLite want 1/0.
func TrueVal() string { return trueVal }

// FalseVal returns the dialect's false literal.
func FalseVal() string { return falseVal }

// LogGroupCol returns the quoted "group" column for the log database, which may
// run a different engine than the main one.
func LogGroupCol() string { return logGroupCol }

// LogKeyCol returns the quoted "key" column for the log database.
func LogKeyCol() string { return logKeyCol }

// InitColumns resolves every dialect-dependent fragment from the configured
// database types. It must run after common.SetDatabaseTypes and before any query
// built from these accessors.
func InitColumns() {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		groupCol = `"group"`
		keyCol = `"key"`
		trueVal = "true"
		falseVal = "false"
	} else {
		groupCol = "`group`"
		keyCol = "`key`"
		trueVal = "1"
		falseVal = "0"
	}

	switch common.LogDatabaseType() {
	case common.DatabaseTypePostgreSQL:
		logGroupCol = `"group"`
		logKeyCol = `"key"`
	default:
		logGroupCol = "`group`"
		logKeyCol = "`key`"
	}
}

// LockForUpdate makes the next query emit SELECT ... FOR UPDATE so the matched
// rows stay locked until the surrounding transaction ends.
//
// GORM v2 silently ignores the legacy Set("gorm:query_option", "FOR UPDATE")
// spelling from GORM v1, so that form locks nothing. Always use this helper.
//
// SQLite has no FOR UPDATE syntax (the clause would be a syntax error), so it is
// skipped there; SQLite's single-writer model makes one of two conflicting
// transactions fail instead of both committing.
func LockForUpdate(tx *gorm.DB) *gorm.DB {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}
