package channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// This domain owns channel_model_health and the gateway config revision, so it
// registers its own post-AutoMigrate steps instead of the bootstrap naming them.
// Registration order matches the previous literal sequence in model/main.go:
// the key-index migration, then the gateway revision seed.
func init() {
	dbx.RegisterPostMigration(
		migrateChannelModelHealthKeyIndex,
		InitializeGatewayConfigRevision,
	)
}

func migrateChannelModelHealthKeyIndex() error {
	if !dbx.DB.Migrator().HasTable(&ChannelModelHealth{}) {
		return nil
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		var rows []struct {
			Name string `gorm:"column:name"`
			PK   int    `gorm:"column:pk"`
		}
		if err := dbx.DB.Raw("PRAGMA table_info(channel_model_health)").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.Name == "key_index" && row.PK == 2 {
				return nil
			}
		}
		return dbx.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE channel_model_health_new (
				channel_id integer NOT NULL,
				key_index integer NOT NULL DEFAULT 0,
				model varchar(255) NOT NULL,
				state varchar(16) NOT NULL DEFAULT 'healthy',
				isolation_level integer NOT NULL DEFAULT 0,
				until bigint,
				version integer NOT NULL DEFAULT 1,
				dormant_disable_count integer NOT NULL DEFAULT 0,
				local_failure_count integer NOT NULL DEFAULT 0,
				upstream_failure_count integer NOT NULL DEFAULT 0,
				last_error_code varchar(64),
				last_error_at bigint,
				last_success_at bigint,
				updated_at bigint,
				PRIMARY KEY (channel_id, key_index, model)
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO channel_model_health_new (
				channel_id, key_index, model, state, isolation_level, until, version,
				dormant_disable_count, local_failure_count, upstream_failure_count,
				last_error_code, last_error_at, last_success_at, updated_at
			) SELECT channel_id, 0, model, state, isolation_level, until, version,
				dormant_disable_count, local_failure_count, upstream_failure_count,
				last_error_code, last_error_at, last_success_at, updated_at
			FROM channel_model_health`).Error; err != nil {
				return err
			}
			if err := tx.Exec("DROP TABLE channel_model_health").Error; err != nil {
				return err
			}
			return tx.Exec("ALTER TABLE channel_model_health_new RENAME TO channel_model_health").Error
		})
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		var primaryColumns []string
		if err := dbx.DB.Raw("SELECT column_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = 'PRIMARY' ORDER BY seq_in_index", "channel_model_health").Scan(&primaryColumns).Error; err != nil {
			return err
		}
		if len(primaryColumns) == 3 && primaryColumns[1] == "key_index" {
			return nil
		}
		return dbx.DB.Transaction(func(tx *gorm.DB) error {
			if !tx.Migrator().HasColumn(&ChannelModelHealth{}, "key_index") {
				if err := tx.Exec("ALTER TABLE channel_model_health ADD COLUMN key_index integer NOT NULL DEFAULT 0").Error; err != nil {
					return err
				}
			}
			return tx.Exec("ALTER TABLE channel_model_health DROP PRIMARY KEY, ADD PRIMARY KEY (channel_id, key_index, model)").Error
		})
	}
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		var primaryColumns []string
		if err := dbx.DB.Raw(`SELECT attribute.attname
			FROM pg_index index_def
			JOIN pg_class table_def ON table_def.oid = index_def.indrelid
			JOIN pg_attribute attribute ON attribute.attrelid = table_def.oid AND attribute.attnum = ANY(index_def.indkey)
			WHERE table_def.relname = 'channel_model_health' AND index_def.indisprimary
			ORDER BY array_position(index_def.indkey, attribute.attnum)`).Scan(&primaryColumns).Error; err != nil {
			return err
		}
		if len(primaryColumns) == 3 && primaryColumns[1] == "key_index" {
			return nil
		}
		return dbx.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("ALTER TABLE channel_model_health ADD COLUMN IF NOT EXISTS key_index integer NOT NULL DEFAULT 0").Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE channel_model_health DROP CONSTRAINT channel_model_health_pkey").Error; err != nil {
				return err
			}
			return tx.Exec("ALTER TABLE channel_model_health ADD PRIMARY KEY (channel_id, key_index, model)").Error
		})
	}
	return fmt.Errorf("unsupported database for channel model health migration")
}
