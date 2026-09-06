package usage

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/usage/insight"
	"gorm.io/gorm"
)

// UserInsightProfile 是用户级画像聚合表：每个用户一行，随请求增量累加。
// 明细仍然保存在消费日志的 other.insight 里，这张表只做看板与风控用的汇总，
// 因此所有 JSON 字段都使用 TEXT 以兼容 SQLite / MySQL / PostgreSQL。
type UserInsightProfile struct {
	UserId   int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Username string `json:"username" gorm:"size:64;index;default:''"`

	FirstSeenAt int64 `json:"first_seen_at" gorm:"bigint;default:0"`
	LastSeenAt  int64 `json:"last_seen_at" gorm:"bigint;index;default:0"`

	TotalRequests int `json:"total_requests" gorm:"default:0"`

	// 用途分布
	CodeRequests      int `json:"code_requests" gorm:"default:0"`
	RoleplayRequests  int `json:"roleplay_requests" gorm:"default:0"`
	QARequests        int `json:"qa_requests" gorm:"default:0"`
	TranslateRequests int `json:"translate_requests" gorm:"default:0"`
	EmbeddingRequests int `json:"embedding_requests" gorm:"default:0"`
	OtherRequests     int `json:"other_requests" gorm:"default:0"`

	// 代码方向累计分（除以 CodeRequests 得到平均倾向）
	FrontendScoreSum int `json:"frontend_score_sum" gorm:"default:0"`
	BackendScoreSum  int `json:"backend_score_sum" gorm:"default:0"`
	InfraRequests    int `json:"infra_requests" gorm:"default:0"`
	MobileRequests   int `json:"mobile_requests" gorm:"default:0"`
	DataRequests     int `json:"data_requests" gorm:"default:0"`

	// 角色扮演画像
	//
	// 性别推断按依据强度分三组计数，不能合并：
	// *SelfReport* 来自用户明确自述（"我是女生"），是强证据；
	// *Preference* 来自内容题材偏好（BL/GL 向内容的受众性别倾向），是中等证据；
	// *Guess* 是三级证据的总数，最弱的一档是 AI 角色性别取反的反向推断。
	// 合并后就无法区分"1 次自述"与"50 次反推"，而后者重复再多次
	// 也不会变可靠——这正是早期版本在 UI 上显示 90% 置信度的原因。
	MaleGuessRequests   int `json:"male_guess_requests" gorm:"default:0"`
	FemaleGuessRequests int `json:"female_guess_requests" gorm:"default:0"`
	// 自述计数是 Guess 计数的子集。
	MaleSelfReportRequests   int `json:"male_self_report_requests" gorm:"default:0"`
	FemaleSelfReportRequests int `json:"female_self_report_requests" gorm:"default:0"`
	// 内容偏好计数是 Guess 计数的子集，且强度介于自述与反推之间。
	// 偏好推断目前只指向女性（BL/GL 受众），故不设男性列，但保留结构对称。
	MalePreferenceRequests   int `json:"male_preference_requests" gorm:"default:0"`
	FemalePreferenceRequests int `json:"female_preference_requests" gorm:"default:0"`
	AIFemaleRequests         int `json:"ai_female_requests" gorm:"default:0"`
	AIMaleRequests           int `json:"ai_male_requests" gorm:"default:0"`

	// 破甲检测
	JailbreakSuspect   int   `json:"jailbreak_suspect" gorm:"default:0"`
	JailbreakLikely    int   `json:"jailbreak_likely" gorm:"default:0"`
	JailbreakConfirmed int   `json:"jailbreak_confirmed" gorm:"default:0"`
	JailbreakMaxScore  int   `json:"jailbreak_max_score" gorm:"default:0"`
	LastJailbreakAt    int64 `json:"last_jailbreak_at" gorm:"bigint;default:0"`

	// 中转站转发
	RelayRequests int `json:"relay_requests" gorm:"default:0"`

	// JSON 明细：client -> 次数、版本、厂商、破甲手法标签、技术栈标签
	ClientsJSON       string `json:"clients_json" gorm:"type:text"`
	RelayVendorsJSON  string `json:"relay_vendors_json" gorm:"type:text"`
	JailbreakTagsJSON string `json:"jailbreak_tags_json" gorm:"type:text"`
	LanguagesJSON     string `json:"languages_json" gorm:"type:text"`
}

func (UserInsightProfile) TableName() string {
	return "user_insight_profiles"
}

// insightDelta 是一次请求对某个用户画像的增量，先在内存里合并再批量落库，
// 避免每次 relay 都写一次数据库。
type insightDelta struct {
	profile        UserInsightProfile
	clients        map[string]int
	clientVersions map[string]string
	relayVendors   map[string]int
	jailbreakTags  map[string]int
	languages      map[string]int
}

var (
	insightCache     = make(map[int]*insightDelta)
	insightCacheLock sync.Mutex
)

// maxInsightMapEntries 限制 JSON 明细的条目数，防止异常输入把行撑爆。
const maxInsightMapEntries = 30

// RecordInsight 累加一次请求的画像结果到内存缓存。
func RecordInsight(userId int, username string, result *insight.Result) {
	if userId <= 0 || !result.HasProfile() {
		return
	}
	now := common.GetTimestamp()

	insightCacheLock.Lock()
	defer insightCacheLock.Unlock()

	delta, ok := insightCache[userId]
	if !ok {
		delta = &insightDelta{
			clients:        map[string]int{},
			clientVersions: map[string]string{},
			relayVendors:   map[string]int{},
			jailbreakTags:  map[string]int{},
			languages:      map[string]int{},
		}
		insightCache[userId] = delta
	}
	p := &delta.profile
	p.UserId = userId
	p.Username = username
	p.LastSeenAt = now
	if p.FirstSeenAt == 0 {
		p.FirstSeenAt = now
	}
	p.TotalRequests++

	switch result.Category {
	case insight.CategoryCode:
		p.CodeRequests++
	case insight.CategoryRoleplay:
		p.RoleplayRequests++
	case insight.CategoryQA:
		p.QARequests++
	case insight.CategoryTranslate:
		p.TranslateRequests++
	case insight.CategoryEmbedding:
		p.EmbeddingRequests++
	default:
		p.OtherRequests++
	}

	p.FrontendScoreSum += result.StackFront
	p.BackendScoreSum += result.StackBack
	switch result.Stack {
	case insight.StackInfra:
		p.InfraRequests++
	case insight.StackMobile:
		p.MobileRequests++
	case insight.StackData:
		p.DataRequests++
	}

	switch result.GuessGender {
	case insight.GenderMale:
		p.MaleGuessRequests++
		switch result.GenderBasis {
		case insight.GenderBasisSelfReport:
			p.MaleSelfReportRequests++
		case insight.GenderBasisPreference:
			p.MalePreferenceRequests++
		}
	case insight.GenderFemale:
		p.FemaleGuessRequests++
		switch result.GenderBasis {
		case insight.GenderBasisSelfReport:
			p.FemaleSelfReportRequests++
		case insight.GenderBasisPreference:
			p.FemalePreferenceRequests++
		}
	}
	switch result.AIGender {
	case insight.GenderFemale:
		p.AIFemaleRequests++
	case insight.GenderMale:
		p.AIMaleRequests++
	}

	switch result.JailbreakLevel {
	case insight.JailbreakSuspect:
		p.JailbreakSuspect++
	case insight.JailbreakLikely:
		p.JailbreakLikely++
	case insight.JailbreakConfirmed:
		p.JailbreakConfirmed++
	}
	if result.JailbreakScore > p.JailbreakMaxScore {
		p.JailbreakMaxScore = result.JailbreakScore
	}
	if result.Jailbreak {
		p.LastJailbreakAt = now
	}
	if result.IsRelay {
		p.RelayRequests++
		if result.RelayVendor != "" {
			incrementBounded(delta.relayVendors, result.RelayVendor)
		}
	}
	if result.Client != "" {
		incrementBounded(delta.clients, result.Client)
		if result.ClientVersion != "" {
			delta.clientVersions[result.Client] = result.ClientVersion
		}
	}
	for _, tag := range result.JailbreakTags {
		incrementBounded(delta.jailbreakTags, tag)
	}
	for _, lang := range result.Languages {
		incrementBounded(delta.languages, lang)
	}
}

func incrementBounded(counter map[string]int, key string) {
	if _, exists := counter[key]; !exists && len(counter) >= maxInsightMapEntries {
		return
	}
	counter[key]++
}

// UpdateInsightProfiles 周期性把内存增量写入数据库，与 UpdateQuotaData 同样由 main 启动。
func UpdateInsightProfiles() {
	for {
		time.Sleep(time.Minute)
		if err := FlushInsightCache(); err != nil {
			common.SysError("failed to flush insight cache: " + err.Error())
		}
	}
}

// FlushInsightCache 将内存中的画像增量合并进数据库。
func FlushInsightCache() error {
	insightCacheLock.Lock()
	pending := insightCache
	insightCache = make(map[int]*insightDelta)
	insightCacheLock.Unlock()

	if len(pending) == 0 {
		return nil
	}
	// 自动封禁的候选：只有"本批出现过破甲命中"或"本批出现过代码请求"的用户
	// 才需要重新评估，因为这两个维度是判定条件的唯一变量。
	// 这样避免每次 flush 都对全部活跃用户查库。
	candidates := make(map[int]string)
	for userId, delta := range pending {
		if err := mergeInsightDelta(userId, delta); err != nil {
			return err
		}
		d := delta.profile
		if d.JailbreakSuspect+d.JailbreakLikely+d.JailbreakConfirmed > 0 || d.CodeRequests > 0 {
			candidates[userId] = d.Username
		}
	}
	// 评估放在合并全部完成之后，保证读到的画像是最新落库值。
	for userId, username := range candidates {
		EvaluateInsightAutoBan(userId, username)
	}
	return nil
}

func mergeInsightDelta(userId int, delta *insightDelta) error {
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var existing UserInsightProfile
		err := dbx.LockForUpdate(tx).Where("user_id = ?", userId).First(&existing).Error
		if err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			// 首次出现：直接写入增量作为初始值。
			row := delta.profile
			row.ClientsJSON = marshalCounterWithVersions(delta.clients, delta.clientVersions)
			row.RelayVendorsJSON = marshalCounter(delta.relayVendors)
			row.JailbreakTagsJSON = marshalCounter(delta.jailbreakTags)
			row.LanguagesJSON = marshalCounter(delta.languages)
			return tx.Create(&row).Error
		}

		d := delta.profile
		existing.Username = d.Username
		existing.LastSeenAt = d.LastSeenAt
		if existing.FirstSeenAt == 0 {
			existing.FirstSeenAt = d.FirstSeenAt
		}
		existing.TotalRequests += d.TotalRequests
		existing.CodeRequests += d.CodeRequests
		existing.RoleplayRequests += d.RoleplayRequests
		existing.QARequests += d.QARequests
		existing.TranslateRequests += d.TranslateRequests
		existing.EmbeddingRequests += d.EmbeddingRequests
		existing.OtherRequests += d.OtherRequests
		existing.FrontendScoreSum += d.FrontendScoreSum
		existing.BackendScoreSum += d.BackendScoreSum
		existing.InfraRequests += d.InfraRequests
		existing.MobileRequests += d.MobileRequests
		existing.DataRequests += d.DataRequests
		existing.MaleGuessRequests += d.MaleGuessRequests
		existing.FemaleGuessRequests += d.FemaleGuessRequests
		existing.MaleSelfReportRequests += d.MaleSelfReportRequests
		existing.FemaleSelfReportRequests += d.FemaleSelfReportRequests
		existing.MalePreferenceRequests += d.MalePreferenceRequests
		existing.FemalePreferenceRequests += d.FemalePreferenceRequests
		existing.AIFemaleRequests += d.AIFemaleRequests
		existing.AIMaleRequests += d.AIMaleRequests
		existing.JailbreakSuspect += d.JailbreakSuspect
		existing.JailbreakLikely += d.JailbreakLikely
		existing.JailbreakConfirmed += d.JailbreakConfirmed
		if d.JailbreakMaxScore > existing.JailbreakMaxScore {
			existing.JailbreakMaxScore = d.JailbreakMaxScore
		}
		if d.LastJailbreakAt > existing.LastJailbreakAt {
			existing.LastJailbreakAt = d.LastJailbreakAt
		}
		existing.RelayRequests += d.RelayRequests

		existing.ClientsJSON = mergeCounterJSONWithVersions(existing.ClientsJSON, delta.clients, delta.clientVersions)
		existing.RelayVendorsJSON = mergeCounterJSON(existing.RelayVendorsJSON, delta.relayVendors)
		existing.JailbreakTagsJSON = mergeCounterJSON(existing.JailbreakTagsJSON, delta.jailbreakTags)
		existing.LanguagesJSON = mergeCounterJSON(existing.LanguagesJSON, delta.languages)

		return tx.Save(&existing).Error
	})
}

// ClientUsage 是 ClientsJSON 的解码结果，供 API 层直接返回。
type ClientUsage struct {
	Client  string `json:"client"`
	Count   int    `json:"count"`
	Version string `json:"version,omitempty"`
}

func marshalCounter(counter map[string]int) string {
	if len(counter) == 0 {
		return ""
	}
	bytes, err := common.Marshal(counter)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func marshalCounterWithVersions(counter map[string]int, versions map[string]string) string {
	if len(counter) == 0 {
		return ""
	}
	items := make([]ClientUsage, 0, len(counter))
	for client, count := range counter {
		items = append(items, ClientUsage{Client: client, Count: count, Version: versions[client]})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Count > items[j].Count })
	bytes, err := common.Marshal(items)
	if err != nil {
		return ""
	}
	return string(bytes)
}

// DecodeCounter 解析计数型 JSON 字段，解析失败时返回空 map 而不是报错，
// 保证看板在历史脏数据下仍可渲染。
func DecodeCounter(raw string) map[string]int {
	result := map[string]int{}
	if raw == "" {
		return result
	}
	if err := common.UnmarshalJsonStr(raw, &result); err != nil {
		return map[string]int{}
	}
	return result
}

// DecodeClientUsage 解析客户端使用明细。
func DecodeClientUsage(raw string) []ClientUsage {
	var result []ClientUsage
	if raw == "" {
		return nil
	}
	if err := common.UnmarshalJsonStr(raw, &result); err != nil {
		return nil
	}
	return result
}

func mergeCounterJSON(raw string, delta map[string]int) string {
	if len(delta) == 0 {
		return raw
	}
	merged := DecodeCounter(raw)
	for key, value := range delta {
		if _, exists := merged[key]; !exists && len(merged) >= maxInsightMapEntries {
			continue
		}
		merged[key] += value
	}
	return marshalCounter(merged)
}

func mergeCounterJSONWithVersions(raw string, delta map[string]int, versions map[string]string) string {
	if len(delta) == 0 {
		return raw
	}
	existing := DecodeClientUsage(raw)
	counter := make(map[string]int, len(existing)+len(delta))
	versionMap := make(map[string]string, len(existing)+len(versions))
	for _, item := range existing {
		counter[item.Client] = item.Count
		if item.Version != "" {
			versionMap[item.Client] = item.Version
		}
	}
	for client, count := range delta {
		if _, exists := counter[client]; !exists && len(counter) >= maxInsightMapEntries {
			continue
		}
		counter[client] += count
	}
	for client, version := range versions {
		// 版本以最近一次上报为准，便于发现用户升级了工具。
		versionMap[client] = version
	}
	return marshalCounterWithVersions(counter, versionMap)
}

// GetUserInsightProfiles 按最后活跃时间倒序分页返回画像。
func GetUserInsightProfiles(offset, limit int) (profiles []*UserInsightProfile, total int64, err error) {
	if err = dbx.DB.Model(&UserInsightProfile{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = dbx.DB.Order("last_seen_at desc").Offset(offset).Limit(limit).Find(&profiles).Error
	return profiles, total, err
}

// GetAllUserInsightProfiles 全量返回画像，供需要在内存里做派生字段筛选/统计的场景。
//
// 画像表一行一个用户，规模等于站点用户数，全量读取可接受；
// 但仍设一个硬上限，避免用户数异常膨胀时把这条路径变成 OOM 来源。
// 触顶时 total 仍返回真实总数，调用方可据此判断结果是否完整。
func GetAllUserInsightProfiles() (profiles []*UserInsightProfile, total int64, err error) {
	if err = dbx.DB.Model(&UserInsightProfile{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = dbx.DB.Order("last_seen_at desc").Limit(maxInsightProfileScan).Find(&profiles).Error
	return profiles, total, err
}

// maxInsightProfileScan 是全量扫描的行数上限。
const maxInsightProfileScan = 20000

// GetUserInsightProfile 返回单个用户的画像，不存在时返回 nil。
func GetUserInsightProfile(userId int) (*UserInsightProfile, error) {
	var profile UserInsightProfile
	err := dbx.DB.Where("user_id = ?", userId).First(&profile).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// PurgeUserInsightResult 是一次"清除画像"操作的结果，供 API 回显给管理员。
type PurgeUserInsightResult struct {
	ProfileDeleted bool  `json:"profile_deleted"`
	SamplesDeleted int64 `json:"samples_deleted"`
}

// PurgeUserInsight 一次性清干净某个用户的全部画像痕迹。
//
// 必须同时处理四处，否则"清除"只是看起来清了：
//  1. 内存增量缓存 insightCache —— 里面是还没落库的计数，
//     不清的话下一次 FlushInsightCache 会把画像重新写回来（这是最容易漏的一处）；
//  2. 聚合画像行 user_insight_profiles；
//  3. 证据样本 user_insight_samples（含提示词原句，用户提异议时必须一起删）；
//  4. 自动封禁标记 autoBanOnce —— 画像归零后该标记也应复位，
//     否则用户之后真的再次触发风险却不会被重新评估。
//
// 顺序很重要：先摘掉内存增量，再删库。反过来会有一个窗口期，
// 让 flush 把刚删掉的行又插回去。
func PurgeUserInsight(userId int) (*PurgeUserInsightResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}

	insightCacheLock.Lock()
	delete(insightCache, userId)
	insightCacheLock.Unlock()

	result := &PurgeUserInsightResult{}

	deleted := dbx.DB.Where("user_id = ?", userId).Delete(&UserInsightProfile{})
	if deleted.Error != nil {
		return nil, deleted.Error
	}
	result.ProfileDeleted = deleted.RowsAffected > 0

	samples, err := DeleteInsightSamplesByUser(userId)
	if err != nil {
		return nil, err
	}
	result.SamplesDeleted = samples

	// 画像已归零，自动封禁标记必须复位，否则该用户永远不会被再次评估。
	ResetInsightAutoBanMark(userId)

	return result, nil
}

// GetInsightUsersByIds 批量读取画像所属用户的状态字段，供看板列表补齐展示信息。
// 只 Select 需要的列，避免把密码哈希等敏感字段带出数据层。
func GetInsightUsersByIds(ids []int) ([]*identity.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []*identity.User
	err := dbx.DB.Model(&identity.User{}).
		Select("id", "username", "display_name", "status", "role", dbx.GroupCol()).
		Where("id IN ?", ids).
		Find(&users).Error
	return users, err
}
