package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	rootmodel "github.com/QuantumNous/new-api/model"
)

// pre-migrate 钩子：把 subscription_plans.price_amount 列从 float/double
// 调整为 decimal(10,6)。多次运行安全（先查 metadata 再决定是否发 ALTER）。
// SQLite 通过类型亲和自动处理，跳过即可。
func init() {
	rootmodel.RegisterPreMigrateHook(func() error {
		migrateSubscriptionPlanPriceAmount()
		return nil
	})

	// SQLite 下 AutoMigrate 对 subscription_plans 的部分列类型支持不完整，
	// 因此在 AutoMigrate 之后再做一次兜底建表 + 补列。
	rootmodel.RegisterPostMigrateHook(func() error {
		return ensureSubscriptionPlanTableSQLite()
	})
}

func migrateSubscriptionPlanPriceAmount() {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return
	}

	tableName := "subscription_plans"
	columnName := "price_amount"

	if !common.DB.Migrator().HasTable(tableName) {
		return
	}

	if !common.DB.Migrator().HasColumn(&SubscriptionPlan{}, columnName) {
		return
	}

	var alterSQL string
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		var dataType string
		if err := common.DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "numeric" {
			return
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE decimal(10,6) USING %s::decimal(10,6)`,
			tableName, columnName, columnName)
	} else if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		var columnType string
		if err := common.DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.HasPrefix(strings.ToLower(columnType), "decimal") {
			return
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s decimal(10,6) NOT NULL DEFAULT 0",
			tableName, columnName)
	} else {
		return
	}

	if alterSQL != "" {
		if err := common.DB.Exec(alterSQL).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to migrate %s.%s to decimal: %v", tableName, columnName, err))
		} else {
			common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to decimal(10,6)", tableName, columnName))
		}
	}
}

// SQLite 下 AutoMigrate 无法正确生成部分列（decimal / numeric 默认值、CHECK 约束），
// 因此用裸 SQL 兜底：表不存在则按结构创建，存在则补齐缺失列。
func ensureSubscriptionPlanTableSQLite() error {
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return nil
	}
	tableName := "subscription_plans"
	if !common.DB.Migrator().HasTable(tableName) {
		createSQL := `CREATE TABLE ` + "`" + tableName + "`" + ` (
` + "`id`" + ` integer,
` + "`title`" + ` varchar(128) NOT NULL,
` + "`subtitle`" + ` varchar(255) DEFAULT '',
` + "`price_amount`" + ` decimal(10,6) NOT NULL,
` + "`currency`" + ` varchar(8) NOT NULL DEFAULT 'USD',
` + "`duration_unit`" + ` varchar(16) NOT NULL DEFAULT 'month',
` + "`duration_value`" + ` integer NOT NULL DEFAULT 1,
` + "`custom_seconds`" + ` bigint NOT NULL DEFAULT 0,
` + "`enabled`" + ` numeric DEFAULT 1,
` + "`sort_order`" + ` integer DEFAULT 0,
` + "`allow_balance_pay`" + ` numeric DEFAULT 1,
` + "`allow_wallet_overflow`" + ` numeric DEFAULT 1,
` + "`stripe_price_id`" + ` varchar(128) DEFAULT '',
` + "`creem_product_id`" + ` varchar(128) DEFAULT '',
` + "`waffo_pancake_product_id`" + ` varchar(128) DEFAULT '',
` + "`max_purchase_per_user`" + ` integer DEFAULT 0,
` + "`upgrade_group`" + ` varchar(64) DEFAULT '',
` + "`downgrade_group`" + ` varchar(64) DEFAULT '',
` + "`total_amount`" + ` bigint NOT NULL DEFAULT 0,
` + "`quota_reset_period`" + ` varchar(16) DEFAULT 'never',
` + "`quota_reset_custom_seconds`" + ` bigint DEFAULT 0,
` + "`created_at`" + ` bigint,
` + "`updated_at`" + ` bigint,
PRIMARY KEY (` + "`id`" + `)
)`
		return common.DB.Exec(createSQL).Error
	}
	var cols []struct {
		Name string `gorm:"column:name"`
	}
	if err := common.DB.Raw("PRAGMA table_info(`" + tableName + "`)").Scan(&cols).Error; err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		existing[c.Name] = struct{}{}
	}
	required := []struct {
		Name string
		DDL  string
	}{
		{"title", "`title` varchar(128) NOT NULL"},
		{"subtitle", "`subtitle` varchar(255) DEFAULT ''"},
		{"price_amount", "`price_amount` decimal(10,6) NOT NULL"},
		{"currency", "`currency` varchar(8) NOT NULL DEFAULT 'USD'"},
		{"duration_unit", "`duration_unit` varchar(16) NOT NULL DEFAULT 'month'"},
		{"duration_value", "`duration_value` integer NOT NULL DEFAULT 1"},
		{"custom_seconds", "`custom_seconds` bigint NOT NULL DEFAULT 0"},
		{"enabled", "`enabled` numeric DEFAULT 1"},
		{"sort_order", "`sort_order` integer DEFAULT 0"},
		{"allow_balance_pay", "`allow_balance_pay` numeric DEFAULT 1"},
		{"allow_wallet_overflow", "`allow_wallet_overflow` numeric DEFAULT 1"},
		{"stripe_price_id", "`stripe_price_id` varchar(128) DEFAULT ''"},
		{"creem_product_id", "`creem_product_id` varchar(128) DEFAULT ''"},
		{"waffo_pancake_product_id", "`waffo_pancake_product_id` varchar(128) DEFAULT ''"},
		{"max_purchase_per_user", "`max_purchase_per_user` integer DEFAULT 0"},
		{"upgrade_group", "`upgrade_group` varchar(64) DEFAULT ''"},
		{"downgrade_group", "`downgrade_group` varchar(64) DEFAULT ''"},
		{"total_amount", "`total_amount` bigint NOT NULL DEFAULT 0"},
		{"quota_reset_period", "`quota_reset_period` varchar(16) DEFAULT 'never'"},
		{"quota_reset_custom_seconds", "`quota_reset_custom_seconds` bigint DEFAULT 0"},
		{"created_at", "`created_at` bigint"},
		{"updated_at", "`updated_at` bigint"},
	}
	for _, col := range required {
		if _, ok := existing[col.Name]; ok {
			continue
		}
		if err := common.DB.Exec("ALTER TABLE `" + tableName + "` ADD COLUMN " + col.DDL).Error; err != nil {
			return err
		}
	}
	return nil
}
