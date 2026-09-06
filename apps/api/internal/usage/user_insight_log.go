package usage

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// attachInsight 把 UserInsight 中间件产出的画像结果并入消费日志的 other 字段，
// 同时累加到用户级画像聚合缓存。
//
// 放在 RecordConsumeLog 这一处统一处理，是因为 text / claude / audio / wss / image
// 各条计费路径最终都会汇聚到这里，避免在每个 relay handler 里重复接线。
func attachInsight(c contract.Context, userId int, username string, other map[string]interface{}) {
	if c == nil {
		return
	}
	setting := GetUserInsightSetting()
	if !setting.Enabled {
		return
	}
	value, exists := c.Get(string(constant.ContextKeyUserInsight))
	if !exists {
		return
	}
	result, ok := value.(*insight.Result)
	if !ok || result == nil {
		return
	}

	RecordInsight(userId, username, result)

	// 复核样本：命中原句 + 可选请求体原文。写库放在这里而不是中间件，
	// 是为了拿到 model 名与请求 id，同时不占用 relay 热路径。
	if setting.SampleEnabled && insight.ShouldCollect(result, setting.SampleRate()) {
		requestId := ""
		if id, ok := c.Get(common.RequestIdKey); ok {
			requestId, _ = id.(string)
		}
		modelName := ""
		if name, ok := c.Get(string(constant.ContextKeyOriginalModel)); ok {
			modelName, _ = name.(string)
		}
		requestPath := c.Path()
		RecordInsightSample(userId, username, requestId, modelName, requestPath,
			result, setting.SampleKeepBody, setting.SampleQuotaBytes())
	}

	if setting.RecordInLog && other != nil {
		other["insight"] = result
	}
	if result.JailbreakScore >= setting.JailbreakAlertThreshold() {
		logger.LogWarn(c.Context(), fmt.Sprintf(
			"insight: jailbreak attempt detected user=%d username=%s score=%d level=%s vector=%s tags=%v client=%s",
			userId, username, result.JailbreakScore, result.JailbreakLevel,
			result.JailbreakVector, result.JailbreakTags, result.Client))
	}
	if result.IsRelay {
		logger.LogInfo(c.Context(), fmt.Sprintf(
			"insight: upstream relay detected user=%d vendor=%s score=%d reasons=%v",
			userId, result.RelayVendor, result.RelayScore, result.RelayReasons))
	}
}

// InsightRatios 是画像的派生比例，供 API 层直接返回给前端，避免前端重复计算。
type InsightRatios struct {
	CodeRatio      float64 `json:"code_ratio"`
	RoleplayRatio  float64 `json:"roleplay_ratio"`
	QARatio        float64 `json:"qa_ratio"`
	TranslateRatio float64 `json:"translate_ratio"`
	OtherRatio     float64 `json:"other_ratio"`
	// FrontendRatio / BackendRatio 是代码请求内部的前后端倾向占比。
	FrontendRatio float64 `json:"frontend_ratio"`
	BackendRatio  float64 `json:"backend_ratio"`
	// MaleRatio / FemaleRatio 是角色扮演请求里推断出的性别倾向占比。
	MaleRatio   float64 `json:"male_ratio"`
	FemaleRatio float64 `json:"female_ratio"`
}

// Ratios 计算画像比例。分母为 0 时返回 0，不产生 NaN。
func (p *UserInsightProfile) Ratios() InsightRatios {
	ratios := InsightRatios{}
	if p == nil {
		return ratios
	}
	if p.TotalRequests > 0 {
		total := float64(p.TotalRequests)
		ratios.CodeRatio = float64(p.CodeRequests) / total
		ratios.RoleplayRatio = float64(p.RoleplayRequests) / total
		ratios.QARatio = float64(p.QARequests) / total
		ratios.TranslateRatio = float64(p.TranslateRequests) / total
		ratios.OtherRatio = float64(p.OtherRequests+p.EmbeddingRequests) / total
	}
	if stackTotal := p.FrontendScoreSum + p.BackendScoreSum; stackTotal > 0 {
		ratios.FrontendRatio = float64(p.FrontendScoreSum) / float64(stackTotal)
		ratios.BackendRatio = float64(p.BackendScoreSum) / float64(stackTotal)
	}
	if genderTotal := p.MaleGuessRequests + p.FemaleGuessRequests; genderTotal > 0 {
		ratios.MaleRatio = float64(p.MaleGuessRequests) / float64(genderTotal)
		ratios.FemaleRatio = float64(p.FemaleGuessRequests) / float64(genderTotal)
	}
	return ratios
}

// PrimaryCategory 返回请求量最大的用途类别。
func (p *UserInsightProfile) PrimaryCategory() string {
	if p == nil || p.TotalRequests == 0 {
		return insight.CategoryOther
	}
	best, bestCount := insight.CategoryOther, 0
	for _, candidate := range []struct {
		name  string
		count int
	}{
		{insight.CategoryCode, p.CodeRequests},
		{insight.CategoryRoleplay, p.RoleplayRequests},
		{insight.CategoryQA, p.QARequests},
		{insight.CategoryTranslate, p.TranslateRequests},
		{insight.CategoryEmbedding, p.EmbeddingRequests},
	} {
		if candidate.count > bestCount {
			best, bestCount = candidate.name, candidate.count
		}
	}
	return best
}

// PrimaryStack 返回代码请求的主要技术栈方向。
func (p *UserInsightProfile) PrimaryStack() string {
	if p == nil || p.CodeRequests == 0 {
		return insight.StackUnknown
	}
	// 专项方向（运维/移动/数据）请求占比过半时优先展示，否则回到前后端对比。
	half := p.CodeRequests / 2
	switch {
	case p.InfraRequests > half:
		return insight.StackInfra
	case p.MobileRequests > half:
		return insight.StackMobile
	case p.DataRequests > half:
		return insight.StackData
	}
	front, back := p.FrontendScoreSum, p.BackendScoreSum
	if front == 0 && back == 0 {
		return insight.StackUnknown
	}
	total := front + back
	// 双方都不低于 35% 视为全栈。
	if front*100 >= total*35 && back*100 >= total*35 {
		return insight.StackFull
	}
	if front > back {
		return insight.StackFrontend
	}
	return insight.StackBackend
}

// GuessedGender 返回用户性别倾向推断、置信度与依据强度。
//
// 置信度按依据分三档，强度递减，这是本次修正的核心——
// 不同来源的证据不能混成同一个"一致率"：
//
//   - 有自述（self_report）：用自述计数算一致率，上限 0.9。
//     用户自己写的，重复出现确实提升可信度。
//   - 有内容偏好（preference）：BL/GL 题材的受众性别倾向。
//     折扣系数 0.7、上限 0.7。比反推可靠（抓的是主动选的题材），
//     但仍是群体统计，不能等同自述。
//   - 只有反向推断（inverse）：一致率乘 0.4 并封顶 0.45。
//     40 分的群体先验，对单个用户判别力很弱，重复再多次也不升级。
//
// 三档按优先级短路：有自述就只看自述，没有再看偏好，最后才是反推。
// 早期版本把所有来源混成一个"多次结论一致率"，导致 40 分弱先验在 UI 上
// 显示成 90% 置信度，摆在封禁按钮旁边。
func (p *UserInsightProfile) GuessedGender() (gender string, confidence float64, basis string) {
	if p == nil {
		return insight.GenderUnknown, 0, ""
	}
	// 第一档：自述。只要有自述样本，就完全按自述判定，忽略其余计数。
	selfTotal := p.MaleSelfReportRequests + p.FemaleSelfReportRequests
	if selfTotal > 0 {
		gender, confidence = pickGender(
			p.MaleSelfReportRequests, p.FemaleSelfReportRequests, selfTotal, 1)
		if gender == insight.GenderUnknown {
			return gender, confidence, ""
		}
		if confidence > 0.9 {
			confidence = 0.9
		}
		return gender, confidence, insight.GenderBasisSelfReport
	}

	// 第二档：内容偏好。介于自述与反推之间。
	prefTotal := p.MalePreferenceRequests + p.FemalePreferenceRequests
	if prefTotal > 0 {
		gender, confidence = pickGender(
			p.MalePreferenceRequests, p.FemalePreferenceRequests, prefTotal, preferenceGenderPrior)
		if gender == insight.GenderUnknown {
			return gender, confidence, ""
		}
		if confidence > maxPreferenceGenderConfidence {
			confidence = maxPreferenceGenderConfidence
		}
		return gender, confidence, insight.GenderBasisPreference
	}

	// 第三档：纯反向推断。计数里减去更强档的贡献，只用真正的反推样本。
	inverseMale := p.MaleGuessRequests - p.MaleSelfReportRequests - p.MalePreferenceRequests
	inverseFemale := p.FemaleGuessRequests - p.FemaleSelfReportRequests - p.FemalePreferenceRequests
	if inverseMale < 0 {
		inverseMale = 0
	}
	if inverseFemale < 0 {
		inverseFemale = 0
	}
	total := inverseMale + inverseFemale
	// 样本过少时不下结论：单次角色卡不足以代表用户。
	if total < 3 {
		return insight.GenderUnknown, 0, ""
	}
	gender, confidence = pickGender(inverseMale, inverseFemale, total, inverseGenderPrior)
	if gender == insight.GenderUnknown {
		return gender, confidence, ""
	}
	if confidence > maxInverseGenderConfidence {
		confidence = maxInverseGenderConfidence
	}
	return gender, confidence, insight.GenderBasisInverse
}

const (
	// inverseGenderPrior 是反向推断的置信度折扣系数，对应 inferGender 给出的 40 分。
	inverseGenderPrior = 0.4
	// maxInverseGenderConfidence 是纯反推结论的置信度上限。
	// 弱证据不因重复次数变强，因此必须有硬顶。
	maxInverseGenderConfidence = 0.45
	// preferenceGenderPrior 是内容偏好推断的置信度折扣系数。
	preferenceGenderPrior = 0.7
	// maxPreferenceGenderConfidence 是偏好推断结论的置信度上限。
	maxPreferenceGenderConfidence = 0.7
)

// pickGender 按多数方判定性别，一致率低于 60% 视为无结论。
// prior 是依据强度折扣系数（自述为 1，反推为 inverseGenderPrior）。
func pickGender(male, female, total int, prior float64) (string, float64) {
	if total <= 0 {
		return insight.GenderUnknown, 0
	}
	switch {
	case male > female:
		ratio := float64(male) / float64(total)
		if ratio < 0.6 {
			return insight.GenderUnknown, ratio * prior
		}
		return insight.GenderMale, ratio * prior
	case female > male:
		ratio := float64(female) / float64(total)
		if ratio < 0.6 {
			return insight.GenderUnknown, ratio * prior
		}
		return insight.GenderFemale, ratio * prior
	default:
		return insight.GenderUnknown, 0.5 * prior
	}
}

// RiskLevel 汇总破甲风险等级，供看板排序与筛选。
func (p *UserInsightProfile) RiskLevel() string {
	if p == nil {
		return insight.JailbreakNone
	}
	switch {
	case p.JailbreakConfirmed >= 3 || p.JailbreakMaxScore >= 85:
		return insight.JailbreakConfirmed
	case p.JailbreakConfirmed > 0 || p.JailbreakLikely >= 3:
		return insight.JailbreakLikely
	case p.JailbreakLikely > 0 || p.JailbreakSuspect >= 5:
		return insight.JailbreakSuspect
	default:
		return insight.JailbreakNone
	}
}
