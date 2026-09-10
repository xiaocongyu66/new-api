package dbx

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
)

// CollapseRouteUnitGroups retires channel_model_routes.group.
//
// A user group decides which aliases a user may reach; it never decided how
// traffic is split inside an alias. Keying route units by group therefore
// multiplied every unit by the number of groups that could see it: one channel
// serving one model across six groups produced six rows that were identical in
// every scheduling respect, each with its own weight to tune and its own EWMA
// sample stream to warm up. Operators saw six indistinguishable rows and could
// only tune one at a time — an observed production incident left one group of a
// six-row set at the seed weight while the other five were tuned to 2.
//
// This MUST run before AutoMigrate. AutoMigrate creates the new unique index on
// (public_model_alias, channel_id, key_index, upstream_model), and PostgreSQL and
// MySQL both refuse a unique index on a table that still holds duplicates under
// that key — which every multi-group deployment does by construction. Collapsing
// first is what lets the index be created at all.
//
// The surviving row per unit is the lowest id, and it keeps the maximum weight of
// its group-siblings.
//
// Maximum rather than minimum or first, because a divergent sibling set is
// evidence of an accident rather than intent. The rows were un-authorable: the
// admin list rendered them identically and offered no per-group control, so no
// operator could deliberately give one group a different weight. What divergence
// actually records is a half-finished pass over rows the operator could not tell
// apart. Taking the maximum restores the seed default (100) in that case, which
// keeps the route's share of its pool intact; taking the minimum would silently
// adopt the accident and, at the observed values, leave the unit with 3/202 of
// the pool — a 50x under-serve that looks like an outage, not a tuning choice.
// Genuine re-weighting is done afterwards on the single surviving row.
//
// A route is kept enabled when any sibling was enabled, because a unit disabled
// in one group but serving in another is live.
func CollapseRouteUnitGroups() error {
	if !tableExists("channel_model_routes") {
		return nil
	}
	if !columnExists("channel_model_routes", "group") {
		return nil
	}

	type collapsed struct {
		Alias         string
		ChannelId     int
		KeyIndex      int
		UpstreamModel string
		KeepId        int
		StaticWeight  int
		MinWeight     int
		Enabled       bool
	}
	var rows []collapsed
	// Aggregate per route-unit identity. bool_or/max are spelled differently per
	// dialect, so the enabled flag is folded with max() over an integer cast, which
	// every supported engine accepts.
	enabledExpr := "max(CASE WHEN enabled THEN 1 ELSE 0 END) AS enabled"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		enabledExpr = "max(CASE WHEN enabled THEN 1 ELSE 0 END)::int AS enabled"
	}
	if err := DB.Table("channel_model_routes").
		Select("public_model_alias AS alias, channel_id, key_index, upstream_model, " +
			"min(id) AS keep_id, max(static_weight) AS static_weight, " +
			"min(static_weight) AS min_weight, " + enabledExpr).
		Group("public_model_alias, channel_id, key_index, upstream_model").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("failed to aggregate route units for group collapse: %w", err)
	}
	if len(rows) == 0 {
		// Nothing stored yet: drop the column and let the route seed repopulate.
		return dropRouteUnitGroupColumn()
	}

	keepIds := make([]int, 0, len(rows))
	for _, r := range rows {
		keepIds = append(keepIds, r.KeepId)
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		// Delete the redundant siblings first, so the surviving row is unique under
		// the new key before the index is built.
		res := tx.Table("channel_model_routes").Where("id NOT IN ?", keepIds).Delete(nil)
		if res.Error != nil {
			return res.Error
		}
		removed := res.RowsAffected
		for _, r := range rows {
			if err := tx.Table("channel_model_routes").
				Where("id = ?", r.KeepId).
				Updates(map[string]any{"static_weight": r.StaticWeight, "enabled": r.Enabled}).Error; err != nil {
				return err
			}
		}
		if removed > 0 {
			common.SysLog(fmt.Sprintf(
				"collapsed group-scoped route units: %d redundant rows removed, %d units kept", removed, len(rows)))
		}
		// Name every unit whose siblings disagreed. The resolved value is the one now
		// in force, so an operator who did mean to re-weight the unit can see which
		// row to revisit instead of discovering the change by traffic shift.
		for _, r := range rows {
			if r.MinWeight == r.StaticWeight {
				continue
			}
			common.SysLog(fmt.Sprintf(
				"route unit %s (channel %d, key %d, upstream %s) had per-group weights %d..%d; resolved to %d",
				r.Alias, r.ChannelId, r.KeyIndex, r.UpstreamModel, r.MinWeight, r.StaticWeight, r.StaticWeight))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to collapse group-scoped route units: %w", err)
	}

	return dropRouteUnitGroupColumn()
}

// dropRouteUnitGroupColumn removes the retired column together with the old
// group-scoped unique index. The index must go first: it names a column that is
// about to disappear, and SQLite refuses the column drop while such an index
// survives.
func dropRouteUnitGroupColumn() error {
	if err := dropColumnIfExists("channel_model_routes", "group"); err != nil {
		return fmt.Errorf("failed to drop channel_model_routes.group: %w", err)
	}
	return nil
}
