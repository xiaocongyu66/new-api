package helper

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// 输出侧敏感内容静默过滤：命中不再断流，而是把目标域名/词库敏感词从 chunk
// 文本中切除后照常下发，客户端永远看不到完整的系统配置敏感内容。
//
// 跨 chunk 处理：命中单元可能被 SSE 分片切开。每个 chunk 扫描时前置上一轮
// 尾部上下文（outputHoldbackLen() 字节）用于识别跨块命中；发射时剥掉上下文
// 段、只发本轮新增部分的净化结果。零额外延迟、SSE 帧永不切断。
//
// ponytail: 极端情况下跨块命中的前半段（≤ holdback 字节）随上一 chunk 已
// 发出——客户端看到的碎片拼不成完整命中词。需要严格无碎片时改用整帧延迟
// 方案（代价是每流 +1 chunk 延迟并破坏部分可见性契约，见 aws 取消测试）。
// JSON \uXXXX 转义形式的内容不在折叠范围（上游几乎都发原始 UTF-8）。

type sensitiveOutputState struct {
	ctxTail string // 上一轮输出文本的尾部上下文（只读参与扫描）
}

// outputFilterState 获取/初始化请求级输出过滤状态。
func outputFilterState(c *gin.Context) *sensitiveOutputState {
	if v, ok := c.Get(string(constant.ContextKeySensitiveOutputState)); ok {
		if s, ok := v.(*sensitiveOutputState); ok {
			return s
		}
	}
	s := &sensitiveOutputState{}
	c.Set(string(constant.ContextKeySensitiveOutputState), s)
	return s
}

// outputHoldbackLen 最长可命中单元的字节长度（词库词 + 目标域名 + 子域余量）。
// 下限 16 兜底动态配置，上限 256 防极端词条拖垮扫描成本。
var outputHoldbackLen = sync.OnceValue(func() int {
	h := 16
	for _, w := range setting.SensitiveWords {
		if l := len(w); l > h {
			h = l
		}
	}
	for _, d := range service.TargetDomains() {
		if l := len(d) + 8; l > h { // www. 子域前缀余量
			h = l
		}
	}

	if h > 256 {
		h = 256
	}
	return h
})

// mapOffset 把原文偏移映射为净化后坐标；offset 落在切除区间内时钳到该区间起点。
func mapOffset(cuts [][2]int, offset int) int {
	delta := 0
	for _, c := range cuts {
		switch {
		case c[1] <= offset:
			delta += c[1] - c[0]
		case c[0] < offset:
			return c[0] - delta // 钳到切除起点，命中段整体不发
		default:
			return offset - delta
		}
	}
	return offset - delta
}

func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// outputChunkFiltered 过滤一个输出 chunk，返回可立即下发的净化文本。
// 目标域过滤无条件生效；词库过滤受 CheckSensitiveOnCompletionEnabled 控制。
func outputChunkFiltered(c *gin.Context, data string) string {
	st := outputFilterState(c)
	scan := st.ctxTail + data
	var cleaned string
	var labels []string
	var cuts [][2]int
	if setting.ShouldCheckCompletionSensitive() {
		cleaned, labels, cuts = service.SanitizeSensitiveTextRanges(scan)
	} else {
		cleaned, labels, cuts = service.SanitizeTargetDomainsRanges(scan)
	}
	if len(labels) > 0 {
		common.SysLog("output sanitized by sensitive filter: [" + strings.Join(labels, ",") + "]")
		service.RecordSensitiveBlock(c, "output", "sanitize:"+strings.Join(labels, ","), scan)
	}
	// 剥掉上下文段：它属于上一轮已发出的文本，只发本轮新增的净化结果。
	ctxLen := mapOffset(cuts, len(st.ctxTail))
	out := cleaned[ctxLen:]
	st.ctxTail = tailBytes(cleaned, outputHoldbackLen())
	return out
}
