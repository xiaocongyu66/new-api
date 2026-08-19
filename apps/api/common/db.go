package common

import (
	"gorm.io/gorm"
)

// DB is the primary GORM database handle. Initialized by model.InitDB().
// Hoisted here so every domain model package can reach the connection without
// importing apps/api/model (which would create a cycle once model/main.go
// imports the domain packages for AutoMigrate registration).
var DB *gorm.DB

// LOG_DB is the dedicated GORM handle for the log database. May equal DB when
// the deployment reuses the primary database for logs.
var LOG_DB *gorm.DB

// Dialect-specific column name and boolean literal constants. PostgreSQL uses
// double-quoted identifiers and "true"/"false"; MySQL/SQLite use backticks and
// "1"/"0".
var (
	CommonGroupCol string
	CommonKeyCol   string
	CommonTrueVal  string
	CommonFalseVal string

	LogKeyCol   string
	LogGroupCol string
)

// InitCol sets the dialect-specific column and literal constants. Safe to call
// multiple times; later calls overwrite earlier ones.
func InitCol() {
	if UsingMainDatabase(DatabaseTypePostgreSQL) {
		CommonGroupCol = `"group"`
		CommonKeyCol = `"key"`
		CommonTrueVal = "true"
		CommonFalseVal = "false"
	} else {
		CommonGroupCol = "`group`"
		CommonKeyCol = "`key`"
		CommonTrueVal = "1"
		CommonFalseVal = "0"
	}
	switch LogDatabaseType() {
	case DatabaseTypePostgreSQL:
		LogGroupCol = `"group"`
		LogKeyCol = `"key"`
	default:
		LogGroupCol = "`group`"
		LogKeyCol = "`key`"
	}
}
