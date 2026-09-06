package usage

import (
	"github.com/QuantumNous/new-api/internal/common"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// insightSampleView 是复核样本的前端视图。
// 列表接口不返回 body，详情接口才带原文。
type insightSampleView struct {
	Id        int64  `json:"id"`
	UserId    int    `json:"user_id"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
	RequestId string `json:"request_id,omitempty"`
	ModelName string `json:"model_name,omitempty"`
	Path      string `json:"path,omitempty"`

	Category      string `json:"category,omitempty"`
	Client        string `json:"client,omitempty"`
	ClientVersion string `json:"client_version,omitempty"`
	IsRelay       bool   `json:"is_relay"`
	RelayVendor   string `json:"relay_vendor,omitempty"`

	RiskLevel      string `json:"risk_level,omitempty"`
	JailbreakScore int    `json:"jailbreak_score"`
	GuessGender    string `json:"guess_gender,omitempty"`
	// GenderBasis 区分自述与反向推断，前端据此标注证据强度。
	GenderBasis string `json:"gender_basis,omitempty"`

	// Evidence 是命中关键词与原文片段，这是"人工查看命中句子"的核心数据。
	Evidence []insight.Evidence `json:"evidence,omitempty"`
	// EvidenceCount 便于列表直接展示"命中 N 条"。
	EvidenceCount int `json:"evidence_count"`

	// HitCount 是同一命中模式累计出现的请求次数。
	// 去重后一行代表一种模式，前端需要它才能表达"这类请求出现了 N 次"。
	HitCount int `json:"hit_count"`
	// LastSeenAt 是该模式最近一次出现的时间。
	LastSeenAt int64 `json:"last_seen_at"`

	// Body 仅详情接口返回，且需要开启完整留存才有值。
	Body     string `json:"body,omitempty"`
	BodySize int    `json:"body_size"`
	ByteSize int    `json:"byte_size"`
}

func buildSampleView(sample *UserInsightSample, withBody bool) insightSampleView {
	evidence := sample.DecodeEvidence()
	view := insightSampleView{
		Id:             sample.Id,
		UserId:         sample.UserId,
		Username:       sample.Username,
		CreatedAt:      sample.CreatedAt,
		RequestId:      sample.RequestId,
		ModelName:      sample.ModelName,
		Path:           sample.Path,
		Category:       sample.Category,
		Client:         sample.Client,
		ClientVersion:  sample.ClientVersion,
		IsRelay:        sample.IsRelay,
		RelayVendor:    sample.RelayVendor,
		RiskLevel:      sample.RiskLevel,
		JailbreakScore: sample.JailbreakScore,
		GuessGender:    sample.GuessGender,
		GenderBasis:    sample.GenderBasis,
		Evidence:       evidence,
		EvidenceCount:  len(evidence),
		HitCount:       sample.HitCount,
		LastSeenAt:     sample.LastSeenAt,
		BodySize:       sample.BodySize,
		ByteSize:       sample.ByteSize,
	}
	if withBody {
		view.Body = sample.Body
	}
	return view
}

// GetInsightSamples 返回复核样本列表。
// 支持按用户、风险等级筛选，以及只看中转站流量。
func HandleGetInsightSamples(c contract.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	userId, _ := strconv.Atoi(c.Query("user_id"))
	riskLevel := c.Query("risk")
	relayOnly := c.Query("relay_only") == "true"

	samples, total, err := GetInsightSamples(userId, riskLevel, relayOnly,
		(page-1)*pageSize, pageSize)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	views := make([]insightSampleView, 0, len(samples))
	for _, sample := range samples {
		views = append(views, buildSampleView(sample, false))
	}

	setting := GetUserInsightSetting()
	used, limit := InsightSampleUsage(setting.SampleQuotaBytes())

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"items": views,
			"page":  page,
			"size":  pageSize,
			"total": total,
			// 容量信息随列表一起返回，方便看板直接显示"已用 / 上限"。
			"quota": common.H{
				"used_bytes":  used,
				"limit_bytes": limit,
				"keep_body":   setting.SampleKeepBody,
				"sample_rate": setting.SampleRate(),
				"enabled":     setting.SampleEnabled,
			},
		},
	})
}

// GetInsightSampleGroups 返回按用户聚合的样本概览。
// 看板默认用这个接口："一个用户一行"，点开才拉该用户的具体证据，
// 否则单个 agent 用户能刷出几百条几乎相同的记录把界面占满。
func HandleGetInsightSampleGroups(c contract.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	riskLevel := c.Query("risk")
	relayOnly := c.Query("relay_only") == "true"

	groups, total, err := GetInsightSampleUserGroups(riskLevel, relayOnly,
		(page-1)*pageSize, pageSize)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	setting := GetUserInsightSetting()
	used, limit := InsightSampleUsage(setting.SampleQuotaBytes())

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"items": groups,
			"page":  page,
			"size":  pageSize,
			"total": total,
			"quota": common.H{
				"used_bytes":  used,
				"limit_bytes": limit,
				"keep_body":   setting.SampleKeepBody,
				"sample_rate": setting.SampleRate(),
				"enabled":     setting.SampleEnabled,
			},
		},
	})
}

// GetInsightSampleDetail 返回单条样本，含请求体原文（若已留存）。
func HandleGetInsightSampleDetail(c contract.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": i18n.TCtx(c, i18n.MsgInvalidParams),
		})
		return
	}
	sample, err := GetInsightSample(id)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if sample == nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": i18n.TCtx(c, i18n.MsgNotFound),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    buildSampleView(sample, true),
	})
}

// DeleteUserInsightSamples 删除指定用户的全部复核样本。
// 提供这个口子是因为样本里可能含用户提示词原文，
// 管理员需要能在用户提出异议时立即清除。
func HandleDeleteUserInsightSamples(c contract.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": i18n.TCtx(c, i18n.MsgInvalidParams),
		})
		return
	}
	deleted, err := DeleteInsightSamplesByUser(userId)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    common.H{"deleted": deleted},
	})
}
