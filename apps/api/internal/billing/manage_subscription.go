package billing

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"math"
	"strconv"
	"strings"

	channel "github.com/QuantumNous/new-api/internal/catalog"
	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"gorm.io/gorm"
)

// ---- Shared types ----

type SubscriptionPlanDTO struct {
	Plan SubscriptionPlan `json:"plan"`
}

type BillingPreferenceRequest struct {
	BillingPreference string `json:"billing_preference"`
}

type SubscriptionBalancePayRequest struct {
	PlanId int `json:"plan_id"`
}

// ---- User APIs ----

func GetSubscriptionPlans(c contract.Context) {
	if !IsPaymentComplianceConfirmed() {
		common.CtxApiSuccess(c, []SubscriptionPlanDTO{})
		return
	}

	var plans []SubscriptionPlan
	if err := dbx.DB.Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.CtxApiSuccess(c, result)
}

func GetSubscriptionSelf(c contract.Context) {
	userId := c.GetInt("id")
	settingMap, _ := identity.GetUserSetting(userId, false)
	pref := common.NormalizeBillingPreference(settingMap.BillingPreference)

	// Get all subscriptions (including expired)
	allSubscriptions, err := GetAllUserSubscriptions(userId)
	if err != nil {
		allSubscriptions = []SubscriptionSummary{}
	}

	// Get active subscriptions for backward compatibility
	activeSubscriptions, err := GetAllActiveUserSubscriptions(userId)
	if err != nil {
		activeSubscriptions = []SubscriptionSummary{}
	}

	common.CtxApiSuccess(c, common.H{
		"billing_preference": pref,
		"subscriptions":      activeSubscriptions, // all active subscriptions
		"all_subscriptions":  allSubscriptions,    // all subscriptions including expired
	})
}

func UpdateSubscriptionPreference(c contract.Context) {
	userId := c.GetInt("id")
	var req BillingPreferenceRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	pref := common.NormalizeBillingPreference(req.BillingPreference)

	user, err := identity.GetUserById(userId, true)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	current := user.GetSetting()
	current.BillingPreference = pref
	if err := identity.UpdateUserSetting(user.Id, current); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, common.H{"billing_preference": pref})
}

func SubscriptionRequestBalancePay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	userId := c.GetInt("id")
	var req SubscriptionBalancePayRequest
	if err := c.BindJSON(&req); err != nil || req.PlanId <= 0 {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}

	if err := PurchaseSubscriptionWithWallet(userId, req.PlanId); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}

// ---- Admin APIs ----

func AdminListSubscriptionPlans(c contract.Context) {
	var plans []SubscriptionPlan
	if err := dbx.DB.Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.CtxApiSuccess(c, result)
}

type AdminUpsertSubscriptionPlanRequest struct {
	Plan SubscriptionPlan `json:"plan"`
}

// applyPlanUpgradeGroups normalizes the multi-group snapshot, validates every
// group against the configured ratios and mirrors the first one into the
// legacy UpgradeGroup column. Empty input stays empty (unrestricted).
func applyPlanUpgradeGroups(plan *SubscriptionPlan) error {
	groups := normalizeStringList(plan.UpgradeGroups)
	if len(groups) == 0 && strings.TrimSpace(plan.UpgradeGroupsJSON) != "" {
		parsed, err := parseStringListJSON(plan.UpgradeGroupsJSON)
		if err != nil {
			return errors.New("升级分组格式错误")
		}
		groups = normalizeStringList(parsed)
	}
	if len(groups) == 0 && strings.TrimSpace(plan.UpgradeGroup) != "" {
		groups = normalizeStringList([]string{plan.UpgradeGroup})
	}
	groupRatios := ratio_setting.GetGroupRatioCopy()
	for _, group := range groups {
		if _, ok := groupRatios[group]; !ok {
			return fmt.Errorf("升级分组不存在: %s", group)
		}
	}
	plan.UpgradeGroups = groups
	plan.UpgradeGroupsJSON = marshalStringListJSON(groups)
	plan.UpgradeGroup = ""
	if len(groups) > 0 {
		plan.UpgradeGroup = groups[0]
	}
	return nil
}

// ruleLabel names the colliding key in a duplicate-rule error so the operator
// sees which model (or group) is configured twice.
func ruleLabel(rule SubscriptionRequestRule, measure string) string {
	if len(rule.Models) > 0 {
		return strings.Join(normalizeStringList(rule.Models), ", ")
	}
	if measure == SubscriptionMeasureQuota {
		return "金额"
	}
	return "次数"
}

// applyPlanRequestRules validates and serializes request rules for request-kind
// plans. Other kinds drop any stale rules so they cannot grant an entitlement.
func applyPlanRequestRules(plan *SubscriptionPlan) error {
	scope, measure := planLimitAxes(plan)
	if scope == SubscriptionScopeAll {
		plan.RequestRules = nil
		plan.RequestRulesJSON = ""
		return nil
	}
	rules := plan.RequestRules
	if len(rules) == 0 && strings.TrimSpace(plan.RequestRulesJSON) != "" {
		parsed, err := parseRequestRulesJSON(plan.RequestRulesJSON)
		if err != nil {
			return errors.New("请求规则格式错误")
		}
		rules = parsed
	}
	if len(rules) == 0 {
		return errors.New("限制规则至少需要一条")
	}
	groupRatios := ratio_setting.GetGroupRatioCopy()
	normalized := make([]SubscriptionRequestRule, 0, len(rules))
	// The (group, model) pair is the entitlement key, so configuring it twice
	// would silently grant the sum of both amounts under a single row.
	seenKeys := make(map[string]struct{}, len(rules)*2)
	for _, rule := range rules {
		group := strings.TrimSpace(rule.Group)
		if _, ok := groupRatios[group]; !ok {
			return fmt.Errorf("请求规则分组不存在: %s", group)
		}
		var models []string
		if scope == SubscriptionScopeModel {
			models = normalizeStringList(rule.Models)
			if len(models) == 0 {
				return errors.New("请求规则模型不能为空")
			}
		}
		normalizedRule := SubscriptionRequestRule{Group: group, Models: models, Measure: measure}
		if measure == SubscriptionMeasureQuota {
			amount := rule.Amount
			if rule.AmountDisplay != 0 {
				converted := int64(QuotaFromDisplayAmount(rule.AmountDisplay))
				if converted <= 0 {
					return errors.New("限制额度必须大于0")
				}
				amount = converted
			}
			if amount <= 0 {
				return errors.New("限制额度必须大于0")
			}
			normalizedRule.Amount = amount
		} else {
			if rule.Count <= 0 {
				return errors.New("请求规则次数必须大于0")
			}
			normalizedRule.Count = rule.Count
		}
		keys := models
		if scope == SubscriptionScopeGroup {
			keys = []string{""}
		}
		for _, model := range keys {
			key := group + "\x00" + model
			if _, dup := seenKeys[key]; dup {
				return fmt.Errorf("请求规则中分组 %s 下的同一限制不能重复设置: %v", group, ruleLabel(rule, measure))
			}
			seenKeys[key] = struct{}{}
		}
		normalized = append(normalized, normalizedRule)
	}
	plan.RequestRules = normalized
	plan.RequestRulesJSON = marshalRequestRulesJSON(normalized)
	return nil
}

// applySubscriptionKindConstraints enforces the kind-specific field contract.
func applySubscriptionKindConstraints(plan *SubscriptionPlan) error {
	// Quota-expiry plans end exactly when their pool or counters are spent, so
	// there is no "after the quota runs out" window to cover from the wallet.
	// Wallet fallback is a term-scoped add-on: force it off there so the
	// snapshot cannot promise something the product never does.
	if normalizeExpireMode(plan.ExpireMode, plan.Kind) == SubscriptionExpireModeQuota {
		plan.AllowWalletOverflow = common.GetPointer(false)
	}
	if scope, _ := planLimitAxes(plan); scope != SubscriptionScopeAll {
		// Scoped plans sell entitlement rows, not a shared pool.
		plan.TotalAmount = 0
		plan.QuotaResetPeriod = SubscriptionResetNever
		return nil
	}
	switch plan.Kind {
	case SubscriptionKindQuota:
		if plan.TotalAmount <= 0 {
			return errors.New("额度订阅的总额度必须大于0")
		}
		plan.QuotaResetPeriod = SubscriptionResetNever
	case SubscriptionKindRequest:
		// Request-kind plans sell counts, not a quota pool or a term.
		plan.TotalAmount = 0
		plan.QuotaResetPeriod = SubscriptionResetNever
	}
	return nil
}

// resolvePlanModes collapses the admin form's axes into the stored kind and
// rejects terms that purchase could not compute. Payloads that only carry the
// legacy kind keep its meaning.
func resolvePlanModes(plan *SubscriptionPlan) error {
	if strings.TrimSpace(plan.Kind) != "" && normalizeSubscriptionKind(plan.Kind) != strings.TrimSpace(plan.Kind) {
		return errors.New("无效的订阅种类")
	}
	limitMode := normalizeLimitMode(plan.LimitMode, plan.Kind)
	expireMode := normalizeExpireMode(plan.ExpireMode, plan.Kind)
	scope := normalizeScope(plan.Scope)
	measure := normalizeMeasure(plan.Measure)
	if scope == "" {
		// Legacy payload: limit_mode carries the only scope signal it had.
		if limitMode == SubscriptionLimitModeRequest {
			scope, measure = SubscriptionScopeModel, SubscriptionMeasureCount
		} else {
			scope, measure = SubscriptionScopeAll, SubscriptionMeasureQuota
		}
	} else if measure == "" {
		measure = SubscriptionMeasureCount
	}
	plan.Scope = scope
	plan.Measure = measure
	plan.ExpireMode = expireMode
	plan.LimitMode = limitMode
	// The stored kind stays the legacy discriminator the runtime and the
	// existing rows understand: request means per-model counts, everything
	// else follows the expiry axis.
	plan.Kind = deriveSubscriptionKind(scope, measure, expireMode)
	if expireMode == SubscriptionExpireModeTime && plan.DurationUnit == SubscriptionDurationCustom && plan.CustomSeconds <= 0 {
		return errors.New("自定义时长需大于0秒")
	}
	return nil
}

func AdminCreateSubscriptionPlan(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	var req AdminUpsertSubscriptionPlanRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	req.Plan.Id = 0
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.CtxApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if err := resolvePlanModes(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.CtxApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.CtxApiErrorMsg(c, "价格不能超过9999")
		return
	}
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	if req.Plan.AllowBalancePay == nil {
		req.Plan.AllowBalancePay = common.GetPointer(true)
	}
	if req.Plan.AllowWalletOverflow == nil {
		req.Plan.AllowWalletOverflow = common.GetPointer(true)
	}
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.CtxApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	// The admin form submits the total in display currency; the server owns
	// the conversion so the frontend never multiplies by QuotaPerUnit.
	if req.Plan.TotalAmountDisplay != 0 {
		if math.IsNaN(req.Plan.TotalAmountDisplay) || math.IsInf(req.Plan.TotalAmountDisplay, 0) {
			common.CtxApiErrorMsg(c, "无效的总额度数值")
			return
		}
		req.Plan.TotalAmount = int64(QuotaFromDisplayAmount(req.Plan.TotalAmountDisplay))
	}
	if req.Plan.TotalAmount < 0 {
		common.CtxApiErrorMsg(c, "总额度不能为负数")
		return
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.CtxApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	if err := applySubscriptionKindConstraints(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	req.Plan.QuotaResetPeriod = NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.CtxApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}
	if err := applyPlanUpgradeGroups(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := applyPlanRequestRules(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := validateFreePlanPurchaseLimit(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	err := dbx.DB.Create(&req.Plan).Error
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	InvalidateSubscriptionPlanCache(req.Plan.Id)
	common.CtxApiSuccess(c, req.Plan)
}

func AdminUpdateSubscriptionPlan(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.CtxApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	var existing SubscriptionPlan
	if err := dbx.DB.Where("id = ?", id).First(&existing).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.CtxApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if strings.TrimSpace(req.Plan.Kind) == "" {
		req.Plan.Kind = normalizeSubscriptionKind(existing.Kind)
	}
	// Omitted expire_mode keeps the stored term; an explicit empty clears it
	// back to the kind default.
	if strings.TrimSpace(req.Plan.ExpireMode) == "" && strings.TrimSpace(existing.ExpireMode) != "" {
		req.Plan.ExpireMode = existing.ExpireMode
	}
	// Omitted limit axes keep the stored ones: a partial update (price, copy)
	// must not silently widen a group-scoped plan into an all-scope plan.
	if strings.TrimSpace(req.Plan.Scope) == "" {
		req.Plan.Scope = existing.Scope
	}
	if strings.TrimSpace(req.Plan.Measure) == "" {
		req.Plan.Measure = existing.Measure
	}
	if err := resolvePlanModes(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.CtxApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.CtxApiErrorMsg(c, "价格不能超过9999")
		return
	}
	req.Plan.Id = id
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.CtxApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	// The admin form submits the total in display currency; the server owns
	// the conversion so the frontend never multiplies by QuotaPerUnit.
	if req.Plan.TotalAmountDisplay != 0 {
		if math.IsNaN(req.Plan.TotalAmountDisplay) || math.IsInf(req.Plan.TotalAmountDisplay, 0) {
			common.CtxApiErrorMsg(c, "无效的总额度数值")
			return
		}
		req.Plan.TotalAmount = int64(QuotaFromDisplayAmount(req.Plan.TotalAmountDisplay))
	}
	if req.Plan.TotalAmount < 0 {
		common.CtxApiErrorMsg(c, "总额度不能为负数")
		return
	}
	// A payload that carries neither amount field keeps the stored pool: zero
	// means unlimited, so writing it on a partial update would silently drop
	// the plan's quota.
	if req.Plan.TotalAmount == 0 && req.Plan.TotalAmountDisplay == 0 {
		req.Plan.TotalAmount = existing.TotalAmount
		req.Plan.TotalAmountDisplay = existing.TotalAmountDisplay
	}
	// Omitted upgrade_groups / request_rules mean "keep the current values"; an
	// explicitly empty array clears them.
	if req.Plan.UpgradeGroups == nil && strings.TrimSpace(req.Plan.UpgradeGroupsJSON) == "" {
		if len(existing.UpgradeGroups) > 0 {
			req.Plan.UpgradeGroups = existing.UpgradeGroups
		} else if strings.TrimSpace(req.Plan.UpgradeGroup) == "" {
			req.Plan.UpgradeGroup = existing.UpgradeGroup
		}
	}
	if req.Plan.RequestRules == nil && strings.TrimSpace(req.Plan.RequestRulesJSON) == "" {
		req.Plan.RequestRules = existing.RequestRules
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.CtxApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	if err := applySubscriptionKindConstraints(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	req.Plan.QuotaResetPeriod = NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.CtxApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}
	if err := applyPlanUpgradeGroups(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := applyPlanRequestRules(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}

	if err := validateFreePlanPurchaseLimit(&req.Plan); err != nil {
		common.CtxApiError(c, err)
		return
	}
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		// update plan (allow zero values updates with map)
		updateMap := map[string]interface{}{
			"title":                      req.Plan.Title,
			"subtitle":                   req.Plan.Subtitle,
			"kind":                       req.Plan.Kind,
			"expire_mode":                req.Plan.ExpireMode,
			"scope":                      req.Plan.Scope,
			"measure":                    req.Plan.Measure,
			"price_amount":               req.Plan.PriceAmount,
			"currency":                   req.Plan.Currency,
			"duration_unit":              req.Plan.DurationUnit,
			"duration_value":             req.Plan.DurationValue,
			"custom_seconds":             req.Plan.CustomSeconds,
			"enabled":                    req.Plan.Enabled,
			"sort_order":                 req.Plan.SortOrder,
			"stripe_price_id":            req.Plan.StripePriceId,
			"creem_product_id":           req.Plan.CreemProductId,
			"waffo_pancake_product_id":   req.Plan.WaffoPancakeProductId,
			"max_purchase_per_user":      req.Plan.MaxPurchasePerUser,
			"total_amount":               req.Plan.TotalAmount,
			"upgrade_group":              req.Plan.UpgradeGroup,
			"upgrade_groups":             req.Plan.UpgradeGroupsJSON,
			"request_rules":              req.Plan.RequestRulesJSON,
			"downgrade_group":            req.Plan.DowngradeGroup,
			"quota_reset_period":         req.Plan.QuotaResetPeriod,
			"quota_reset_custom_seconds": req.Plan.QuotaResetCustomSeconds,
			"updated_at":                 common.GetTimestamp(),
		}
		if req.Plan.AllowBalancePay != nil {
			updateMap["allow_balance_pay"] = *req.Plan.AllowBalancePay
		}
		if req.Plan.AllowWalletOverflow != nil {
			updateMap["allow_wallet_overflow"] = *req.Plan.AllowWalletOverflow
		}
		if err := tx.Model(&SubscriptionPlan{}).Where("id = ?", id).Updates(updateMap).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	InvalidateSubscriptionPlanCache(id)
	common.CtxApiSuccess(c, nil)
}

type AdminUpdateSubscriptionPlanStatusRequest struct {
	Enabled *bool `json:"enabled"`
}

// AdminListGroupModels returns the models enabled for a group, feeding the
// request-rule model picker.
func AdminListGroupModels(c contract.Context) {
	group := strings.TrimSpace(c.Query("group"))
	if group == "" {
		common.CtxApiErrorMsg(c, "分组不能为空")
		return
	}
	if _, ok := ratio_setting.GetGroupRatioCopy()[group]; !ok {
		common.CtxApiErrorMsg(c, "分组不存在")
		return
	}
	common.CtxApiSuccess(c, channel.GetGroupsEnabledModels([]string{group}))
}

func AdminUpdateSubscriptionPlanStatus(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.CtxApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpdateSubscriptionPlanStatusRequest
	if err := c.BindJSON(&req); err != nil || req.Enabled == nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	if err := dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", id).Update("enabled", *req.Enabled).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	InvalidateSubscriptionPlanCache(id)
	common.CtxApiSuccess(c, nil)
}

type AdminBindSubscriptionRequest struct {
	UserId int `json:"user_id"`
	PlanId int `json:"plan_id"`
}

func AdminBindSubscription(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	var req AdminBindSubscriptionRequest
	if err := c.BindJSON(&req); err != nil || req.UserId <= 0 || req.PlanId <= 0 {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	msg, err := AdminBindSubscriptionRecord(req.UserId, req.PlanId, "")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg != "" {
		common.CtxApiSuccess(c, common.H{"message": msg})
		return
	}
	common.CtxApiSuccess(c, nil)
}

// ---- Admin: user subscription management ----

func AdminListUserSubscriptions(c contract.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.CtxApiErrorMsg(c, "无效的用户ID")
		return
	}
	subs, err := GetAllUserSubscriptions(userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, subs)
}

type AdminCreateUserSubscriptionRequest struct {
	PlanId int `json:"plan_id"`
}

type AdminResetSubscriptionRequest struct {
	PlanId           int   `json:"plan_id"`
	AdvanceResetTime *bool `json:"advance_reset_time"`
}

func resolveAdvanceResetTime(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func recordSubscriptionResetUserLogs(result *SubscriptionResetResult, adminInfo map[string]interface{}) {
	if result == nil || result.ResetCount == 0 {
		return
	}
	content := fmt.Sprintf("管理员重置订阅套餐 %s（ID: %d）额度", result.PlanTitle, result.PlanId)
	for _, userId := range result.AffectedUserIds {
		usage.RecordLogWithAdminInfo(userId, usage.LogTypeManage, content, adminInfo)
	}
}

// AdminCreateUserSubscription creates a new user subscription from a plan (no payment).
func AdminCreateUserSubscription(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.CtxApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminCreateUserSubscriptionRequest
	if err := c.BindJSON(&req); err != nil || req.PlanId <= 0 {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	msg, err := AdminBindSubscriptionRecord(userId, req.PlanId, "")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg != "" {
		common.CtxApiSuccess(c, common.H{"message": msg})
		return
	}
	common.CtxApiSuccess(c, nil)
}

func AdminResetUserSubscriptionsByPlan(c contract.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.CtxApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	if req.PlanId <= 0 {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := AdminResetUserSubscriptionsByPlanRecord(userId, req.PlanId, advanceResetTime)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(result, usage.AuditOperatorInfo(c))
	usage.RecordManageAuditFor(c, userId, "subscription.user_plan_reset", map[string]interface{}{
		"target_user_id":     userId,
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.CtxApiSuccess(c, result)
}

func AdminResetPlanSubscriptions(c contract.Context) {
	planId, _ := strconv.Atoi(c.Param("id"))
	if planId <= 0 {
		common.CtxApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := AdminResetPlanSubscriptionsRecord(planId, advanceResetTime)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(result, usage.AuditOperatorInfo(c))
	common.SysLog(fmt.Sprintf("admin reset subscription plan %d quota: reset_count=%d user_count=%d advance_reset_time=%t",
		result.PlanId, result.ResetCount, result.UserCount, result.AdvanceResetTime))
	usage.RecordManageAudit(c, "subscription.plan_reset", map[string]interface{}{
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.CtxApiSuccess(c, result)
}

// AdminInvalidateUserSubscription cancels a user subscription immediately.
func AdminInvalidateUserSubscription(c contract.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.CtxApiErrorMsg(c, "无效的订阅ID")
		return
	}
	msg, err := invalidateUserSubscriptionRecord(subId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg != "" {
		common.CtxApiSuccess(c, common.H{"message": msg})
		return
	}
	common.CtxApiSuccess(c, nil)
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(c contract.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.CtxApiErrorMsg(c, "无效的订阅ID")
		return
	}
	msg, err := deleteUserSubscriptionRecord(subId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg != "" {
		common.CtxApiSuccess(c, common.H{"message": msg})
		return
	}
	common.CtxApiSuccess(c, nil)
}

// ---------------------------------------------------------------------------
// General setting (from manage_subscription/configure_general.go)
// ---------------------------------------------------------------------------

// 额度展示类型
const (
	QuotaDisplayTypeUSD    = "USD"
	QuotaDisplayTypeCNY    = "CNY"
	QuotaDisplayTypeTokens = "TOKENS"
	QuotaDisplayTypeCustom = "CUSTOM"
)

type GeneralSetting struct {
	DocsLink            string `json:"docs_link"`
	PingIntervalEnabled bool   `json:"ping_interval_enabled"`
	PingIntervalSeconds int    `json:"ping_interval_seconds"`
	// 当前站点额度展示类型：USD / CNY / TOKENS
	QuotaDisplayType string `json:"quota_display_type"`
	// 自定义货币符号，用于 CUSTOM 展示类型
	CustomCurrencySymbol string `json:"custom_currency_symbol"`
	// 自定义货币与美元汇率（1 USD = X Custom）
	CustomCurrencyExchangeRate float64 `json:"custom_currency_exchange_rate"`
	// 支付金额名称（充值/买套餐等支付侧金额），独立于消费货币符号。
	// 仅在 AmountUnit 为 "custom" 时生效。
	AmountName string `json:"amount_name"`
	// 支付金额单位：usd($) / cny(¥) / custom(自定义名称)。空历史值按 usd 处理。
	AmountUnit string `json:"amount_unit"`
}

// 默认配置
var generalSetting = GeneralSetting{
	DocsLink:                   "https://docs.newapi.pro",
	PingIntervalEnabled:        true,
	PingIntervalSeconds:        10,
	QuotaDisplayType:           QuotaDisplayTypeUSD,
	CustomCurrencyExchangeRate: 1.0,
	AmountName:                 "",
	AmountUnit:                 "usd",
}

func init() {
	// 注册到全局配置管理器
	settings.GlobalConfig.Register("general_setting", &generalSetting)
}

func GetGeneralSetting() *GeneralSetting {
	return &generalSetting
}

// AmountUnitEffective returns the payment amount unit: "usd", "cny", or
// "custom". "custom" labels payment amounts with AmountName instead of a
// currency symbol; empty and unrecognized values fall back to "usd".
func (g *GeneralSetting) AmountUnitEffective() string {
	switch g.AmountUnit {
	case "cny", "custom":
		return g.AmountUnit
	default:
		return "usd"
	}
}

// IsCurrencyDisplay 是否以货币形式展示（美元或人民币）
func IsCurrencyDisplay() bool {
	return generalSetting.QuotaDisplayType != QuotaDisplayTypeTokens
}

// IsCNYDisplay 是否以人民币展示
func IsCNYDisplay() bool {
	return generalSetting.QuotaDisplayType == QuotaDisplayTypeCNY
}

// GetQuotaDisplayType 返回额度展示类型
func GetQuotaDisplayType() string {
	return generalSetting.QuotaDisplayType
}

// GetCurrencySymbol 返回当前展示类型对应符号
func GetCurrencySymbol() string {
	switch generalSetting.QuotaDisplayType {
	case QuotaDisplayTypeUSD:
		return "$"
	case QuotaDisplayTypeCNY:
		return "¥"
	case QuotaDisplayTypeCustom:
		if generalSetting.CustomCurrencySymbol != "" {
			return generalSetting.CustomCurrencySymbol
		}
		return "¤"
	default:
		return ""
	}
}

// GetUsdToCurrencyRate 返回 1 USD = X <currency> 的 X（TOKENS 不适用）
func GetUsdToCurrencyRate(usdToCny float64) float64 {
	switch generalSetting.QuotaDisplayType {
	case QuotaDisplayTypeUSD:
		return 1
	case QuotaDisplayTypeCNY:
		return usdToCny
	case QuotaDisplayTypeCustom:
		if generalSetting.CustomCurrencyExchangeRate > 0 {
			return generalSetting.CustomCurrencyExchangeRate
		}
		return 1
	default:
		return 1
	}
}
