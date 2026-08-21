package setting

import "strings"

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// var SensitiveWords []string
var SensitiveWords = []string{
	"test_sensitive",
}

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

func SensitiveGroupsFromString(s string) {
	groups := []string{}
	for _, g := range strings.Split(s, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
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
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
