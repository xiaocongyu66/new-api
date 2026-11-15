package billing

import (
	"errors"
	"fmt"
	"strings"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"gorm.io/gorm"
)

// The shared-quota scope is retired: every legacy shared pool becomes one
// group-scoped quota row. Plans keep selling the same amount, but only inside
// the chosen group; existing subscriptions keep their remaining balance as a
// counter row for the group they currently use, so nobody loses paid quota.
//
// The conversion is intentionally an explicit admin operation (dry-run first)
// rather than an AutoMigrate hook: it moves money between ledgers, and the
// operator must see which plan gets which group before it runs.

var errSharedScopeDryRun = errors.New("shared scope conversion dry run")

type SharedScopeConversionRequest struct {
	DryRun bool `json:"dry_run"`
	// Force skips the in-flight pre-consume guard. Only use when the relay is
	// drained; a request settling during the move would bypass the new row.
	Force bool `json:"force"`
	// DefaultGroup is the fallback for plans/subscriptions without a usable
	// upgrade group. Empty means "default".
	DefaultGroup string `json:"default_group"`
	// PlanGroups pins a plan id to a group, overriding the upgrade snapshot.
	PlanGroups map[int]string `json:"plan_groups"`
}

type SharedScopeConversionPlan struct {
	Id          int    `json:"id"`
	Title       string `json:"title"`
	TargetGroup string `json:"target_group"`
	Amount      int64  `json:"amount"`
}

type SharedScopeConversionSubscription struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id"`
	PlanId      int    `json:"plan_id"`
	TargetGroup string `json:"target_group"`
	Total       int64  `json:"total"`
	Used        int64  `json:"used"`
}

type SharedScopeConversionSkip struct {
	Kind   string `json:"kind"`
	Id     int    `json:"id"`
	Reason string `json:"reason"`
}

type SharedScopeConversionReport struct {
	DryRun        bool                                `json:"dry_run"`
	DefaultGroup  string                              `json:"default_group"`
	Plans         []SharedScopeConversionPlan         `json:"plans"`
	Subscriptions []SharedScopeConversionSubscription `json:"subscriptions"`
	Skipped       []SharedScopeConversionSkip         `json:"skipped"`
}

func (r *SharedScopeConversionReport) skip(kind string, id int, reason string) {
	r.Skipped = append(r.Skipped, SharedScopeConversionSkip{Kind: kind, Id: id, Reason: reason})
}

// ConvertLegacySharedScopes rewrites every scope=” / scope='all' non-request
// plan and subscription into scope='group' + measure='quota'.
//
// Plans move total_amount into one request rule for the target group; the
// subscription snapshot moves amount_total/amount_used into a quota-measured
// SubscriptionRequestCounter row and zeroes the shared-pool columns so the
// counter is the only remaining gate.
func ConvertLegacySharedScopes(req SharedScopeConversionRequest) (*SharedScopeConversionReport, error) {
	groupRatios := ratio_setting.GetGroupRatioCopy()
	if len(groupRatios) == 0 {
		return nil, errors.New("group ratio settings are not loaded")
	}
	defaultGroup := strings.TrimSpace(req.DefaultGroup)
	if defaultGroup == "" {
		defaultGroup = "default"
	}
	if _, ok := groupRatios[defaultGroup]; !ok {
		return nil, fmt.Errorf("default group %q is not configured", defaultGroup)
	}

	report := &SharedScopeConversionReport{
		DryRun:        req.DryRun,
		DefaultGroup:  defaultGroup,
		Plans:         []SharedScopeConversionPlan{},
		Subscriptions: []SharedScopeConversionSubscription{},
		Skipped:       []SharedScopeConversionSkip{},
	}

	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if !req.DryRun && !req.Force {
			cutoff := common.GetTimestamp() - 300
			var inFlight int64
			if err := tx.Model(&SubscriptionPreConsumeRecord{}).
				Where("status = ? AND request_counter_id = 0 AND created_at >= ?", "consumed", cutoff).
				Count(&inFlight).Error; err != nil {
				return err
			}
			if inFlight > 0 {
				return fmt.Errorf("%d subscription request(s) are still settling; retry after they finish or pass force=true", inFlight)
			}
		}
		if err := convertLegacySharedPlansTx(tx, req, groupRatios, defaultGroup, report); err != nil {
			return err
		}
		if err := convertLegacySharedSubscriptionsTx(tx, req, groupRatios, defaultGroup, report); err != nil {
			return err
		}
		if req.DryRun {
			return errSharedScopeDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSharedScopeDryRun) {
		return nil, err
	}
	if !req.DryRun {
		for _, plan := range report.Plans {
			InvalidateSubscriptionPlanCache(plan.Id)
		}
	}
	return report, nil
}

func convertLegacySharedPlansTx(tx *gorm.DB, req SharedScopeConversionRequest, groupRatios map[string]float64, defaultGroup string, report *SharedScopeConversionReport) error {
	var plans []SubscriptionPlan
	if err := dbx.LockForUpdate(tx).
		Order("id asc").
		Find(&plans).Error; err != nil {
		return err
	}
	for i := range plans {
		plan := &plans[i]
		scope, measure := planLimitAxes(plan)
		if scope != SubscriptionScopeAll {
			// Legacy request plans already resolve to model/count; their
			// counters are the entitlement and there is no shared pool to move.
			continue
		}
		if measure == SubscriptionMeasureCount {
			report.skip("plan", plan.Id, "shared scope with count measure is not convertible")
			continue
		}
		if NormalizeResetPeriod(plan.QuotaResetPeriod) != SubscriptionResetNever {
			report.skip("plan", plan.Id, "quota reset cycle cannot be expressed in group scope")
			continue
		}
		if plan.TotalAmount <= 0 {
			report.skip("plan", plan.Id, "unlimited quota plans need a positive per-group amount")
			continue
		}
		target, reason := sharedScopePlanTarget(plan, req.PlanGroups, groupRatios)
		if target == "" {
			report.skip("plan", plan.Id, reason)
			continue
		}
		expireMode := normalizeExpireMode(plan.ExpireMode, plan.Kind)
		kind := deriveSubscriptionKind(SubscriptionScopeGroup, SubscriptionMeasureQuota, expireMode)
		rule := SubscriptionRequestRule{
			Group:         target,
			Measure:       SubscriptionMeasureQuota,
			Amount:        plan.TotalAmount,
			AmountDisplay: QuotaToDisplayAmount64(plan.TotalAmount),
		}
		updates := map[string]interface{}{
			"scope":                      SubscriptionScopeGroup,
			"measure":                    SubscriptionMeasureQuota,
			"kind":                       kind,
			"expire_mode":                expireMode,
			"total_amount":               0,
			"quota_reset_period":         SubscriptionResetNever,
			"quota_reset_custom_seconds": 0,
			"request_rules":              marshalRequestRulesJSON([]SubscriptionRequestRule{rule}),
			"upgrade_group":              target,
			"upgrade_groups":             marshalStringListJSON([]string{target}),
			"updated_at":                 common.GetTimestamp(),
		}
		if err := tx.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(updates).Error; err != nil {
			return err
		}
		report.Plans = append(report.Plans, SharedScopeConversionPlan{
			Id:          plan.Id,
			Title:       plan.Title,
			TargetGroup: target,
			Amount:      plan.TotalAmount,
		})
	}
	return nil
}

func convertLegacySharedSubscriptionsTx(tx *gorm.DB, _ SharedScopeConversionRequest, groupRatios map[string]float64, defaultGroup string, report *SharedScopeConversionReport) error {
	var subs []UserSubscription
	if err := dbx.LockForUpdate(tx).
		Order("id asc").
		Find(&subs).Error; err != nil {
		return err
	}
	for i := range subs {
		sub := &subs[i]
		scope, measure := subscriptionLimitAxes(sub)
		if scope != SubscriptionScopeAll {
			continue
		}
		if measure == SubscriptionMeasureCount {
			report.skip("subscription", sub.Id, "shared scope with count measure is not convertible")
			continue
		}
		target, reason := sharedScopeSubscriptionTarget(tx, sub, groupRatios, defaultGroup)
		if target == "" {
			report.skip("subscription", sub.Id, reason)
			continue
		}
		total := sub.AmountTotal
		used := sub.AmountUsed
		if total > 0 && used > total {
			used = total
		}
		if total == 0 {
			// amount_total=0 meant "unlimited" on the shared pool; the counter
			// keeps that meaning (total=0 is unbounded for quota rows).
			used = 0
		}
		if err := upsertSharedScopeCounterTx(tx, sub.Id, target, total, used); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"scope":           SubscriptionScopeGroup,
			"measure":         SubscriptionMeasureQuota,
			"amount_total":    0,
			"amount_used":     0,
			"last_reset_time": 0,
			"next_reset_time": 0,
			"upgrade_groups":  marshalStringListJSON([]string{target}),
			"updated_at":      common.GetTimestamp(),
		}
		if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(updates).Error; err != nil {
			return err
		}
		report.Subscriptions = append(report.Subscriptions, SharedScopeConversionSubscription{
			Id:          sub.Id,
			UserId:      sub.UserId,
			PlanId:      sub.PlanId,
			TargetGroup: target,
			Total:       total,
			Used:        used,
		})
	}
	return nil
}

func sharedScopePlanTarget(plan *SubscriptionPlan, overrides map[int]string, groupRatios map[string]float64) (string, string) {
	if override, ok := overrides[plan.Id]; ok {
		override = strings.TrimSpace(override)
		if override == "" {
			return "", "explicit plan group override is empty"
		}
		if _, ok := groupRatios[override]; !ok {
			return "", fmt.Sprintf("override group %q is not configured", override)
		}
		return override, ""
	}
	if group := strings.TrimSpace(plan.UpgradeGroup); group != "" {
		if _, ok := groupRatios[group]; ok {
			return group, ""
		}
	}
	for _, group := range plan.UpgradeGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := groupRatios[group]; ok {
			return group, ""
		}
	}
	// A plan with no upgrade group cannot pick one without changing who the
	// plan upgrades; make the operator choose explicitly.
	return "", "no upgrade group; pass plan_groups to choose the target group"
}

func sharedScopeSubscriptionTarget(tx *gorm.DB, sub *UserSubscription, groupRatios map[string]float64, defaultGroup string) (string, string) {
	if group := strings.TrimSpace(sub.UpgradeGroup); group != "" {
		if _, ok := groupRatios[group]; ok {
			return group, ""
		}
	}
	for _, group := range sub.UpgradeGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := groupRatios[group]; ok {
			return group, ""
		}
	}
	if userGroup, err := getUserGroupByIdTx(tx, sub.UserId); err == nil {
		if _, ok := groupRatios[userGroup]; ok {
			return userGroup, ""
		}
	}
	if _, ok := groupRatios[defaultGroup]; ok {
		return defaultGroup, ""
	}
	return "", "no configured group available"
}

func upsertSharedScopeCounterTx(tx *gorm.DB, subId int, group string, total int64, used int64) error {
	var counters []SubscriptionRequestCounter
	if err := dbx.LockForUpdate(tx).
		Where("user_subscription_id = ?", subId).
		Find(&counters).Error; err != nil {
		return err
	}
	if len(counters) == 0 {
		return tx.Create(&SubscriptionRequestCounter{
			UserSubscriptionId: subId,
			GroupName:          group,
			ModelName:          "",
			Measure:            SubscriptionMeasureQuota,
			Total:              total,
			Used:               used,
		}).Error
	}
	// A shared-pool row should not own entitlement rows. If a previous partial
	// conversion left one, reuse it only when it is unambiguous.
	if len(counters) > 1 {
		return fmt.Errorf("subscription %d already has %d entitlement rows; refusing to merge", subId, len(counters))
	}
	counter := counters[0]
	counter.GroupName = group
	counter.ModelName = ""
	counter.Measure = SubscriptionMeasureQuota
	counter.Total = total
	counter.Used = used
	counter.QuotaTotal = 0
	counter.QuotaUsed = 0
	return tx.Save(&counter).Error
}

// AdminConvertLegacySharedScope is the one-time migration endpoint. Run it
// with dry_run=true first, inspect the report, then apply with dry_run=false.
func AdminConvertLegacySharedScope(c contract.Context) {
	var req SharedScopeConversionRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	report, err := ConvertLegacySharedScopes(req)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, report)
}
