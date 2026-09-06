package usage

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// insightProfileView 是画像的前端视图：原始计数 + 派生结论，
// 让前端不必重复实现比例与阈值逻辑。
type insightProfileView struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Status      int    `json:"status"`
	Role        int    `json:"role"`
	Group       string `json:"group,omitempty"`

	FirstSeenAt   int64 `json:"first_seen_at"`
	LastSeenAt    int64 `json:"last_seen_at"`
	TotalRequests int   `json:"total_requests"`

	CodeRequests      int `json:"code_requests"`
	RoleplayRequests  int `json:"roleplay_requests"`
	QARequests        int `json:"qa_requests"`
	TranslateRequests int `json:"translate_requests"`
	EmbeddingRequests int `json:"embedding_requests"`
	OtherRequests     int `json:"other_requests"`

	PrimaryCategory string        `json:"primary_category"`
	PrimaryStack    string        `json:"primary_stack"`
	Ratios          InsightRatios `json:"ratios"`

	GuessedGender    string  `json:"guessed_gender"`
	GenderConfidence float64 `json:"gender_confidence"`
	// GenderBasis 是性别结论的依据强度：self_report（用户自述）/
	// inverse（由 AI 角色性别反推的群体倾向）。前端必须展示它——
	// inverse 的置信度上限只有 0.45，与自述不是同一个量级的证据。
	GenderBasis      string `json:"gender_basis,omitempty"`
	AIFemaleRequests int    `json:"ai_female_requests"`
	AIMaleRequests   int    `json:"ai_male_requests"`

	RiskLevel          string         `json:"risk_level"`
	JailbreakSuspect   int            `json:"jailbreak_suspect"`
	JailbreakLikely    int            `json:"jailbreak_likely"`
	JailbreakConfirmed int            `json:"jailbreak_confirmed"`
	JailbreakMaxScore  int            `json:"jailbreak_max_score"`
	LastJailbreakAt    int64          `json:"last_jailbreak_at"`
	JailbreakTags      map[string]int `json:"jailbreak_tags,omitempty"`

	RelayRequests int            `json:"relay_requests"`
	RelayVendors  map[string]int `json:"relay_vendors,omitempty"`

	Clients   []ClientUsage  `json:"clients,omitempty"`
	Languages map[string]int `json:"languages,omitempty"`
}

// GetUserInsights 返回用户画像列表，支持按类别、风险等级、中转站来源筛选。
// 仅管理员可访问（路由层已挂 AdminAuth）。
//
// 分页策略：派生结论（主类别 / 风险等级）依赖多列组合，用 SQL 表达会牺牲
// 跨数据库兼容性，因此筛选在内存里做。但这意味着**不能**先 SQL 分页再筛选——
// 那样返回的 total 是筛选前的总数，前端会显示"1 / 5"却只有 3 行数据，
// 往后翻全是空页（这是修正前的行为）。
//
// 正确做法：无筛选条件时走 SQL 分页（快路径）；有筛选条件时全量取出、
// 内存筛选、再手工切片，total 用筛选后的真实条数。
// 画像表一行一个用户，规模等于站点用户数，全量取出可接受。
func HandleGetUserInsights(c contract.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	category := strings.TrimSpace(c.Query("category"))
	risk := strings.TrimSpace(c.Query("risk"))
	relayOnly := c.Query("relay_only") == "true"
	sortBy := c.DefaultQuery("sort", "last_seen")
	// keyword 支持按用户名 / 显示名 / 用户 ID 精确或子串匹配。
	// 与其它筛选一样在内存里做：画像表一行一个用户，且用户名需要
	// 先经 enrichWithUserStatus 补齐才完整，SQL 层拿不到 display_name。
	keyword := strings.ToLower(strings.TrimSpace(c.Query("keyword")))

	filtered := category != "" || risk != "" || relayOnly || keyword != ""

	var (
		views []insightProfileView
		total int64
	)
	if !filtered {
		// 快路径：无筛选，SQL 分页的 total 就是准确值。
		profiles, dbTotal, err := GetUserInsightProfiles((page-1)*pageSize, pageSize)
		if err != nil {
			common.CtxApiError(c, err)
			return
		}
		total = dbTotal
		views = make([]insightProfileView, 0, len(profiles))
		for _, profile := range profiles {
			views = append(views, buildInsightView(profile))
		}
		sortInsightViews(views, sortBy)
	} else {
		profiles, _, err := GetAllUserInsightProfiles()
		if err != nil {
			common.CtxApiError(c, err)
			return
		}
		matched := make([]insightProfileView, 0, len(profiles))
		for _, profile := range profiles {
			view := buildInsightView(profile)
			if category != "" && view.PrimaryCategory != category {
				continue
			}
			if risk != "" && view.RiskLevel != risk {
				continue
			}
			if relayOnly && view.RelayRequests == 0 {
				continue
			}
			if keyword != "" && !insightViewMatchesKeyword(&view, keyword) {
				continue
			}
			matched = append(matched, view)
		}
		// 先排序再切片：否则每页各自排序，跨页顺序是乱的。
		sortInsightViews(matched, sortBy)
		total = int64(len(matched))
		start := (page - 1) * pageSize
		if start > len(matched) {
			start = len(matched)
		}
		end := start + pageSize
		if end > len(matched) {
			end = len(matched)
		}
		views = matched[start:end]
	}
	enrichWithUserStatus(views)

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"items": views,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// insightViewMatchesKeyword 判断画像行是否命中搜索词。
//
// 只匹配画像表自带的 username 与 user_id：display_name 来自 users 表，
// 需要 enrichWithUserStatus 才有值，而那一步在分页切片之后才执行——
// 若为了搜索显示名而提前把全部候选行都补齐用户信息，等于把一次
// "IN (20 个 id)" 的查询放大成全站用户扫描。因此搜索范围限定为
// 用户名与 ID，前端的输入框提示也据此措辞。
func insightViewMatchesKeyword(view *insightProfileView, keyword string) bool {
	if keyword == "" {
		return true
	}
	if strings.Contains(strings.ToLower(view.Username), keyword) {
		return true
	}
	return strings.Contains(strconv.Itoa(view.UserId), keyword)
}

// GetUserInsightDetail 返回单个用户的画像详情。
func HandleGetUserInsightDetail(c contract.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	profile, err := GetUserInsightProfile(userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if profile == nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": true,
			"message": "",
			"data":    nil,
		})
		return
	}
	view := buildInsightView(profile)
	views := []insightProfileView{view}
	enrichWithUserStatus(views)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    views[0],
	})
}

// PurgeUserInsight 清除指定用户的全部画像数据。
//
// 提供这个口子的原因：画像是启发式结论，误判时管理员需要能把
// 该用户的历史清零重新观察；用户提出隐私异议时也需要能立即删除
// 含提示词原句的证据样本。一次调用清干净聚合画像、证据样本、
// 内存未落库增量与自动封禁标记（见 PurgeUserInsight）。
func HandlePurgeUserInsight(c contract.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := PurgeUserInsight(userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.SysLog(fmt.Sprintf("insight purged: user=%d operator=%d profile_deleted=%t samples_deleted=%d",
		userId, c.GetInt("id"), result.ProfileDeleted, result.SamplesDeleted))
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

// GetUserInsightSummary 返回站点级画像汇总，用于看板顶部的统计卡片。
func HandleGetUserInsightSummary(c contract.Context) {
	profiles, total, err := GetAllUserInsightProfiles()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	summary := common.H{
		"total_users":      total,
		"coders":           0,
		"roleplayers":      0,
		"qa_users":         0,
		"frontend_leaning": 0,
		"backend_leaning":  0,
		"male_leaning":     0,
		"female_leaning":   0,
		// *_self_report 是上面两项里"有用户自述"的子集。
		// 前端据此说明这块统计有多少来自反向推断——
		// 不标出来的话，一个纯反推得到的数字看起来和实名统计没有区别。
		"male_self_report":   0,
		"female_self_report": 0,
		"relay_users":        0,
		"risky_users":        0,
	}
	clientTotals := map[string]int{}
	for _, profile := range profiles {
		switch profile.PrimaryCategory() {
		case insight.CategoryCode:
			summary["coders"] = summary["coders"].(int) + 1
		case insight.CategoryRoleplay:
			summary["roleplayers"] = summary["roleplayers"].(int) + 1
		case insight.CategoryQA:
			summary["qa_users"] = summary["qa_users"].(int) + 1
		}
		switch profile.PrimaryStack() {
		case insight.StackFrontend:
			summary["frontend_leaning"] = summary["frontend_leaning"].(int) + 1
		case insight.StackBackend:
			summary["backend_leaning"] = summary["backend_leaning"].(int) + 1
		}
		gender, _, basis := profile.GuessedGender()
		switch gender {
		case insight.GenderMale:
			summary["male_leaning"] = summary["male_leaning"].(int) + 1
			if basis == insight.GenderBasisSelfReport {
				summary["male_self_report"] = summary["male_self_report"].(int) + 1
			}
		case insight.GenderFemale:
			summary["female_leaning"] = summary["female_leaning"].(int) + 1
			if basis == insight.GenderBasisSelfReport {
				summary["female_self_report"] = summary["female_self_report"].(int) + 1
			}
		}
		if profile.RelayRequests > 0 {
			summary["relay_users"] = summary["relay_users"].(int) + 1
		}
		if profile.RiskLevel() != insight.JailbreakNone {
			summary["risky_users"] = summary["risky_users"].(int) + 1
		}
		for _, usage := range DecodeClientUsage(profile.ClientsJSON) {
			clientTotals[usage.Client] += usage.Count
		}
	}
	summary["clients"] = clientTotals
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    summary,
	})
}

func buildInsightView(profile *UserInsightProfile) insightProfileView {
	gender, confidence, genderBasis := profile.GuessedGender()
	view := insightProfileView{
		UserId:             profile.UserId,
		Username:           profile.Username,
		FirstSeenAt:        profile.FirstSeenAt,
		LastSeenAt:         profile.LastSeenAt,
		TotalRequests:      profile.TotalRequests,
		CodeRequests:       profile.CodeRequests,
		RoleplayRequests:   profile.RoleplayRequests,
		QARequests:         profile.QARequests,
		TranslateRequests:  profile.TranslateRequests,
		EmbeddingRequests:  profile.EmbeddingRequests,
		OtherRequests:      profile.OtherRequests,
		PrimaryCategory:    profile.PrimaryCategory(),
		PrimaryStack:       profile.PrimaryStack(),
		Ratios:             profile.Ratios(),
		GuessedGender:      gender,
		GenderConfidence:   confidence,
		GenderBasis:        genderBasis,
		AIFemaleRequests:   profile.AIFemaleRequests,
		AIMaleRequests:     profile.AIMaleRequests,
		RiskLevel:          profile.RiskLevel(),
		JailbreakSuspect:   profile.JailbreakSuspect,
		JailbreakLikely:    profile.JailbreakLikely,
		JailbreakConfirmed: profile.JailbreakConfirmed,
		JailbreakMaxScore:  profile.JailbreakMaxScore,
		LastJailbreakAt:    profile.LastJailbreakAt,
		RelayRequests:      profile.RelayRequests,
		Clients:            DecodeClientUsage(profile.ClientsJSON),
	}
	if tags := DecodeCounter(profile.JailbreakTagsJSON); len(tags) > 0 {
		view.JailbreakTags = tags
	}
	if vendors := DecodeCounter(profile.RelayVendorsJSON); len(vendors) > 0 {
		view.RelayVendors = vendors
	}
	if languages := DecodeCounter(profile.LanguagesJSON); len(languages) > 0 {
		view.Languages = languages
	}
	return view
}

// enrichWithUserStatus 补齐用户当前状态与分组，供前端展示与封禁按钮判断。
func enrichWithUserStatus(views []insightProfileView) {
	if len(views) == 0 {
		return
	}
	ids := make([]int, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.UserId)
	}
	users, err := GetInsightUsersByIds(ids)
	if err != nil {
		return
	}
	byId := make(map[int]*identity.User, len(users))
	for _, user := range users {
		byId[user.Id] = user
	}
	for i := range views {
		user, ok := byId[views[i].UserId]
		if !ok {
			continue
		}
		views[i].Status = user.Status
		views[i].Role = user.Role
		views[i].Group = user.Group
		views[i].DisplayName = user.DisplayName
		if views[i].Username == "" {
			views[i].Username = user.Username
		}
	}
}

func sortInsightViews(views []insightProfileView, sortBy string) {
	switch sortBy {
	case "requests":
		sort.SliceStable(views, func(i, j int) bool {
			return views[i].TotalRequests > views[j].TotalRequests
		})
	case "risk":
		sort.SliceStable(views, func(i, j int) bool {
			return views[i].JailbreakMaxScore > views[j].JailbreakMaxScore
		})
	case "roleplay":
		sort.SliceStable(views, func(i, j int) bool {
			return views[i].RoleplayRequests > views[j].RoleplayRequests
		})
	case "code":
		sort.SliceStable(views, func(i, j int) bool {
			return views[i].CodeRequests > views[j].CodeRequests
		})
	default:
		sort.SliceStable(views, func(i, j int) bool {
			return views[i].LastSeenAt > views[j].LastSeenAt
		})
	}
}
