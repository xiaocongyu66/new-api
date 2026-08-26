package setting

import "strings"

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

// CheckSensitiveOnCompletionEnabled 输出侧敏感检测（用户要求默认 block，
// 命中即终止响应流并注入 content_filter，不向客户端泄露任何后续内容）。
var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// 默认空：内容词库不参与生产拦截（用户拍板：只管破甲 + 攻击 gov 两类，
// 由 breakoutTerms/targetActionTerms/指纹模板层承担，已被 #380 移除。
var SensitiveWords = []string{}

// SensitiveBlockGroups 敏感检测启用组。可选值（逗号分隔）：
//
//	gov  — 词库黑名单层（L1/L2/L3 词库 AC 命中），默认开启
//	tech — 技术破甲指纹与模板（指令覆盖/双答/无限制/模型接管等）
//	rp   — 角色扮演特征（act as/pretend/沉浸/角色卡模板），酒馆场景可关
//
// 默认 gov,tech：拦截政府敏感词 + 技术破甲，放行角色扮演内容。
var SensitiveBlockGroups = []string{"gov", "tech"}

func SensitiveGroupsToString() string {
	return strings.Join(SensitiveBlockGroups, ",")
}

// knownSensitiveGroups 是 SensitiveBlockGroups 的合法取值。未知值会被
// SensitiveGroupsFromString 丢弃（含拼写错误/历史遗留），避免静默禁用全部组。
var knownSensitiveGroups = map[string]struct{}{
	"gov":  {},
	"tech": {},
	"rp":   {},
}

func SensitiveGroupsFromString(s string) {
	groups := []string{}
	for _, g := range strings.Split(s, ",") {
		g = strings.TrimSpace(g)
		if _, ok := knownSensitiveGroups[g]; ok {
			groups = append(groups, g)
		}
	}
	if len(groups) > 0 {
		SensitiveBlockGroups = groups
	}
}

// SensitiveGroupEnabled 报告某个组别是否在启用集合中。
func SensitiveGroupEnabled(g string) bool {
	for _, x := range SensitiveBlockGroups {
		if x == g {
			return true
		}
	}
	return false
}

func SensitiveWordsToString() string {
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	SensitiveWords = []string{}
	sw := strings.Split(s, "\n")
	for _, w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			SensitiveWords = append(SensitiveWords, w)
		}
	}
	// 生产入口过滤易误伤短词（2 字泛词剔除、攻击词白名单保留）；
	// testdata 基线 fixture 不经过此处，回归锚不受影响。
	SensitiveWords = FilterSensitiveWords(SensitiveWords)
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

// ShouldCheckCompletionSensitive 输出侧检测总开关（用户要求默认开启、命中即 block）。
func ShouldCheckCompletionSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}

// SensitiveAuditEnabled 敏感拦截审计开关：拦截事件异步写入统一 logs 表
// （type=LogTypeSensitive），供敏感词设置页"最近拦截"表格查询。默认开启。
var SensitiveAuditEnabled = true

// SensitiveAuditRetentionDays 敏感审计日志保留天数，超期由后台清理任务
// 分批删除（仅命中 LogTypeSensitive 行）。<=0 表示永久保留。默认 30 天。
var SensitiveAuditRetentionDays = 30
