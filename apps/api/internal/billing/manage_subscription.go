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

// applyPlanRequestRules validates and serializes request rules for request-kind
// plans. Other kinds drop any stale rules so they cannot grant an entitlement.
func applyPlanRequestRules(plan *SubscriptionPlan) error {
	if plan.Kind != SubscriptionKindRequest {
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
		return errors.New("请求次数订阅至少需要一条请求规则")
	}
	groupRatios := ratio_setting.GetGroupRatioCopy()
	normalized := make([]SubscriptionRequestRule, 0, len(rules))
	for _, rule := range rules {
		group := strings.TrimSpace(rule.Group)
		if _, ok := groupRatios[group]; !ok {
			return fmt.Errorf("请求规则分组不存在: %s", group)
		}
		models := normalizeStringList(rule.Models)
		if len(models) == 0 {
			return errors.New("请求规则模型不能为空")
		}
		if rule.Count <= 0 {
			return errors.New("请求规则次数必须大于0")
		}
		normalized = append(normalized, SubscriptionRequestRule{
			Group:  group,
			Models: models,
			Count:  rule.Count,
		})
	}
	plan.RequestRules = normalized
	plan.RequestRulesJSON = marshalRequestRulesJSON(normalized)
	return nil
}

// applySubscriptionKindConstraints enforces the kind-specific field contract.
func applySubscriptionKindConstraints(plan *SubscriptionPlan) error {
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
	requestedKind := normalizeSubscriptionKind(req.Plan.Kind)
	if strings.TrimSpace(req.Plan.Kind) != "" && requestedKind != strings.TrimSpace(req.Plan.Kind) {
		common.CtxApiErrorMsg(c, "无效的订阅种类")
		return
	}
	req.Plan.Kind = requestedKind
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
	} else {
		requestedKind := normalizeSubscriptionKind(req.Plan.Kind)
		if requestedKind != strings.TrimSpace(req.Plan.Kind) {
			common.CtxApiErrorMsg(c, "无效的订阅种类")
			return
		}
		req.Plan.Kind = requestedKind
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
