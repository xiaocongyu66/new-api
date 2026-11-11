package billing

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/usage"
	"strconv"
	"strings"
	"sync"
	"time"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/cachex"
	"github.com/samber/hot"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Subscription duration units
const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"
)

// Subscription quota reset period
const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
)

// Subscription kinds decide how a subscription ends and what it grants.
const (
	SubscriptionKindTime    = "time"
	SubscriptionKindQuota   = "quota"
	SubscriptionKindRequest = "request"
)

// The admin form exposes two orthogonal axes instead of the kind dropdown:
// the limit mode picks what the plan grants (a group-scoped quota pool or
// per-(group, model) request counts) and is stored as Kind; the expiry mode
// picks how it ends (at end_time or when the granted resource runs out) and is
// stored as ExpireMode.
const (
	SubscriptionExpireModeTime  = "time"
	SubscriptionExpireModeQuota = "quota"

	SubscriptionLimitModeGroup   = "group"
	SubscriptionLimitModeRequest = "request"
)

var (
	ErrSubscriptionOrderNotFound      = errors.New("subscription order not found")
	ErrSubscriptionOrderStatusInvalid = errors.New("subscription order status invalid")
)

const (
	subscriptionPlanCacheNamespace     = "new-api:subscription_plan:v1"
	subscriptionPlanInfoCacheNamespace = "new-api:subscription_plan_info:v1"
)

var (
	subscriptionPlanCacheOnce     sync.Once
	subscriptionPlanInfoCacheOnce sync.Once

	subscriptionPlanCache     *cachex.HybridCache[SubscriptionPlan]
	subscriptionPlanInfoCache *cachex.HybridCache[SubscriptionPlanInfo]
)

func subscriptionPlanCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_CAP", 5000)
	if capacity <= 0 {
		capacity = 5000
	}
	return capacity
}

func getSubscriptionPlanCache() *cachex.HybridCache[SubscriptionPlan] {
	subscriptionPlanCacheOnce.Do(func() {
		ttl := subscriptionPlanCacheTTL()
		subscriptionPlanCache = cachex.NewHybridCache[SubscriptionPlan](cachex.HybridCacheConfig[SubscriptionPlan]{
			Namespace: cachex.Namespace(subscriptionPlanCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlan]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlan] {
				return hot.NewHotCache[string, SubscriptionPlan](hot.LRU, subscriptionPlanCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanCache
}

func subscriptionPlanCacheKey(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func normalizeSubscriptionKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case SubscriptionKindQuota, SubscriptionKindRequest, SubscriptionKindTime:
		return strings.TrimSpace(kind)
	default:
		return SubscriptionKindTime
	}
}

// normalizeExpireMode resolves the stored expiry axis. A stored value wins;
// rows written before the column existed fall back to the kind's historical
// behaviour so nothing already sold changes meaning.
func normalizeExpireMode(raw string, kind string) string {
	switch strings.TrimSpace(raw) {
	case SubscriptionExpireModeTime, SubscriptionExpireModeQuota:
		return strings.TrimSpace(raw)
	}
	switch normalizeSubscriptionKind(kind) {
	case SubscriptionKindQuota, SubscriptionKindRequest:
		return SubscriptionExpireModeQuota
	default:
		return SubscriptionExpireModeTime
	}
}

// normalizeLimitMode resolves the limit axis the payload asked for. An empty
// payload (e.g. a legacy caller that only sends kind) keeps its old meaning.
func normalizeLimitMode(raw string, kind string) string {
	switch strings.TrimSpace(raw) {
	case SubscriptionLimitModeGroup, SubscriptionLimitModeRequest:
		return strings.TrimSpace(raw)
	}
	if normalizeSubscriptionKind(kind) == SubscriptionKindRequest {
		return SubscriptionLimitModeRequest
	}
	return SubscriptionLimitModeGroup
}

// deriveSubscriptionKind collapses the limit axes into the single kind the
// runtime and the legacy rows understand: request means per-model counts,
// everything else follows the expiry axis.
func deriveSubscriptionKind(scope string, measure string, expireMode string) string {
	if scope == SubscriptionScopeModel && measure == SubscriptionMeasureCount {
		return SubscriptionKindRequest
	}
	if expireMode == SubscriptionExpireModeQuota {
		return SubscriptionKindQuota
	}
	return SubscriptionKindTime
}

// The entitlement scopes: a shared pool (legacy pool kinds), one row per
// group, or one row per (group, model).
const (
	SubscriptionScopeAll   = "all"
	SubscriptionScopeGroup = "group"
	SubscriptionScopeModel = "model"
)

// The entitlement measures: flat request counts or quota units.
const (
	SubscriptionMeasureQuota = "quota"
	SubscriptionMeasureCount = "count"
)

func normalizeScope(raw string) string {
	switch strings.TrimSpace(raw) {
	case SubscriptionScopeAll, SubscriptionScopeGroup, SubscriptionScopeModel:
		return strings.TrimSpace(raw)
	}
	return ""
}

func normalizeMeasure(raw string) string {
	switch strings.TrimSpace(raw) {
	case SubscriptionMeasureQuota, SubscriptionMeasureCount:
		return strings.TrimSpace(raw)
	}
	return ""
}

// planLimitAxes resolves the limit axes of a plan: (scope, measure). Legacy
// rows predate the columns: request plans count per model, the pool kinds
// grant a shared quota. The empty scope marks the legacy shared-pool path so
// the runtime can untouched keep its behaviour.
func planLimitAxes(plan *SubscriptionPlan) (scope string, measure string) {
	if s := normalizeScope(plan.Scope); s != "" {
		return s, normalizeMeasureWithDefault(plan.Measure)
	}
	if normalizeSubscriptionKind(plan.Kind) == SubscriptionKindRequest {
		return SubscriptionScopeModel, SubscriptionMeasureCount
	}
	return SubscriptionScopeAll, SubscriptionMeasureQuota
}

// subscriptionLimitAxes is the purchase-time snapshot equivalent of
// planLimitAxes.
func subscriptionLimitAxes(sub *UserSubscription) (scope string, measure string) {
	if s := normalizeScope(sub.Scope); s != "" {
		return s, normalizeMeasureWithDefault(sub.Measure)
	}
	if normalizeSubscriptionKind(sub.Kind) == SubscriptionKindRequest {
		return SubscriptionScopeModel, SubscriptionMeasureCount
	}
	return SubscriptionScopeAll, SubscriptionMeasureQuota
}

// subscriptionCounterKey is the row key a request's entitlement lives under:
// (group, model) for model scope, (group, "") for group scope.
func subscriptionCounterKey(scope string, usingGroup string, modelName string) (string, string) {
	if scope == SubscriptionScopeGroup {
		return strings.TrimSpace(usingGroup), ""
	}
	return strings.TrimSpace(usingGroup), strings.TrimSpace(modelName)
}

func normalizeMeasureWithDefault(raw string) string {
	if m := normalizeMeasure(raw); m != "" {
		return m
	}
	return SubscriptionMeasureCount
}

func parseStringListJSON(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := common.UnmarshalJsonStr(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func marshalStringListJSON(values []string) string {
	if len(values) == 0 {
		return ""
	}
	data, err := common.Marshal(values)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseRequestRulesJSON(raw string) ([]SubscriptionRequestRule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var rules []SubscriptionRequestRule
	if err := common.UnmarshalJsonStr(raw, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func marshalRequestRulesJSON(rules []SubscriptionRequestRule) string {
	if len(rules) == 0 {
		return ""
	}
	data, err := common.Marshal(rules)
	if err != nil {
		return ""
	}
	return string(data)
}

// normalizeStringList trims entries, drops empty ones and removes duplicates
// while preserving the caller's order.
func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// subscriptionAllowsGroup reports whether the subscription's group snapshot
// covers usingGroup. Rows bought before multi-group snapshots existed have no
// UpgradeGroups and stay usable everywhere.
func subscriptionAllowsGroup(sub *UserSubscription, usingGroup string) bool {
	if sub == nil {
		return false
	}
	groups := sub.UpgradeGroups
	if len(groups) == 0 && strings.TrimSpace(sub.UpgradeGroup) != "" {
		groups = []string{sub.UpgradeGroup}
	}
	if len(groups) == 0 {
		return true
	}
	usingGroup = strings.TrimSpace(usingGroup)
	for _, group := range groups {
		if strings.TrimSpace(group) == usingGroup {
			return true
		}
	}
	return false
}

// Subscription plan
type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	// Kind is the limit axis stored for the runtime: request (per-(group,
	// model) request counts) or a quota pool with time-based (time) or
	// exhaustion-based (quota) expiry.
	Kind string `json:"kind" gorm:"type:varchar(16);not null;default:'time'"`

	// ExpireMode is the expiry axis: time (ends at end_time) or quota (ends
	// when the granted pool or request counters run out). Legacy rows store
	// '' and fall back to the kind default in AfterFind.
	ExpireMode string `json:"expire_mode" gorm:"column:expire_mode;type:varchar(16);not null;default:''"`

	// LimitMode is the limit axis as the admin form submits it (group/request).
	// Derived from Kind on read; never stored.
	LimitMode string `json:"limit_mode" gorm:"-"`

	// Scope and Measure are the limit axes the admin form picks: the scope
	// decides what each granted amount applies to (a whole group or one
	// model inside it) and the measure how it is counted (flat requests or
	// quota units). Legacy rows store '' and fall back to Kind.
	Scope   string `json:"scope" gorm:"type:varchar(8);not null;default:''"`
	Measure string `json:"measure" gorm:"type:varchar(8);not null;default:''"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	AllowBalancePay *bool `json:"allow_balance_pay"`
	// Allow falling back to wallet balance after subscription quota is exhausted (empty = true)
	AllowWalletOverflow   *bool  `json:"allow_wallet_overflow"`
	StripePriceId         string `json:"stripe_price_id" gorm:"type:varchar(128);default:''"`
	CreemProductId        string `json:"creem_product_id" gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id" gorm:"type:varchar(128);default:''"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	// Upgrade user group after purchase (empty = no change). It mirrors the
	// first UpgradeGroups entry so old rows and queries keep working.
	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`

	// UpgradeGroups is the multi-group snapshot as a JSON array. The first
	// entry is the primary group; an empty list leaves consumption unrestricted
	// for rows created before this field existed.
	UpgradeGroupsJSON string   `json:"-" gorm:"column:upgrade_groups;type:text"`
	UpgradeGroups     []string `json:"upgrade_groups" gorm:"-"`

	// RequestRules configures per-group, per-model request counts for the
	// request kind. Other kinds ignore it.
	RequestRulesJSON string                    `json:"-" gorm:"column:request_rules;type:text"`
	RequestRules     []SubscriptionRequestRule `json:"request_rules" gorm:"-"`

	// Downgrade user group on expiry (empty = revert to the group held before purchase)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount" gorm:"type:bigint;not null;default:0"`
	// TotalAmountDisplay is the API-boundary rendering of TotalAmount in the
	// site's display currency (gorm:"-", never a column). Plan create/update
	// accept it and convert server-side, so the admin form never multiplies by
	// QuotaPerUnit; list responses emit it for the form seed.
	TotalAmountDisplay float64 `json:"total_amount_display" gorm:"-"`
	// BalanceCostDisplay is PriceAmount rendered in display currency — what a
	// wallet-balance purchase actually costs the user, comparable against
	// quota_display without any client-side conversion.
	BalanceCostDisplay float64 `json:"balance_cost_display" gorm:"-"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// AfterFind fills the API-boundary display field so plan payloads (admin list,
// public list, single) carry the converted amount without each handler
// repeating the math.
func (plan *SubscriptionPlan) AfterFind(_ *gorm.DB) error {
	plan.TotalAmountDisplay = QuotaToDisplayAmount(int(plan.TotalAmount))
	// Price is USD; internal quota = price * QuotaPerUnit, then rendered in
	// display currency. Server-side only — the frontend compares displays.
	plan.BalanceCostDisplay = QuotaToDisplayAmount(
		common.QuotaFromFloat(plan.PriceAmount * common.QuotaPerUnit),
	)
	if groups, err := parseStringListJSON(plan.UpgradeGroupsJSON); err == nil {
		plan.UpgradeGroups = groups
	}
	if rules, err := parseRequestRulesJSON(plan.RequestRulesJSON); err == nil {
		plan.RequestRules = rules
	}
	// The two admin-form axes are derived on read so legacy rows answer the
	// same shape as plans saved through the new form.
	plan.ExpireMode = normalizeExpireMode(plan.ExpireMode, plan.Kind)
	plan.LimitMode = normalizeLimitMode(plan.LimitMode, plan.Kind)
	// Scope/measure answer the same shape for legacy rows: they describe how
	// the granted amount is scoped and counted.
	plan.Scope, plan.Measure = planLimitAxes(plan)
	return nil
}

func (p *SubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *SubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *SubscriptionPlan) NormalizeDefaults() {
	if p.AllowBalancePay == nil {
		p.AllowBalancePay = common.GetPointer(true)
	}
	if p.AllowWalletOverflow == nil {
		p.AllowWalletOverflow = common.GetPointer(true)
	}
	if strings.TrimSpace(p.Kind) == "" {
		p.Kind = SubscriptionKindTime
	}
	p.ExpireMode = normalizeExpireMode(p.ExpireMode, p.Kind)
	p.LimitMode = normalizeLimitMode(p.LimitMode, p.Kind)
	p.Scope, p.Measure = planLimitAxes(p)
}

// planGrantsValuableEntitlement reports whether the plan hands out anything of
// value on fulfillment: a quota pool or a group upgrade.
func planGrantsValuableEntitlement(plan *SubscriptionPlan) bool {
	return plan.TotalAmount > 0 || len(plan.RequestRules) > 0 || strings.TrimSpace(plan.UpgradeGroup) != ""
}

// SubscriptionRequestRule grants the rule's amount under Group. Models is
// only used by model-scoped plans: group-scoped plans leave it empty and the
// rule applies to every request served through the group.
type SubscriptionRequestRule struct {
	Group  string   `json:"group"`
	Models []string `json:"models"`
	Count  int64    `json:"count"`
	// Measure is copied from the plan so every rule carries how it is counted.
	Measure string `json:"measure"`
	// Amount is the quota-unit budget of quota-measured rules; the admin form
	// submits AmountDisplay and the server owns the conversion.
	Amount        int64   `json:"amount"`
	AmountDisplay float64 `json:"amount_display"`
}

// SubscriptionRequestCounter is the per-group, per-model request counter of a
// request-kind user subscription. Each model selected in a plan rule gets its
// own row so the frontend can render one progress entry per model.
type SubscriptionRequestCounter struct {
	Id                 int    `json:"id"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index;uniqueIndex:idx_sub_request_counter,priority:1"`
	GroupName          string `json:"group_name" gorm:"type:varchar(64);uniqueIndex:idx_sub_request_counter,priority:2"`
	ModelName          string `json:"model_name" gorm:"type:varchar(128);uniqueIndex:idx_sub_request_counter,priority:3"`
	// Measure tells quota-counted rows (Total/Used in quota units) apart from
	// flat request counts. Group-scoped rows keep ModelName empty.
	Measure string `json:"measure" gorm:"type:varchar(8);not null;default:'count'"`
	Total   int64  `json:"total" gorm:"type:bigint;not null;default:0"`
	Used    int64  `json:"used" gorm:"type:bigint;not null;default:0"`
	// QuotaTotal / QuotaUsed snapshot the per-counter budget converted from
	// model price x group ratio at purchase time, and the quota actually
	// settled against this counter. They are attribution/bookkeeping columns:
	// the billing truth stays in the subscription's amount_total/amount_used.
	QuotaTotal int64 `json:"quota_total" gorm:"type:bigint;not null;default:0"`
	QuotaUsed  int64 `json:"quota_used" gorm:"type:bigint;not null;default:0"`
	CreatedAt  int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64 `json:"updated_at" gorm:"bigint"`
}

func (c *SubscriptionRequestCounter) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

func (c *SubscriptionRequestCounter) BeforeUpdate(tx *gorm.DB) error {
	c.UpdatedAt = common.GetTimestamp()
	return nil
}

// planSelfServeIsFree reports whether a user can take the plan through the
// wallet endpoint without paying balance on any path: balance purchase is
// disabled outright, or the plan carries no price (a zero price still
// grants the entitlement for free).
func planSelfServeIsFree(plan *SubscriptionPlan) bool {
	if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
		return true
	}
	return plan.PriceAmount <= 0
}

// validateFreePlanPurchaseLimit enforces the free-claim invariant: a plan that
// grants quota or upgrade-group entitlement for free through the wallet path
// must carry a finite per-user purchase cap. Without MaxPurchasePerUser every
// account can claim (and stack) unlimited full-value entitlements, which is a
// silent grant switch rather than a pricing decision.
func validateFreePlanPurchaseLimit(plan *SubscriptionPlan) error {
	if plan.MaxPurchasePerUser > 0 || !planGrantsValuableEntitlement(plan) || !planSelfServeIsFree(plan) {
		return nil
	}
	return errors.New("免费赠送额度或升级权益的套餐必须设置每用户购买上限")
}

// Subscription order (payment -> webhook -> create UserSubscription)
type SubscriptionOrder struct {
	Id     int     `json:"id"`
	UserId int     `json:"user_id" gorm:"index"`
	PlanId int     `json:"plan_id" gorm:"index"`
	Money  float64 `json:"money"`

	TradeNo         string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Status          string `json:"status"`
	CreateTime      int64  `json:"create_time"`
	CompleteTime    int64  `json:"complete_time"`

	ProviderPayload string `json:"provider_payload" gorm:"type:text"`
}

func (o *SubscriptionOrder) Insert() error {
	if o.CreateTime == 0 {
		o.CreateTime = common.GetTimestamp()
	}
	return dbx.DB.Create(o).Error
}

func (o *SubscriptionOrder) Update() error {
	return dbx.DB.Save(o).Error
}

// User subscription instance
type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	// Kind is snapshotted from the plan at purchase time. Scope/Measure
	// snapshot the plan's limit axes; legacy rows keep them empty and
	// subscriptionAxes derives them from Kind.
	Kind    string `json:"kind" gorm:"type:varchar(16);not null;default:'time'"`
	Scope   string `json:"scope" gorm:"type:varchar(8);not null;default:''"`
	Measure string `json:"measure" gorm:"type:varchar(8);not null;default:''"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`
	// AmountTotalDisplay / AmountUsedDisplay are the API-boundary renderings of
	// the int64 quota fields above. Populated in AfterFind so the frontend never
	// has to convert raw quota.
	AmountTotalDisplay float64 `json:"amount_total_display" gorm:"-"`
	AmountUsedDisplay  float64 `json:"amount_used_display" gorm:"-"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_user_sub_active,priority:3"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_user_sub_active,priority:2"` // active/expired/cancelled

	Source string `json:"source" gorm:"type:varchar(32);default:'order'"` // order/admin

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`

	// UpgradeGroups snapshots the plan's multi-group entitlement at purchase
	// time. An empty list keeps legacy subscriptions unrestricted.
	UpgradeGroupsJSON string   `json:"-" gorm:"column:upgrade_groups;type:text"`
	UpgradeGroups     []string `json:"upgrade_groups" gorm:"-"`

	// Downgrade target group on expiry (snapshot from plan; empty = revert to PrevUserGroup)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Whether wallet fallback is allowed after this subscription's quota is exhausted (snapshot from plan)
	AllowWalletOverflow bool `json:"allow_wallet_overflow"`

	// RequestCounters carries the per-model request counters of a request-kind
	// subscription; nil for the other kinds. Filled by buildSubscriptionSummaries.
	RequestCounters []SubscriptionRequestCounter `json:"request_counters,omitempty" gorm:"-"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *UserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

func (s *UserSubscription) AfterFind(_ *gorm.DB) error {
	s.AmountTotalDisplay = QuotaToDisplayAmount64(s.AmountTotal)
	s.AmountUsedDisplay = QuotaToDisplayAmount64(s.AmountUsed)
	if groups, err := parseStringListJSON(s.UpgradeGroupsJSON); err == nil {
		s.UpgradeGroups = groups
	}
	return nil
}

type SubscriptionSummary struct {
	Subscription *UserSubscription `json:"subscription"`
}

type SubscriptionResetResult struct {
	PlanId           int    `json:"plan_id"`
	MatchedCount     int    `json:"matched_count"`
	ResetCount       int    `json:"reset_count"`
	UserCount        int    `json:"user_count"`
	AdvanceResetTime bool   `json:"advance_reset_time"`
	PlanTitle        string `json:"-"`
	AffectedUserIds  []int  `json:"-"`
}

func calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error) {
	if plan == nil {
		return 0, errors.New("plan is nil")
	}
	// Only time-expiry plans carry a fixed term. Quota-expiry plans end when
	// their pool or request counters run out, so end_time stays 0 = no limit.
	if normalizeExpireMode(plan.ExpireMode, plan.Kind) != SubscriptionExpireModeTime {
		return 0, nil
	}
	if plan.DurationValue <= 0 && plan.DurationUnit != SubscriptionDurationCustom {
		return 0, errors.New("duration_value must be > 0")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		return start.AddDate(plan.DurationValue, 0, 0).Unix(), nil
	case SubscriptionDurationMonth:
		return start.AddDate(0, plan.DurationValue, 0).Unix(), nil
	case SubscriptionDurationDay:
		return start.Add(time.Duration(plan.DurationValue) * 24 * time.Hour).Unix(), nil
	case SubscriptionDurationHour:
		return start.Add(time.Duration(plan.DurationValue) * time.Hour).Unix(), nil
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 {
			return 0, errors.New("custom_seconds must be > 0")
		}
		return start.Add(time.Duration(plan.CustomSeconds) * time.Second).Unix(), nil
	default:
		return 0, fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64 {
	if plan == nil {
		return 0
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetNever {
		return 0
	}
	var next time.Time
	switch period {
	case SubscriptionResetDaily:
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, 1)
	case SubscriptionResetWeekly:
		// Align to next Monday 00:00
		weekday := int(base.Weekday()) // Sunday=0
		// Convert to Monday=1..Sunday=7
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, daysUntil)
	case SubscriptionResetMonthly:
		// Align to first day of next month 00:00
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).
			AddDate(0, 1, 0)
	case SubscriptionResetCustom:
		if plan.QuotaResetCustomSeconds <= 0 {
			return 0
		}
		next = base.Add(time.Duration(plan.QuotaResetCustomSeconds) * time.Second)
	default:
		return 0
	}
	if endUnix > 0 && next.Unix() > endUnix {
		return 0
	}
	return next.Unix()
}

func getSubscriptionPlanByIdTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	key := subscriptionPlanCacheKey(id)
	if key != "" {
		if cached, found, err := getSubscriptionPlanCache().Get(key); err == nil && found {
			cached.NormalizeDefaults()
			return &cached, nil
		}
	}
	var plan SubscriptionPlan
	query := dbx.DB
	if tx != nil {
		query = tx
	}
	if err := query.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	plan.NormalizeDefaults()
	_ = getSubscriptionPlanCache().SetWithTTL(key, plan, subscriptionPlanCacheTTL())
	return &plan, nil
}

func getUserGroupByIdTx(tx *gorm.DB, userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid userId")
	}
	if tx == nil {
		tx = dbx.DB
	}
	var group string
	if err := identity.UserQuery(dbx.LockForUpdate(tx)).Where("id = ?", userId).Select(dbx.GroupCol()).Find(&group).Error; err != nil {
		return "", err
	}
	return group, nil
}

// resolveSubscriptionGroupBaselineTx unwinds the user group the subscription
// system granted: from `group` it follows the earliest recorded purchase that
// moved the user into the group, repeating until no subscription claims to have
// granted it. Overlapping purchases snapshot prev_user_group only on the row
// that actually elevated the user (duplicate buys store the empty string), so
// snapshot cannot be trusted after several overlapping grants. A group no
// subscription grants is the user's own baseline — typically an admin
// assignment — and is returned unchanged.
func resolveSubscriptionGroupBaselineTx(tx *gorm.DB, userId int, group string) (string, error) {
	visited := make(map[string]struct{}, 8)
	for group != "" {
		if _, seen := visited[group]; seen {
			return group, nil
		}
		visited[group] = struct{}{}
		var grant UserSubscription
		res := tx.Where("user_id = ? AND upgrade_group = ? AND prev_user_group <> ''", userId, group).
			Order("id asc").Limit(1).Find(&grant)
		if res.Error != nil {
			return "", res.Error
		}
		prev := strings.TrimSpace(grant.PrevUserGroup)
		if res.RowsAffected == 0 || prev == "" || prev == group {
			return group, nil
		}
		group = prev
	}
	return group, nil
}

func downgradeUserGroupForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	downgradeGroup := strings.TrimSpace(sub.DowngradeGroup)
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	// Nothing to do if neither an explicit downgrade target nor an upgrade snapshot exists.
	if downgradeGroup == "" && upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	// If another active upgraded subscription exists, keep the current group.
	var activeSub UserSubscription
	activeQuery := tx.Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?) AND id <> ? AND upgrade_group <> ''",
		sub.UserId, "active", now, sub.Id).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
		return "", nil
	}
	// Determine the downgrade target: an explicit downgrade group takes
	// precedence; otherwise unwind the group the subscription system granted.
	// The per-row prev snapshot misses earlier overlapping grants (duplicates
	// store ''), so walk the grant history instead of trusting this row only.
	target := downgradeGroup
	if target == "" {
		baseline, err := resolveSubscriptionGroupBaselineTx(tx, sub.UserId, currentGroup)
		if err != nil {
			return "", err
		}
		target = baseline
	}
	if target == "" || target == currentGroup {
		return "", nil
	}
	if err := identity.UserQuery(tx).Where("id = ?", sub.UserId).
		Update("group", target).Error; err != nil {
		return "", err
	}
	return target, nil
}

func CreateUserSubscriptionFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, source string) (*UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if plan.MaxPurchasePerUser > 0 {
		var count int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND plan_id = ?", userId, plan.Id).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, errors.New("已达到该套餐购买上限")
		}
	}
	nowUnix := dbinfra.GetDBTimestampTx(tx)
	now := time.Unix(nowUnix, 0)
	kind := normalizeSubscriptionKind(plan.Kind)
	endUnix, err := calcPlanEndTime(now, plan)
	if err != nil {
		return nil, err
	}
	resetBase := now
	nextReset := calcNextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	// Request-count plans whose expiry axis is time keep the term computed
	// above; every plan that expires by exhaustion has no end_time/reset.
	if normalizeExpireMode(plan.ExpireMode, kind) != SubscriptionExpireModeTime {
		endUnix = 0
		nextReset = 0
		lastReset = 0
	}
	upgradeGroups := normalizeStringList(plan.UpgradeGroups)
	if len(upgradeGroups) == 0 && strings.TrimSpace(plan.UpgradeGroup) != "" {
		upgradeGroups = []string{strings.TrimSpace(plan.UpgradeGroup)}
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	if len(upgradeGroups) > 0 {
		upgradeGroup = upgradeGroups[0]
	}
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := getUserGroupByIdTx(tx, userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := identity.UserQuery(tx).Where("id = ?", userId).
				Update("group", upgradeGroup).Error; err != nil {
				return nil, err
			}
		}
	}
	allowWalletOverflow := true
	if plan.AllowWalletOverflow != nil {
		allowWalletOverflow = *plan.AllowWalletOverflow
	}
	amountTotal := plan.TotalAmount
	if kind == SubscriptionKindRequest {
		amountTotal = 0
	}
	scope, measure := planLimitAxes(plan)
	if scope != SubscriptionScopeAll {
		// Scoped plans sell entitlement rows, not a shared pool.
		amountTotal = 0
	}
	sub := &UserSubscription{
		UserId:              userId,
		PlanId:              plan.Id,
		Kind:                kind,
		Scope:               scope,
		Measure:             measure,
		AmountTotal:         amountTotal,
		AmountUsed:          0,
		StartTime:           now.Unix(),
		EndTime:             endUnix,
		Status:              "active",
		Source:              source,
		LastResetTime:       lastReset,
		NextResetTime:       nextReset,
		UpgradeGroup:        upgradeGroup,
		UpgradeGroupsJSON:   marshalStringListJSON(upgradeGroups),
		UpgradeGroups:       upgradeGroups,
		PrevUserGroup:       prevGroup,
		DowngradeGroup:      strings.TrimSpace(plan.DowngradeGroup),
		AllowWalletOverflow: allowWalletOverflow,
		CreatedAt:           common.GetTimestamp(),
		UpdatedAt:           common.GetTimestamp(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	if scope != SubscriptionScopeAll {
		if err := createSubscriptionRequestCountersTx(tx, sub.Id, scope, measure, plan.RequestRules); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

// createSubscriptionRequestCountersTx expands plan rules into entitlement
// rows: one per (group, model) for model scope, one per group (empty model)
// for group scope. Total carries counts or quota units per the measure.
// Plan validation rejects duplicate keys, so the merge below only guards rows
// written before that check existed.
func createSubscriptionRequestCountersTx(tx *gorm.DB, userSubscriptionId int, scope string, measure string, rules []SubscriptionRequestRule) error {
	type counterKey struct {
		group string
		model string
	}
	order := make([]counterKey, 0, len(rules))
	totals := make(map[counterKey]int64, len(rules))
	for _, rule := range rules {
		group := strings.TrimSpace(rule.Group)
		grant := rule.Count
		if measure == SubscriptionMeasureQuota {
			grant = rule.Amount
		}
		if scope == SubscriptionScopeGroup {
			key := counterKey{group: group}
			if _, ok := totals[key]; !ok {
				order = append(order, key)
			}
			totals[key] += grant
			continue
		}
		for _, model := range normalizeStringList(rule.Models) {
			key := counterKey{group: group, model: model}
			if _, ok := totals[key]; !ok {
				order = append(order, key)
			}
			totals[key] += grant
		}
	}
	groupRatios := ratio_setting.GetGroupRatioCopy()
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	for _, key := range order {
		counter := &SubscriptionRequestCounter{
			UserSubscriptionId: userSubscriptionId,
			GroupName:          key.group,
			ModelName:          key.model,
			Measure:            measure,
			Total:              totals[key],
		}
		// Snapshot the indicative budget of a count budget: count x model price
		// x group ratio. Request-time settlement always uses the CURRENT price,
		// so this column seeds display and admin expectations only; unpriced
		// models leave it 0 (count-only, budget tracked nowhere). Quota-measured
		// rows carry the pool itself in Total, so no second budget column.
		if measure == SubscriptionMeasureCount {
			if groupRatio := groupRatios[key.group]; groupRatio > 0 {
				if modelPrice, ok := ratio_setting.GetModelPrice(key.model, false); ok && modelPrice > 0 {
					budget := decimal.NewFromInt(totals[key]).
						Mul(decimal.NewFromFloat(modelPrice)).
						Mul(decimal.NewFromFloat(groupRatio)).
						Mul(quotaPerUnit)
					quota, clamp := common.QuotaFromDecimalChecked(budget)
					if clamp != nil {
						common.SysError(fmt.Sprintf(
							"request-counter budget clamped for subscription %d model %s: %s",
							userSubscriptionId, key.model, clamp.Error()))
					}
					counter.QuotaTotal = int64(quota)
				}
			}
		}
		if err := tx.Create(counter).Error; err != nil {
			return err
		}
	}
	return nil
}

func refreshSubscriptionUserGroupCache(userId int, operation string) {
	if err := identity.RefreshUserGroupCache(userId); err != nil {
		common.SysError(fmt.Sprintf("failed to refresh user group cache after %s for user %d: %v", operation, userId, err))
	}
}

// Complete a subscription order (idempotent). Creates a UserSubscription snapshot from the plan.
// expectedPaymentProvider guards against cross-gateway callback attacks (empty skips the check).
// actualPaymentMethod updates the order's PaymentMethod to reflect the real payment type used (empty skips update).

// Admin bind (no payment). Creates a UserSubscription from a plan.

func calcSubscriptionBalanceQuota(priceAmount float64) (int, error) {
	if priceAmount <= 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("额度单位配置错误")
	}
	quota := decimal.NewFromFloat(priceAmount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil()
	return common.QuotaFromDecimalStrict(quota)
}

// PurchaseSubscriptionWithWallet settles a plan against the user's wallet
// balance. A plan that cannot be paid in site is created without a charge; any
// payable plan costs PriceAmount in quota, and the whole settlement rolls back
// if a later step fails.
func PurchaseSubscriptionWithWallet(userId int, planId int) error {
	if userId <= 0 || planId <= 0 {
		return errors.New("invalid userId or planId")
	}

	var logPlanTitle string
	var logMoney float64
	var chargedQuota int
	var upgradeGroup string
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("套餐未启用")
		}
		if plan.PriceAmount < 0 {
			return errors.New("套餐价格不能为负数")
		}

		if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
			return errors.New("该套餐不允许使用余额兑换")
		}
		requiredQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
		if err != nil {
			return err
		}
		if err := validateFreePlanPurchaseLimit(plan); err != nil {
			return err
		}

		user, err := identity.LockUserRow(tx, userId)
		if err != nil {
			return err
		}
		if requiredQuota > 0 && user.Quota < requiredQuota {
			return errors.New("余额不足")
		}
		if requiredQuota > 0 {
			if err := identity.UserQuery(tx).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", requiredQuota)).Error; err != nil {
				return err
			}
		}

		subscription, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, PaymentMethodBalance)
		if err != nil {
			return err
		}

		now := common.GetTimestamp()
		tradeNo := fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
		order := &SubscriptionOrder{
			UserId:          userId,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodBalance,
			PaymentProvider: PaymentProviderBalance,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", requiredQuota),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		logPlanTitle = plan.Title
		logMoney = plan.PriceAmount
		chargedQuota = requiredQuota
		if subscription.PrevUserGroup != "" {
			upgradeGroup = strings.TrimSpace(subscription.UpgradeGroup)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if chargedQuota > 0 {
		if err := quotacache.DecrUser(userId, int64(chargedQuota)); err != nil {
			common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
		}
	}
	if upgradeGroup != "" {
		refreshSubscriptionUserGroupCache(userId, "subscription balance purchase")
	}
	msg := fmt.Sprintf("购买订阅成功，套餐: %s，支付金额: %.2f，扣除额度: %d",
		logPlanTitle, logMoney, chargedQuota)
	usage.RecordLog(userId, usage.LogTypeTopup, msg)
	return nil
}

// GetAllActiveUserSubscriptions returns all active subscriptions for a user.
func GetAllActiveUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	err := dbx.DB.Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", userId, "active", now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

// HasActiveUserSubscription returns whether the user has any active subscription.
// This is a lightweight existence check to avoid heavy pre-consume transactions.
func HasActiveUserSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var count int64
	if err := dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", userId, "active", now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UserActiveSubscriptionsAllowWalletOverflow returns whether wallet balance may be used
// after the user's subscription quota is exhausted. A single active subscription that
// disallows wallet overflow (allow_wallet_overflow = false) blocks the fallback for the
// request's group.
func UserActiveSubscriptionsAllowWalletOverflow(userId int, usingGroup string) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	if err := dbx.DB.
		Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?) AND allow_wallet_overflow = ?",
			userId, "active", now, false).
		Find(&subs).Error; err != nil {
		return false, err
	}
	for i := range subs {
		if subscriptionAllowsGroup(&subs[i], usingGroup) {
			return false, nil
		}
	}
	return true, nil
}

// HasUsableUserSubscription reports whether an active subscription can serve a
// request in usingGroup for modelName. Request-kind subscriptions only count
// when the matching counter still has requests left.
func HasUsableUserSubscription(userId int, usingGroup string, modelName string, allowRequestCount bool) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	usingGroup = strings.TrimSpace(usingGroup)
	modelName = strings.TrimSpace(modelName)
	now := common.GetTimestamp()
	var subs []UserSubscription
	if err := dbx.DB.
		Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", userId, "active", now).
		Find(&subs).Error; err != nil {
		return false, err
	}
	requestSubIds := make([]int, 0, len(subs))
	scopedKeys := make(map[int][2]string, len(subs))
	for i := range subs {
		if !subscriptionAllowsGroup(&subs[i], usingGroup) {
			continue
		}
		scope, measure := subscriptionLimitAxes(&subs[i])
		if scope != SubscriptionScopeAll {
			// Count-measured plans are text-generation only (the legacy
			// request-kind contract); quota-measured rows are money pools and
			// serve every relay format.
			if measure == SubscriptionMeasureCount && !allowRequestCount {
				continue
			}
			requestSubIds = append(requestSubIds, subs[i].Id)
			rowGroup, rowModel := subscriptionCounterKey(scope, usingGroup, modelName)
			scopedKeys[subs[i].Id] = [2]string{rowGroup, rowModel}
			continue
		}
		if subs[i].AmountTotal == 0 || subs[i].AmountUsed < subs[i].AmountTotal {
			return true, nil
		}
	}
	if len(requestSubIds) == 0 || usingGroup == "" || modelName == "" {
		return false, nil
	}
	// Each scoped subscription matches its own (group, model) row key, so the
	// scan is per subscription rather than one IN clause.
	for _, subId := range requestSubIds {
		key := scopedKeys[subId]
		var count int64
		if err := dbx.DB.Model(&SubscriptionRequestCounter{}).
			Where("user_subscription_id = ? AND group_name = ? AND model_name = ? AND used < total",
				subId, key[0], key[1]).
			Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

// GetAllUserSubscriptions returns all subscriptions (active and expired) for a user.
func GetAllUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	var subs []UserSubscription
	err := dbx.DB.Where("user_id = ?", userId).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

func buildSubscriptionSummaries(subs []UserSubscription) []SubscriptionSummary {
	if len(subs) == 0 {
		return []SubscriptionSummary{}
	}
	requestSubIds := make([]int, 0, len(subs))
	for i := range subs {
		if scope, _ := subscriptionLimitAxes(&subs[i]); scope != SubscriptionScopeAll {
			requestSubIds = append(requestSubIds, subs[i].Id)
		}
	}
	countersBySubscription := make(map[int][]SubscriptionRequestCounter, len(requestSubIds))
	if len(requestSubIds) > 0 {
		var counters []SubscriptionRequestCounter
		if err := dbx.DB.Where("user_subscription_id IN ?", requestSubIds).
			Order("id asc").
			Find(&counters).Error; err != nil {
			common.SysError("failed to load subscription request counters: " + err.Error())
		} else {
			for _, counter := range counters {
				countersBySubscription[counter.UserSubscriptionId] = append(
					countersBySubscription[counter.UserSubscriptionId], counter)
			}
		}
	}
	result := make([]SubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		if _, scopedScope := subscriptionLimitAxes(&sub); scopedScope != SubscriptionScopeAll {
			subCopy.RequestCounters = countersBySubscription[sub.Id]
		}
		result = append(result, SubscriptionSummary{Subscription: &subCopy})
	}
	return result
}

// AdminInvalidateUserSubscription marks a user subscription as cancelled and ends it immediately.
func invalidateUserSubscriptionRecord(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := dbx.LockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":     "cancelled",
			"end_time":   now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		refreshSubscriptionUserGroupCache(userId, "admin subscription update")
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func deleteUserSubscriptionRecord(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := dbx.LockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if err := tx.Where("id = ?", userSubscriptionId).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		refreshSubscriptionUserGroupCache(userId, "admin subscription deletion")
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
	// RequestCounterUsedBefore is the counter's used value before the request
	// was reserved (0 for amount-kind records). SubscriptionFunding syncs it
	// onto RelayInfo so the settlement path can attribute per-request counters.
	RequestCounterUsedBefore int64
	// RequestCounterId is the entitlement row this request reserved (scoped
	// subscriptions only; 0 for the shared pool).
	RequestCounterId int
	// RequestKind marks that this pre-consume served a request-kind
	// subscription and reserved one request counter entry.
	RequestKind bool
}

// ExpireDueSubscriptions marks expired subscriptions and handles group downgrade.
// Time-limited rows expire at end_time (0 = no time limit); quota-kind rows
// expire when the pool is spent; request-kind rows expire when every counter is
// spent. Each category is scanned with its own limit so a large backlog of one
// kind cannot starve the others.
func ExpireDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := dbinfra.GetDBTimestamp()
	candidates := make([]UserSubscription, 0, limit*3)
	var timeExpired []UserSubscription
	if err := dbx.DB.Where("status = ? AND end_time > 0 AND end_time <= ?", "active", now).
		Order("end_time asc, id asc").
		Limit(limit).
		Find(&timeExpired).Error; err != nil {
		return 0, err
	}
	candidates = append(candidates, timeExpired...)
	var quotaExpired []UserSubscription
	if err := dbx.DB.Where("status = ? AND kind = ? AND amount_total > 0 AND amount_used >= amount_total",
		"active", SubscriptionKindQuota).
		Order("id asc").
		Limit(limit).
		Find(&quotaExpired).Error; err != nil {
		return 0, err
	}
	candidates = append(candidates, quotaExpired...)
	requestExpired, err := findExhaustedRequestSubscriptions(limit)
	if err != nil {
		return 0, err
	}
	candidates = append(candidates, requestExpired...)

	seen := make(map[int]struct{}, len(candidates))
	expiredCount := 0
	changedUsers := make(map[int]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.Id]; ok {
			continue
		}
		seen[candidate.Id] = struct{}{}
		downgradedGroup := ""
		err := dbx.DB.Transaction(func(tx *gorm.DB) error {
			var sub UserSubscription
			if err := dbx.LockForUpdate(tx).Where("id = ? AND status = ?", candidate.Id, "active").First(&sub).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			expired, err := subscriptionReachedEndTx(tx, &sub, now)
			if err != nil || !expired {
				return err
			}
			res := tx.Model(&UserSubscription{}).
				Where("id = ? AND status = ?", sub.Id, "active").
				Updates(map[string]interface{}{
					"status":     "expired",
					"updated_at": common.GetTimestamp(),
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil
			}
			expiredCount += int(res.RowsAffected)
			downgradedGroup, err = downgradeUserGroupForSubscriptionTx(tx, &sub, now)
			return err
		})
		if err != nil {
			return expiredCount, err
		}
		if downgradedGroup != "" && candidate.UserId > 0 {
			changedUsers[candidate.UserId] = struct{}{}
		}
	}
	for userId := range changedUsers {
		refreshSubscriptionUserGroupCache(userId, "subscription expiration")
	}
	return expiredCount, nil
}

func subscriptionReachedEndTx(tx *gorm.DB, sub *UserSubscription, now int64) (bool, error) {
	if sub == nil {
		return false, nil
	}
	if sub.EndTime > 0 && sub.EndTime <= now {
		return true, nil
	}
	// Scoped subscriptions (per-group or per-model entitlements) end when every
	// entitlement row is spent, whatever the stored kind says.
	if scope, _ := subscriptionLimitAxes(sub); scope != SubscriptionScopeAll {
		return requestSubscriptionExhaustedTx(tx, sub.Id)
	}
	switch normalizeSubscriptionKind(sub.Kind) {
	case SubscriptionKindQuota:
		return sub.AmountTotal > 0 && sub.AmountUsed >= sub.AmountTotal, nil
	default:
		return false, nil
	}
}

func requestSubscriptionExhaustedTx(tx *gorm.DB, userSubscriptionId int) (bool, error) {
	var counters []SubscriptionRequestCounter
	if err := tx.Where("user_subscription_id = ?", userSubscriptionId).Find(&counters).Error; err != nil {
		return false, err
	}
	if len(counters) == 0 {
		return false, nil
	}
	for _, counter := range counters {
		if counter.Used < counter.Total {
			return false, nil
		}
	}
	return true, nil
}

func findExhaustedRequestSubscriptions(limit int) ([]UserSubscription, error) {
	var subs []UserSubscription
	if err := dbx.DB.Where("status = ? AND (kind = ? OR scope IN ?)",
		"active", SubscriptionKindRequest, []string{SubscriptionScopeGroup, SubscriptionScopeModel}).
		Order("id asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(subs))
	for _, sub := range subs {
		ids = append(ids, sub.Id)
	}
	var counters []SubscriptionRequestCounter
	if err := dbx.DB.Where("user_subscription_id IN ?", ids).Find(&counters).Error; err != nil {
		return nil, err
	}
	hasRemaining := make(map[int]bool, len(subs))
	hasCounter := make(map[int]struct{}, len(subs))
	for _, counter := range counters {
		hasCounter[counter.UserSubscriptionId] = struct{}{}
		if counter.Used < counter.Total {
			hasRemaining[counter.UserSubscriptionId] = true
		}
	}
	exhausted := make([]UserSubscription, 0, len(subs))
	for _, sub := range subs {
		if _, ok := hasCounter[sub.Id]; !ok {
			continue
		}
		if hasRemaining[sub.Id] {
			continue
		}
		exhausted = append(exhausted, sub)
	}
	return exhausted, nil
}

// SubscriptionPreConsumeRecord stores idempotent pre-consume operations per request.
type SubscriptionPreConsumeRecord struct {
	Id                 int    `json:"id"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	// Kind distinguishes quota pre-consume (amount) from request-count
	// pre-consume (request). Rows created before this field default to amount.
	Kind string `json:"kind" gorm:"type:varchar(16);default:'amount'"`
	// RequestCounterId points at the request counter row a request-kind
	// pre-consume incremented. The count is enforced at pre-consume and the
	// outcome classifier; settlement always flows through the standard amount
	// legs, so this stays informational (replays, audits, refunds by row).
	RequestCounterId int    `json:"request_counter_id" gorm:"default:0"`
	PreConsumed      int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status           string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *SubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionPreConsumeRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func maybeResetUserSubscriptionWithPlanTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return nil
	}
	if NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		return nil
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTime(base, plan, sub.EndTime)
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = calcNextResetTime(base, plan, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return tx.Save(sub).Error
		}
		return nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return tx.Save(sub).Error
}

// PreConsumeUserSubscription pre-consumes from any active subscription total
// quota. Request-kind subscriptions join the same quota legs and additionally
// reserve one request from the matching counter inside this transaction.
func PreConsumeUserSubscription(requestId string, userId int, usingGroup string, modelName string, quotaType int, amount int64) (*SubscriptionPreConsumeResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := dbinfra.GetDBTimestamp()

	returnValue := &SubscriptionPreConsumeResult{}

	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var existing SubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if existing.Status == "refunded" {
				return errors.New("subscription pre-consume already refunded")
			}
			var sub UserSubscription
			if err := tx.Where("id = ?", existing.UserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = existing.PreConsumed
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = sub.AmountUsed
			returnValue.AmountUsedAfter = sub.AmountUsed
			returnValue.RequestCounterId = existing.RequestCounterId
			if existing.RequestCounterId > 0 {
				var replayCounter SubscriptionRequestCounter
				if err := tx.Where("id = ?", existing.RequestCounterId).First(&replayCounter).Error; err == nil {
					returnValue.RequestKind = replayCounter.Measure == SubscriptionMeasureCount
					// The row already holds this request's reservation; report
					// what was used before it rather than the row id.
					usedBefore := replayCounter.Used
					if replayCounter.Measure == SubscriptionMeasureQuota {
						usedBefore -= existing.PreConsumed
					} else if usedBefore > 0 {
						usedBefore--
					}
					if usedBefore < 0 {
						usedBefore = 0
					}
					returnValue.RequestCounterUsedBefore = usedBefore
				}
			}
			return nil
		}

		var subs []UserSubscription
		if err := dbx.LockForUpdate(tx).
			Where("user_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", userId, "active", now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return errors.New("no active subscription")
		}
		if len(subs) == 0 {
			return errors.New("no active subscription")
		}
		candidates := make([]UserSubscription, 0, len(subs))
		for _, candidate := range subs {
			if !subscriptionAllowsGroup(&candidate, usingGroup) {
				continue
			}
			if candidate.AmountTotal > 0 && candidate.AmountUsed >= candidate.AmountTotal {
				continue
			}
			candidates = append(candidates, candidate)
		}
		if len(candidates) == 0 {
			return errors.New("no active subscription")
		}
		for _, candidate := range candidates {
			sub := candidate
			plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
			if err != nil {
				return err
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
				return err
			}
			// Gate the shared pool before touching the scoped row, so a
			// skipped candidate cannot leave a phantom reservation behind.
			usedBefore := sub.AmountUsed
			if sub.AmountTotal > 0 {
				remain := sub.AmountTotal - usedBefore
				if remain < amount {
					continue
				}
			}
			// Scoped candidates additionally reserve their entitlement inside
			// this same transaction: one request for count measures, the
			// requested amount for quota measures. The entitlement row is the
			// gate for scoped plans; the amount legs below stay standard so
			// pre-consume / settle / refund share one code path.
			requestCounterUsedBefore := int64(0)
			var requestCounterId int
			scoped := false
			scope, measure := subscriptionLimitAxes(&sub)
			if scope != SubscriptionScopeAll {
				scoped = true
				rowGroup, rowModel := subscriptionCounterKey(scope, usingGroup, modelName)

				var counter SubscriptionRequestCounter
				if err := dbx.LockForUpdate(tx).
					Where("user_subscription_id = ? AND group_name = ? AND model_name = ?", sub.Id, rowGroup, rowModel).
					First(&counter).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return err
				}
				rowUsedBefore := counter.Used
				if measure == SubscriptionMeasureQuota {
					if counter.Total > 0 && counter.Used+amount > counter.Total {
						continue
					}
					counter.Used += amount
				} else {
					if counter.Used >= counter.Total {
						continue
					}
					counter.Used++
				}
				if err := tx.Save(&counter).Error; err != nil {
					return err
				}
				requestCounterId = counter.Id
				requestCounterUsedBefore = rowUsedBefore
			}
			record := &SubscriptionPreConsumeRecord{
				RequestId:          requestId,
				UserId:             userId,
				UserSubscriptionId: sub.Id,
				Kind:               "amount",
				RequestCounterId:   requestCounterId,
				PreConsumed:        amount,
				Status:             "consumed",
			}
			if err := tx.Create(record).Error; err != nil {
				var dup SubscriptionPreConsumeRecord
				if err2 := tx.Where("request_id = ?", requestId).First(&dup).Error; err2 == nil {
					if dup.Status == "refunded" {
						return errors.New("subscription pre-consume already refunded")
					}
					returnValue.UserSubscriptionId = sub.Id
					returnValue.PreConsumed = dup.PreConsumed
					returnValue.AmountTotal = sub.AmountTotal
					returnValue.AmountUsedBefore = sub.AmountUsed
					returnValue.AmountUsedAfter = sub.AmountUsed
					returnValue.RequestCounterId = dup.RequestCounterId
					if dup.RequestCounterId > 0 {
						var replayCounter SubscriptionRequestCounter
						if err := tx.Where("id = ?", dup.RequestCounterId).First(&replayCounter).Error; err == nil {
							returnValue.RequestKind = replayCounter.Measure == SubscriptionMeasureCount
						}
					}
					return nil
				}
				return err
			}
			sub.AmountUsed += amount
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = amount
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = usedBefore
			returnValue.AmountUsedAfter = sub.AmountUsed
			returnValue.RequestCounterUsedBefore = requestCounterUsedBefore
			returnValue.RequestCounterId = requestCounterId
			returnValue.RequestKind = scoped && measure == SubscriptionMeasureCount
			return nil
		}
		return fmt.Errorf("subscription quota insufficient, need=%d", amount)
	})
	if err != nil {
		return nil, err
	}
	return returnValue, nil
}

// RefundSubscriptionPreConsume is idempotent and refunds pre-consumed
// subscription quota by requestId. Request-kind records additionally release
// the counter they reserved; a release failure is logged instead of failing
// the refund, so the quota refund (the billing-critical leg) always lands.
func RefundSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := dbx.LockForUpdate(tx).
			Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		reservedCounterId := record.RequestCounterId
		if reservedCounterId > 0 {
			releaseSubscriptionRequestCounterTx(tx, reservedCounterId, record.PreConsumed)
			// Zero the marker so a later waive/settle cannot release twice.
			record.RequestCounterId = 0
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(&record).Error
		}
		if err := postConsumeUserSubscriptionDeltaTx(tx, record.UserSubscriptionId, -record.PreConsumed); err != nil {
			return err
		}
		attributeSubscriptionCounterQuotaTx(tx, reservedCounterId, -record.PreConsumed)
		record.Status = "refunded"
		return tx.Save(&record).Error
	})
}

// WaiveSubscriptionRequestConsume releases the counter a request-kind
// subscription reserved for requestId when the settled outcome should not
// count (empty response, short upstream cut, policy rejection). Idempotent:
// it only acts on records whose counter was already released.
func WaiveSubscriptionRequestConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := dbx.LockForUpdate(tx).
			Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.RequestCounterId == 0 {
			return nil
		}
		releaseSubscriptionRequestCounterTx(tx, record.RequestCounterId, record.PreConsumed)
		record.RequestCounterId = 0
		return tx.Save(&record).Error
	})
}

// attributeSubscriptionCounterQuotaTx moves settled quota onto the counter's
// attribution column. delta may be negative (refunds). Best-effort: a missing
// row is tolerated because the amount legs carry the billing truth.
func attributeSubscriptionCounterQuotaTx(tx *gorm.DB, counterId int, delta int64) {
	if counterId <= 0 || delta == 0 {
		return
	}
	var counter SubscriptionRequestCounter
	if err := dbx.LockForUpdate(tx).Where("id = ?", counterId).First(&counter).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError(fmt.Sprintf("failed to load request counter %d for quota attribution: %s", counterId, err.Error()))
		}
		return
	}
	newUsed := counter.QuotaUsed + delta
	if newUsed < 0 {
		newUsed = 0
	}
	counter.QuotaUsed = newUsed
	if err := tx.Save(&counter).Error; err != nil {
		common.SysError(fmt.Sprintf("failed to save request counter %d quota attribution: %s", counterId, err.Error()))
	}
}

// releaseSubscriptionRequestCounterTx decrements an entitlement row for a
// refunded/waived reservation: one count, or the pre-consumed quota units for
// quota-measured rows. Best-effort: missing rows and floors below zero are
// tolerated because the standard amount legs carry the billing truth.
func releaseSubscriptionRequestCounterTx(tx *gorm.DB, counterId int, release int64) {
	var counter SubscriptionRequestCounter
	if err := dbx.LockForUpdate(tx).Where("id = ?", counterId).First(&counter).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError(fmt.Sprintf("failed to load request counter %d for refund: %s", counterId, err.Error()))
		}
		return
	}
	if counter.Measure == SubscriptionMeasureQuota {
		if release > counter.Used {
			release = counter.Used
		}
	} else {
		release = 1
	}
	if release > 0 && counter.Used >= release {
		counter.Used -= release
		if err := tx.Save(&counter).Error; err != nil {
			common.SysError(fmt.Sprintf("failed to release request counter %d: %s", counterId, err.Error()))
		}
	}
}

// ResetDueSubscriptions resets subscriptions whose next_reset_time has passed.
func ResetDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := dbinfra.GetDBTimestamp()
	var subs []UserSubscription
	if err := dbx.DB.Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", now, "active").
		Order("next_reset_time asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, sub := range subs {
		subCopy := sub
		plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
		if err != nil || plan == nil {
			continue
		}
		err = dbx.DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscription
			if err := dbx.LockForUpdate(tx).
				Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ?", subCopy.Id, now).
				First(&locked).Error; err != nil {
				return nil
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &locked, plan, now); err != nil {
				return err
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
	}
	return resetCount, nil
}

// CleanupSubscriptionPreConsumeRecords removes old idempotency records to keep table small.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	cutoff := dbinfra.GetDBTimestamp() - olderThanSeconds
	res := dbx.DB.Where("updated_at < ?", cutoff).Delete(&SubscriptionPreConsumeRecord{})
	return res.RowsAffected, res.Error
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

// Update subscription used amount by delta (positive consume more, negative refund).
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if delta == 0 {
		return nil
	}
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		return postConsumeUserSubscriptionDeltaTx(tx, userSubscriptionId, delta)
	})
}

// AttributeSubscriptionCounterQuota mirrors a settled (or refunded) quota delta
// onto the request counter this requestId reserved. delta may be negative.
// Best-effort: rows without a counter (amount-kind records) are a no-op.
func AttributeSubscriptionCounterQuota(requestId string, delta int64) error {
	if delta == 0 || strings.TrimSpace(requestId) == "" {
		return nil
	}
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := tx.Where("request_id = ?", requestId).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		attributeSubscriptionCounterQuotaTx(tx, record.RequestCounterId, delta)
		return nil
	})
}

// postConsumeUserSubscriptionDeltaTx applies the usage delta while locking the
// subscription row on tx. Callers that already hold a transaction — the refund
// path locks the pre-consume record in the same unit of work — must use this
// instead of the exported wrapper: a second, independent transaction per commit
// is not atomic with the record status update, and on multi-connection SQLite
// the nested write blocks behind the outer transaction's locks, leaving failed
// requests permanently unrefunded.
func postConsumeUserSubscriptionDeltaTx(tx *gorm.DB, userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	var sub UserSubscription
	if err := dbx.LockForUpdate(tx).
		Where("id = ?", userSubscriptionId).
		First(&sub).Error; err != nil {
		return err
	}
	newUsed := sub.AmountUsed + delta
	if newUsed < 0 {
		newUsed = 0
	}
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	sub.AmountUsed = newUsed
	return tx.Save(&sub).Error
}

// PostConsumeUserSubscriptionDeltaCapped 消费订阅剩余额度,返回装不下的
// 溢出部分(delta<=0 或未超限时为 0)。与严格版不同:交付完成后的结算
// 必须入账订阅还能覆盖的部分,由调用方处置余额,而不是整笔补扣作废。
func PostConsumeUserSubscriptionDeltaCapped(userSubscriptionId int, delta int64) (int64, error) {
	if userSubscriptionId <= 0 {
		return 0, errors.New("invalid userSubscriptionId")
	}
	if delta <= 0 {
		return 0, errors.New("delta must be positive")
	}
	var overflow int64
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := dbx.LockForUpdate(tx).
			Where("id = ?", userSubscriptionId).
			First(&sub).Error; err != nil {
			return err
		}
		newUsed := sub.AmountUsed + delta
		if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			overflow = newUsed - sub.AmountTotal
			newUsed = sub.AmountTotal
		}
		sub.AmountUsed = newUsed
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return overflow, nil
}
func GetSubscriptionPlanInfoByUserSubscriptionId(userSubscriptionId int) (*SubscriptionPlanInfo, error) {
	if userSubscriptionId <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}
	cacheKey := fmt.Sprintf("sub:%d", userSubscriptionId)
	if cached, found, err := getSubscriptionPlanInfoCache().Get(cacheKey); err == nil && found {
		return &cached, nil
	}
	var sub UserSubscription
	if err := dbx.DB.Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
	if err != nil {
		return nil, err
	}
	info := &SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: plan.Title,
	}
	_ = getSubscriptionPlanInfoCache().SetWithTTL(cacheKey, *info, subscriptionPlanInfoCacheTTL())
	return info, nil
}
func AdminResetPlanSubscriptionsRecord(planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if planId <= 0 {
		return nil, errors.New("invalid planId")
	}
	var result *SubscriptionResetResult
	now := dbinfra.GetDBTimestamp()
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetPlanSubscriptionsTx(tx, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func AdminResetUserSubscriptionsByPlanRecord(userId int, planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if userId <= 0 || planId <= 0 {
		return nil, errors.New("invalid userId or planId")
	}
	var result *SubscriptionResetResult
	now := dbinfra.GetDBTimestamp()
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetUserSubscriptionsByPlanTx(tx, userId, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func adminResetPlanSubscriptionsTx(tx *gorm.DB, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := dbx.LockForUpdate(tx).
		Where("plan_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", plan.Id, "active", now).
		Order("user_id asc, end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}
func adminResetUserSubscriptionsByPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := dbx.LockForUpdate(tx).
		Where("user_id = ? AND plan_id = ? AND status = ? AND (end_time = 0 OR end_time > ?)", userId, plan.Id, "active", now).
		Order("end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, errors.New("该用户没有有效的此套餐订阅")
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}
func resetUserSubscriptionTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64, advanceResetTime bool) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	if scope, _ := subscriptionLimitAxes(sub); scope != SubscriptionScopeAll {
		if err := tx.Model(&SubscriptionRequestCounter{}).
			Where("user_subscription_id = ?", sub.Id).
			Update("used", 0).Error; err != nil {
			return err
		}
	}
	sub.AmountUsed = 0
	if advanceResetTime {
		nextReset := calcNextResetTime(time.Unix(now, 0), plan, sub.EndTime)
		sub.NextResetTime = nextReset
		if nextReset > 0 {
			sub.LastResetTime = now
		} else {
			sub.LastResetTime = 0
		}
	}
	return tx.Save(sub).Error
}
func AdminBindSubscriptionRecord(userId int, planId int, sourceNote string) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid userId or planId")
	}
	plan, err := GetSubscriptionPlanById(planId)
	if err != nil {
		return "", err
	}
	groupChanged := false
	err = dbx.DB.Transaction(func(tx *gorm.DB) error {
		// 与 CompleteSubscriptionOrder 一致：先锁用户行，再做购买次数检查。
		if _, err := identity.LockUserRow(tx, userId); err != nil {
			return err
		}
		subscription, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		if err == nil {
			groupChanged = subscription.PrevUserGroup != ""
		}
		return err
	})
	if err != nil {
		return "", err
	}
	if groupChanged {
		refreshSubscriptionUserGroupCache(userId, "admin subscription creation")
		return fmt.Sprintf("用户分组将升级到 %s", plan.UpgradeGroup), nil
	}
	return "", nil
}
func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := dbx.LockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return nil
		}
		order.Status = common.TopUpStatusExpired
		order.CompleteTime = common.GetTimestamp()
		return tx.Save(&order).Error
	})
}
func upsertSubscriptionTopUpTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}
	now := common.GetTimestamp()
	var topup TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = TopUp{
				UserId:        order.UserId,
				Amount:        0,
				Money:         order.Money,
				TradeNo:       order.TradeNo,
				PaymentMethod: order.PaymentMethod,
				CreateTime:    order.CreateTime,
				CompleteTime:  now,
				Status:        common.TopUpStatusSuccess,
			}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = common.TopUpStatusSuccess
	return tx.Save(&topup).Error
}
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	var logUserId int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var upgradeGroup string
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := dbx.LockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		plan, err := GetSubscriptionPlanById(order.PlanId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			// still allow completion for already purchased orders
		}
		// 锁定用户行：并发完成同一用户的不同订单（包括多实例部署下）时，
		// 使 CreateUserSubscriptionFromPlanTx 的 MaxPurchasePerUser 检查按用户串行。
		if _, err := identity.LockUserRow(tx, order.UserId); err != nil {
			return err
		}
		subscription, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		if subscription.PrevUserGroup != "" {
			upgradeGroup = strings.TrimSpace(subscription.UpgradeGroup)
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		order.Status = common.TopUpStatusSuccess
		order.CompleteTime = common.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
			order.PaymentMethod = actualPaymentMethod
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		logUserId = order.UserId
		logPlanTitle = plan.Title
		logMoney = order.Money
		logPaymentMethod = order.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserId > 0 {
		refreshSubscriptionUserGroupCache(logUserId, "subscription payment completion")
	}
	if logUserId > 0 {
		msg := fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %.2f，支付方式: %s", logPlanTitle, logMoney, logPaymentMethod)
		usage.RecordLog(logUserId, usage.LogTypeTopup, msg)
	}
	return nil
}
func CountUserSubscriptionsByPlan(userId int, planId int) (int64, error) {
	if userId <= 0 || planId <= 0 {
		return 0, errors.New("invalid userId or planId")
	}
	var count int64
	if err := dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", userId, planId).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
func GetSubscriptionPlanById(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(nil, id)
}
func GetSubscriptionOrderByTradeNo(tradeNo string) *SubscriptionOrder {
	if tradeNo == "" {
		return nil
	}
	var order SubscriptionOrder
	if err := dbx.DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil
	}
	return &order
}
func InvalidateSubscriptionPlanCache(planId int) {
	if planId <= 0 {
		return
	}
	cache := getSubscriptionPlanCache()
	_, _ = cache.DeleteMany([]string{subscriptionPlanCacheKey(planId)})
	infoCache := getSubscriptionPlanInfoCache()
	_ = infoCache.Purge()
}
func getSubscriptionPlanInfoCache() *cachex.HybridCache[SubscriptionPlanInfo] {
	subscriptionPlanInfoCacheOnce.Do(func() {
		ttl := subscriptionPlanInfoCacheTTL()
		subscriptionPlanInfoCache = cachex.NewHybridCache[SubscriptionPlanInfo](cachex.HybridCacheConfig[SubscriptionPlanInfo]{
			Namespace: cachex.Namespace(subscriptionPlanInfoCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlanInfo]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlanInfo] {
				return hot.NewHotCache[string, SubscriptionPlanInfo](hot.LRU, subscriptionPlanInfoCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanInfoCache
}
func subscriptionPlanInfoCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_TTL", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}
func subscriptionPlanInfoCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}
func buildSubscriptionResetResult(plan *SubscriptionPlan, subs []UserSubscription, advanceResetTime bool) *SubscriptionResetResult {
	userIds := make([]int, 0, len(subs))
	seenUsers := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if _, ok := seenUsers[sub.UserId]; ok {
			continue
		}
		seenUsers[sub.UserId] = struct{}{}
		userIds = append(userIds, sub.UserId)
	}
	return &SubscriptionResetResult{
		PlanId:           plan.Id,
		MatchedCount:     len(subs),
		ResetCount:       len(subs),
		UserCount:        len(userIds),
		AdvanceResetTime: advanceResetTime,
		PlanTitle:        plan.Title,
		AffectedUserIds:  userIds,
	}
}
