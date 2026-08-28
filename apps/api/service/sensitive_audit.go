package service

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/internal/sensitive"
)

// 敏感拦截审计：拦截事件异步写入统一 logs 表（type=LogTypeSensitive），
// 供敏感词设置页"最近拦截"表格与审计导出消费。
// 硬性要求：绝不阻塞请求路径——单 worker + 有界队列，队列满即丢弃并计数
// （审计缺失好过响应变慢）；丢弃计数打点便于发现容量不足。

const (
	sensitiveAuditQueueSize  = 1024
	sensitiveSnippetMaxBytes = 256
)

// sensitiveAuditEvent 拦截点采集的最小上下文；username 由 worker 落库时查询，
// 避免 DB 查询进入请求路径。
type sensitiveAuditEvent struct {
	userId    int
	channelId int    // 无渠道路径（如提前拒绝）为 0；重试时归属实际服务渠道
	modelName string // 请求原始模型名（重定向/映射前）
	ip        string
	direction string // input | output
	layer     string // target | target-action | target-combo | persona-evasion | breakout | dict
	matched   string
	snippet   string // 截断到 256B 的原文片段，rune 边界安全
	chunkLen  int    // 被检查文本全长（字节），snippet 仅为截断片段
}

var (
	sensitiveAuditOnce sync.Once
	sensitiveAuditCh   chan sensitiveAuditEvent
	sensitiveAuditDrop atomic.Int64
)

func startSensitiveAuditWorker() {
	sensitiveAuditOnce.Do(func() {
		sensitiveAuditCh = make(chan sensitiveAuditEvent, sensitiveAuditQueueSize)
		go func() {
			for ev := range sensitiveAuditCh {
				recordSensitiveAuditEvent(ev)
			}
		}()
	})
}

func recordSensitiveAuditEvent(ev sensitiveAuditEvent) {
	content := fmt.Sprintf("sensitive %s blocked: layer=%s matched=%s", ev.direction, ev.layer, ev.matched)
	params := map[string]interface{}{
		"direction":    ev.direction,
		"layer":        ev.layer,
		"matched":      ev.matched,
		"snippet":      ev.snippet,
		"channel_id":   ev.channelId,
		"model_name":   ev.modelName,
		"chunk_length": ev.chunkLen,
	}
	model.RecordSensitiveAuditLog(ev.userId, content, ev.ip, params)
}

// RecordSensitiveBlock 在拦截点异步记录一条敏感审计事件。
// label 为引擎返回的命中标签："target:gov.cn"、"breakout:xxx"、
// "persona-evasion:xxx" 等，或词库层的裸逗号串（无前缀）。
// text 为被检查文本，截断后作为 snippet 入库。开关关闭时直接返回。
func RecordSensitiveBlock(c contract.Context, direction string, label string, text string) {
	if !sensitive.SensitiveAuditEnabled {
		return
	}
	startSensitiveAuditWorker()
	layer, matched := parseSensitiveLabel(label)
	ev := sensitiveAuditEvent{
		userId:    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		channelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		modelName: common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
		ip:        c.ClientIP(),
		direction: direction,
		layer:     layer,
		matched:   matched,
		snippet:   truncateSensitiveSnippet(text),
		chunkLen:  len(text),
	}
	select {
	case sensitiveAuditCh <- ev:
	default:
		n := sensitiveAuditDrop.Add(1)
		if n == 1 || n%1000 == 0 {
			common.SysError(fmt.Sprintf("sensitive audit queue full, dropped total=%d", n))
		}
	}
}

// parseSensitiveLabel 拆分引擎命中标签为层级与命中内容；
// 无冒号前缀的裸串视为词库层（dict）多命中。
func parseSensitiveLabel(label string) (layer, matched string) {
	if label == "" {
		return "unknown", ""
	}
	if i := strings.Index(label, ":"); i > 0 {
		return label[:i], label[i+1:]
	}
	return "dict", label
}

// truncateSensitiveSnippet 按 rune 边界截断到 sensitiveSnippetMaxBytes，
// 避免把多字节字形切成无效 UTF-8（与 isDefenseContext 同一边界原则）。
func truncateSensitiveSnippet(text string) string {
	if len(text) <= sensitiveSnippetMaxBytes {
		return text
	}
	cut := sensitiveSnippetMaxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
