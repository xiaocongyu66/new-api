package usage

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/usage/insight"
	"gorm.io/gorm"
)

// UserInsightSample 保存单次请求的人工复核材料：命中关键词的原句片段，
// 以及可选的请求体原文。这张表是有容量上限的滚动缓存，
// 超出上限时按"优先级低 + 时间早"淘汰，因此它不是审计留档，
// 只是给管理员一个"看到判定依据"的窗口。
type UserInsightSample struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"user_id" gorm:"index:idx_insight_sample_user,priority:1"`
	Username  string `json:"username" gorm:"size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_insight_sample_user,priority:2;index:idx_insight_sample_created"`

	RequestId string `json:"request_id" gorm:"size:64;default:''"`
	ModelName string `json:"model_name" gorm:"size:128;default:''"`
	Path      string `json:"path" gorm:"size:128;default:''"`

	// 判定摘要，便于列表筛选时不必解析 evidence。
	Category       string `json:"category" gorm:"size:32;default:''"`
	Client         string `json:"client" gorm:"size:64;default:''"`
	ClientVersion  string `json:"client_version" gorm:"size:32;default:''"`
	IsRelay        bool   `json:"is_relay" gorm:"index"`
	RelayVendor    string `json:"relay_vendor" gorm:"size:32;default:''"`
	RiskLevel      string `json:"risk_level" gorm:"size:16;index;default:''"`
	JailbreakScore int    `json:"jailbreak_score" gorm:"default:0"`
	GuessGender    string `json:"guess_gender" gorm:"size:16;default:''"`
	// GenderBasis 是 GuessGender 的依据：self_report / inverse。
	// 复核面板必须显示它，否则管理员无法区分"用户自己说的"与"按角色卡反推的"。
	GenderBasis string `json:"gender_basis" gorm:"size:16;default:''"`

	// EvidenceJSON 是 []insight.Evidence 的序列化结果。
	EvidenceJSON string `json:"evidence_json" gorm:"type:text"`
	// Body 是请求体前缀原文，仅在开启完整留存时才有值。
	Body string `json:"body" gorm:"type:text"`
	// BodySize 是原始请求体大小，用于提示"已截断"。
	BodySize int `json:"body_size" gorm:"default:0"`

	// Fingerprint 是"同一类请求"的指纹：命中关键词集合 + 类别 + 客户端。
	// agent 客户端每次请求都带同一份注入提示词，命中词完全一样，
	// 逐条留存会出现"一个用户 499 条几乎相同的证据"，既占配额又没有复核价值。
	// 同指纹的后续请求只累加 HitCount 并刷新时间，不再新增行。
	Fingerprint string `json:"fingerprint" gorm:"size:32;index:idx_insight_sample_fp,priority:2;default:''"`
	// HitCount 是该指纹累计出现次数，首次写入为 1。
	HitCount int `json:"hit_count" gorm:"default:1"`
	// LastSeenAt 是该指纹最近一次出现的时间。
	LastSeenAt int64 `json:"last_seen_at" gorm:"bigint;default:0"`

	// ByteSize 是这一行的估算占用，用于容量核算。
	// 显式存储而不是每次 SUM(length(...))，因为后者在三种数据库上写法不一致。
	ByteSize int `json:"byte_size" gorm:"default:0"`

	// Priority 决定淘汰顺序：数值越小越先被删。
	// 0=普通采样，1=中转站，2=破甲命中。
	Priority int `json:"priority" gorm:"index;default:0"`
}

func (UserInsightSample) TableName() string {
	return "user_insight_samples"
}

// 淘汰优先级常量。
const (
	samplePriorityNormal    = 0
	samplePriorityRelay     = 1
	samplePriorityJailbreak = 2
)

var (
	// sampleBytesUsed 是样本表的估算总占用，进程启动时从数据库汇总一次，
	// 之后随写入/淘汰增量维护，避免每次插入都做一次全表聚合。
	sampleBytesUsed  int64
	sampleBytesLock  sync.Mutex
	sampleBytesReady bool
)

// InitInsightSampleUsage 在启动时汇总一次已用容量。
func InitInsightSampleUsage() {
	var total int64
	err := dbx.DB.Model(&UserInsightSample{}).
		Select("COALESCE(SUM(byte_size), 0)").
		Scan(&total).Error
	sampleBytesLock.Lock()
	defer sampleBytesLock.Unlock()
	if err != nil {
		// 汇总失败时标记未就绪，后续按保守策略每次重算。
		sampleBytesReady = false
		common.SysError("failed to sum insight sample usage: " + err.Error())
		return
	}
	sampleBytesUsed = total
	sampleBytesReady = true
}

// InsightSampleUsage 返回当前占用与配额（字节）。
func InsightSampleUsage(limitBytes int64) (used int64, limit int64) {
	sampleBytesLock.Lock()
	defer sampleBytesLock.Unlock()
	return sampleBytesUsed, limitBytes
}

// RecordInsightSample 写入一条复核样本。
// 该函数由日志落库阶段调用，失败只告警不影响主流程。
func RecordInsightSample(userId int, username, requestId, modelName, path string,
	result *insight.Result, keepBody bool, limitBytes int64) {
	if userId <= 0 || result == nil {
		return
	}
	evidence := result.BuildEvidence()
	body := ""
	bodySize := 0
	if raw := result.RawBody(); len(raw) > 0 {
		bodySize = len(raw)
		if keepBody {
			body = string(raw)
		}
	}
	if len(evidence) == 0 && body == "" {
		return
	}

	evidenceJSON := ""
	if len(evidence) > 0 {
		if bytes, err := common.Marshal(evidence); err == nil {
			evidenceJSON = string(bytes)
		}
	}

	priority := samplePriorityNormal
	switch {
	case result.JailbreakLevel != "" && result.JailbreakLevel != insight.JailbreakNone:
		priority = samplePriorityJailbreak
	case result.IsRelay:
		priority = samplePriorityRelay
	}

	now := common.GetTimestamp()
	fingerprint := insight.EvidenceFingerprint(result.Category, result.Client, evidence)

	// 同一用户的同指纹样本只保留一行：agent 客户端与固定角色卡会反复发送
	// 完全一样的注入提示词，逐条留存等于把同一份证据抄几百遍。
	// 这里累加计数并刷新时间，让看板显示"该模式命中 N 次"。
	if fingerprint != "" {
		var existing UserInsightSample
		err := dbx.DB.Select("id", "hit_count").
			Where("user_id = ? AND fingerprint = ?", userId, fingerprint).
			First(&existing).Error
		if err == nil {
			updates := map[string]interface{}{
				"hit_count":    existing.HitCount + 1,
				"last_seen_at": now,
			}
			if err := dbx.DB.Model(&UserInsightSample{}).
				Where("id = ?", existing.Id).
				Updates(updates).Error; err != nil {
				common.SysError("failed to bump insight sample hit count: " + err.Error())
			}
			return
		}
		if err != gorm.ErrRecordNotFound {
			common.SysError("failed to look up insight sample fingerprint: " + err.Error())
		}
	}

	row := UserInsightSample{
		UserId:         userId,
		Username:       username,
		CreatedAt:      now,
		RequestId:      requestId,
		ModelName:      modelName,
		Path:           path,
		Category:       result.Category,
		Client:         result.Client,
		ClientVersion:  result.ClientVersion,
		IsRelay:        result.IsRelay,
		RelayVendor:    result.RelayVendor,
		RiskLevel:      result.JailbreakLevel,
		JailbreakScore: result.JailbreakScore,
		GuessGender:    result.GuessGender,
		GenderBasis:    result.GenderBasis,
		EvidenceJSON:   evidenceJSON,
		Body:           body,
		BodySize:       bodySize,
		Fingerprint:    fingerprint,
		HitCount:       1,
		LastSeenAt:     now,
		Priority:       priority,
	}
	// 行开销 = 变长字段实际长度 + 定长字段与索引的粗略常量。
	row.ByteSize = len(row.EvidenceJSON) + len(row.Body) + len(row.Username) +
		len(row.RequestId) + len(row.ModelName) + len(row.Path) + 256

	if err := dbx.DB.Create(&row).Error; err != nil {
		common.SysError("failed to record insight sample: " + err.Error())
		return
	}

	sampleBytesLock.Lock()
	sampleBytesUsed += int64(row.ByteSize)
	over := sampleBytesReady && sampleBytesUsed > limitBytes
	sampleBytesLock.Unlock()

	if over {
		// 超限时立即回收，避免持续写入把库撑爆。
		if err := EnforceInsightSampleQuota(limitBytes); err != nil {
			common.SysError("failed to enforce insight sample quota: " + err.Error())
		}
	}
}

// sampleEvictBatch 是单次淘汰扫描的行数上限，避免一次删除锁表过久。
const sampleEvictBatch = 500

// EnforceInsightSampleQuota 把样本表压回配额以内。
// 淘汰顺序：先删普通采样中最旧的，其次中转站，最后才动破甲记录，
// 这样容量紧张时优先保住最有价值的证据。
func EnforceInsightSampleQuota(limitBytes int64) error {
	if limitBytes <= 0 {
		return nil
	}
	// 回收到配额的 90%，留出缓冲避免频繁触发。
	target := limitBytes * 9 / 10

	for round := 0; round < 20; round++ {
		sampleBytesLock.Lock()
		used := sampleBytesUsed
		sampleBytesLock.Unlock()
		if used <= target {
			return nil
		}

		var freed int64
		var deleted int64
		// 按优先级从低到高逐档淘汰。
		for _, priority := range []int{samplePriorityNormal, samplePriorityRelay, samplePriorityJailbreak} {
			var victims []UserInsightSample
			err := dbx.DB.Select("id", "byte_size").
				Where("priority = ?", priority).
				Order("created_at asc").
				Limit(sampleEvictBatch).
				Find(&victims).Error
			if err != nil {
				return err
			}
			if len(victims) == 0 {
				continue
			}
			ids := make([]int64, 0, len(victims))
			for _, victim := range victims {
				ids = append(ids, victim.Id)
				freed += int64(victim.ByteSize)
			}
			if err := dbx.DB.Where("id IN ?", ids).Delete(&UserInsightSample{}).Error; err != nil {
				return err
			}
			deleted += int64(len(ids))
			break
		}
		if deleted == 0 {
			// 表已空却仍超限，说明计数器漂移了，重新汇总校准。
			InitInsightSampleUsage()
			return nil
		}

		sampleBytesLock.Lock()
		sampleBytesUsed -= freed
		if sampleBytesUsed < 0 {
			sampleBytesUsed = 0
		}
		sampleBytesLock.Unlock()
	}
	return nil
}

// CleanInsightSamples 按保留天数清理过期样本，并顺带校准容量计数。
func CleanInsightSamples(retentionDays int, limitBytes int64) error {
	if retentionDays > 0 {
		cutoff := common.GetTimestamp() - int64(retentionDays)*86400
		if err := dbx.DB.Where("created_at < ?", cutoff).Delete(&UserInsightSample{}).Error; err != nil {
			return err
		}
		InitInsightSampleUsage()
	}
	return EnforceInsightSampleQuota(limitBytes)
}

// GetInsightSamples 分页查询样本。userId <= 0 表示不限用户。
func GetInsightSamples(userId int, riskLevel string, relayOnly bool, offset, limit int) (
	samples []*UserInsightSample, total int64, err error) {
	query := dbx.DB.Model(&UserInsightSample{})
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if riskLevel != "" {
		query = query.Where("risk_level = ?", riskLevel)
	}
	if relayOnly {
		query = query.Where("is_relay = ?", true)
	}
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 列表不返回 body，避免一次查询拖出几十 MB。
	// 按 last_seen_at 倒序：去重后一行代表一种命中模式，
	// 排序依据应是"这种模式最近一次出现"而不是首次记录时间。
	err = query.Omit("body").
		Order("last_seen_at desc, created_at desc").
		Offset(offset).Limit(limit).
		Find(&samples).Error
	return samples, total, err
}

// GetInsightSampleUserGroups 返回按用户分组的样本概览。
// 看板默认按用户展示："一个用户一行，展开才看具体证据"，
// 避免上千条逐请求记录把界面刷满。
type InsightSampleUserGroup struct {
	UserId     int    `json:"user_id"`
	Username   string `json:"username"`
	Samples    int    `json:"samples"`
	HitTotal   int    `json:"hit_total"`
	MaxRisk    string `json:"max_risk"`
	MaxScore   int    `json:"max_score"`
	LastSeenAt int64  `json:"last_seen_at"`
	ByteSize   int    `json:"byte_size"`
}

// GetInsightSampleUserGroups 汇总每个用户的样本情况。
// 风险等级取该用户样本中的最高档，按最近活跃排序。
func GetInsightSampleUserGroups(riskLevel string, relayOnly bool, offset, limit int) (
	groups []*InsightSampleUserGroup, total int64, err error) {
	base := dbx.DB.Model(&UserInsightSample{})
	if riskLevel != "" {
		base = base.Where("risk_level = ?", riskLevel)
	}
	if relayOnly {
		base = base.Where("is_relay = ?", true)
	}

	// 用子查询统计用户数：三种数据库对 COUNT(DISTINCT) 的分页支持不一致，
	// 这里分两步走以保持兼容。
	var userIds []int
	if err = base.Session(&gorm.Session{}).
		Distinct("user_id").Pluck("user_id", &userIds).Error; err != nil {
		return nil, 0, err
	}
	total = int64(len(userIds))

	err = base.Session(&gorm.Session{}).
		Select("user_id, MAX(username) AS username, COUNT(*) AS samples, " +
			"SUM(hit_count) AS hit_total, MAX(jailbreak_score) AS max_score, " +
			"MAX(last_seen_at) AS last_seen_at, SUM(byte_size) AS byte_size").
		Group("user_id").
		// 显式写聚合函数而不是依赖别名：PostgreSQL 会把裸列名解析为输出列，
		// 但 MySQL/SQLite 的行为不完全一致，写全更保险。
		Order("MAX(last_seen_at) desc").
		Offset(offset).Limit(limit).
		Find(&groups).Error
	if err != nil {
		return nil, 0, err
	}

	// 最高风险等级用单独一次查询补齐：SQL 里对字符串枚举排序不可靠
	// （none/suspect/likely/confirmed 的字典序与严重程度不一致）。
	for _, group := range groups {
		group.MaxRisk = insight.JailbreakNone
		var levels []string
		if err := base.Session(&gorm.Session{}).
			Where("user_id = ?", group.UserId).
			Distinct("risk_level").Pluck("risk_level", &levels).Error; err != nil {
			continue
		}
		for _, level := range levels {
			if riskSeverity(level) > riskSeverity(group.MaxRisk) {
				group.MaxRisk = level
			}
		}
	}
	return groups, total, nil
}

// riskSeverity 把风险等级映射为可比较的序数。
func riskSeverity(level string) int {
	switch level {
	case insight.JailbreakConfirmed:
		return 3
	case insight.JailbreakLikely:
		return 2
	case insight.JailbreakSuspect:
		return 1
	default:
		return 0
	}
}

// GetInsightSample 返回单条样本全文（含 body）。
func GetInsightSample(id int64) (*UserInsightSample, error) {
	var sample UserInsightSample
	err := dbx.DB.Where("id = ?", id).First(&sample).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sample, nil
}

// DecodeEvidence 解析证据 JSON，失败时返回 nil 而不是报错。
func (s *UserInsightSample) DecodeEvidence() []insight.Evidence {
	if s == nil || s.EvidenceJSON == "" {
		return nil
	}
	var items []insight.Evidence
	if err := common.UnmarshalJsonStr(s.EvidenceJSON, &items); err != nil {
		return nil
	}
	return items
}

// DeleteInsightSamplesByUser 删除某用户的全部样本，供管理员清理误采数据。
func DeleteInsightSamplesByUser(userId int) (int64, error) {
	if userId <= 0 {
		return 0, nil
	}
	var freed int64
	var rows []UserInsightSample
	if err := dbx.DB.Select("id", "byte_size").Where("user_id = ?", userId).Find(&rows).Error; err != nil {
		return 0, err
	}
	for _, row := range rows {
		freed += int64(row.ByteSize)
	}
	result := dbx.DB.Where("user_id = ?", userId).Delete(&UserInsightSample{})
	if result.Error != nil {
		return 0, result.Error
	}
	sampleBytesLock.Lock()
	sampleBytesUsed -= freed
	if sampleBytesUsed < 0 {
		sampleBytesUsed = 0
	}
	sampleBytesLock.Unlock()
	return result.RowsAffected, nil
}

// MaintainInsightSamples 周期性执行保留期清理与配额回收。
// 每轮都重新读取配置，便于运营方在线调整上限而无需重启。
func MaintainInsightSamples() {
	for {
		time.Sleep(10 * time.Minute)
		setting := GetUserInsightSetting()
		if err := CleanInsightSamples(setting.SampleRetentionDays, setting.SampleQuotaBytes()); err != nil {
			common.SysError("insight sample maintenance failed: " + err.Error())
		}
	}
}
