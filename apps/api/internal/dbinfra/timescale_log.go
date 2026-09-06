package dbinfra

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// TimescaleDB turns the append-only `logs` table into a hypertable so that
// time-range queries prune chunks instead of scanning the whole table, old
// chunks compress, and retention drops chunks instead of deleting rows.
//
// Everything here is opt-in and self-disabling: when TIMESCALEDB_ENABLED is
// false, the log DB is not PostgreSQL, or the extension is not installed in the
// server, setup logs one line and returns nil. SQLite, MySQL, plain PostgreSQL,
// and ClickHouse deployments are unaffected.
const (
	// defaultChunkIntervalSeconds is one week. created_at is a bigint Unix
	// epoch, not a timestamptz, so Timescale cannot infer an interval and an
	// explicit integer must be supplied.
	defaultChunkIntervalSeconds = 7 * 24 * 60 * 60
)

func timescaleEnabled() bool {
	return common.GetEnvOrDefaultBool("TIMESCALEDB_ENABLED", false)
}

// setupTimescaleLogHypertable runs after the log-DB AutoMigrate has created
// `logs`. It is idempotent: every statement tolerates an already-converted
// table, so restarts are cheap.
func setupTimescaleLogHypertable() error {
	if !timescaleEnabled() || !common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		return nil
	}

	var available bool
	if err := dbx.LogDB.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb')",
	).Scan(&available).Error; err != nil {
		return fmt.Errorf("timescaledb: probe failed: %w", err)
	}
	if !available {
		common.SysLog("timescaledb: TIMESCALEDB_ENABLED is set but the extension is not available on this server; continuing with a plain logs table")
		return nil
	}

	if err := dbx.LogDB.Exec("CREATE EXTENSION IF NOT EXISTS timescaledb").Error; err != nil {
		return fmt.Errorf("timescaledb: create extension: %w", err)
	}

	// A hypertable requires its partitioning column in every unique index, so
	// the AutoMigrate-created PRIMARY KEY (id) must widen to (created_at, id).
	// This runs only on the Timescale path; the GORM model is untouched, so
	// SQLite/MySQL/plain-PostgreSQL keep PRIMARY KEY (id). `id` stays a serial
	// with its own non-unique index, which is what the `ORDER BY logs.id desc`
	// pagination queries need.
	var pkDef string
	if err := dbx.LogDB.Raw(
		"SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'logs_pkey' AND conrelid = 'logs'::regclass",
	).Scan(&pkDef).Error; err != nil {
		return fmt.Errorf("timescaledb: read PK def: %w", err)
	}
	if pkDef != "" && pkDef != "PRIMARY KEY (created_at, id)" {
		if pkDef == "PRIMARY KEY (id)" {
			if err := dbx.LogDB.Exec(`
				ALTER TABLE logs DROP CONSTRAINT logs_pkey;
				ALTER TABLE logs ADD PRIMARY KEY (created_at, id);
				CREATE INDEX IF NOT EXISTS idx_logs_id ON logs (id);
			`).Error; err != nil {
				return fmt.Errorf("timescaledb: widen primary key: %w", err)
			}
		}
	}

	chunkInterval := common.GetEnvOrDefault("TIMESCALEDB_CHUNK_INTERVAL_SECONDS", defaultChunkIntervalSeconds)
	if chunkInterval <= 0 {
		chunkInterval = defaultChunkIntervalSeconds
	}
	if err := dbx.LogDB.Exec(
		"SELECT create_hypertable('logs', 'created_at', chunk_time_interval => ?::bigint, if_not_exists => TRUE, migrate_data => TRUE)",
		chunkInterval,
	).Error; err != nil {
		return fmt.Errorf("timescaledb: create_hypertable: %w", err)
	}

	// An integer time column needs integer_now so that age-relative compression
	// and retention policies know what "now" means.
	if err := dbx.LogDB.Exec(`
CREATE OR REPLACE FUNCTION logs_created_at_now() RETURNS bigint
	LANGUAGE SQL STABLE AS $$ SELECT extract(epoch FROM now())::bigint $$`).Error; err != nil {
		return fmt.Errorf("timescaledb: create integer_now function: %w", err)
	}
	if err := dbx.LogDB.Exec(
		"SELECT set_integer_now_func('logs', 'logs_created_at_now', replace_if_exists => TRUE)",
	).Error; err != nil {
		return fmt.Errorf("timescaledb: set_integer_now_func: %w", err)
	}

	if err := applyTimescaleCompression(); err != nil {
		return err
	}
	if err := applyTimescaleRetention(); err != nil {
		return err
	}

	common.SysLog(fmt.Sprintf("timescaledb: logs is a hypertable (chunk_time_interval=%ds)", chunkInterval))
	return nil
}

// applyTimescaleCompression compresses chunks older than the configured age.
// Off by default: compressed chunks are read-only in older Timescale versions,
// so opting in is a deployment decision.
func applyTimescaleCompression() error {
	days := common.GetEnvOrDefault("TIMESCALEDB_COMPRESS_AFTER_DAYS", 0)
	if days <= 0 {
		return nil
	}
	if err := dbx.LogDB.Exec(
		"ALTER TABLE logs SET (timescaledb.compress, timescaledb.compress_orderby = 'created_at DESC')",
	).Error; err != nil {
		return fmt.Errorf("timescaledb: enable compression: %w", err)
	}
	if err := dbx.LogDB.Exec(
		"SELECT add_compression_policy('logs', ?::bigint, if_not_exists => TRUE)",
		days*24*60*60,
	).Error; err != nil {
		return fmt.Errorf("timescaledb: add_compression_policy: %w", err)
	}
	common.SysLog(fmt.Sprintf("timescaledb: compressing log chunks older than %d days", days))
	return nil
}

// applyTimescaleRetention drops whole chunks past the configured age. Off by
// default because logs are billing evidence; dropping them must be deliberate.
func applyTimescaleRetention() error {
	days := common.GetEnvOrDefault("TIMESCALEDB_RETENTION_DAYS", 0)
	if days <= 0 {
		return nil
	}
	if err := dbx.LogDB.Exec(
		"SELECT add_retention_policy('logs', ?::bigint, if_not_exists => TRUE)",
		days*24*60*60,
	).Error; err != nil {
		return fmt.Errorf("timescaledb: add_retention_policy: %w", err)
	}
	common.SysLog(fmt.Sprintf("timescaledb: dropping log chunks older than %d days", days))
	return nil
}
