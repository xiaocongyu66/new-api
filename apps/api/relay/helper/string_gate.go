package helper

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// 输出侧敏感内容静默过滤：命中不再断流，而是把目标域名/词库敏感词从 chunk
// 文本中切除后照常下发，客户端永远看不到系统配置的敏感内容。
//
// 流式边界处理：命中单元可能跨 chunk。buf 保存全部已接收未发出的文本，
// safe 标记「上一轮已有内容」与「本轮新增」的分界。每步对 buf+data 全量
// 扫描，但只发出 safe 之前的净化文本——跨分界的命中其切除区间必然压住
// 分界点，发射端被钳到切除起点，整段留到下一轮再发。因此任何长度有限的
// 命中单元最晚在其后第二个 chunk 到达时被完整切除，且 SSE 帧不会被切断
// （切除只发生在帧内容内部，帧行数不变）。
//
// ponytail: 引入一个 chunk 的额外出字延迟；JSON \uXXXX 转义形式的内容不在
// 折叠范围（上游几乎都发原始 UTF-8）。不经 Done()/StreamScannerHandler
// 收尾的适配器（少数渠道自渲染 [DONE]）尾部缓冲不再补发。

type sensitiveOutputState struct {
	buf  string // 已接收未发出的文本
	safe int    // buf 内「上轮已有内容」结束偏移（本轮不发出越过它的区间）
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

// mapOffset 把原文偏移映射为净化后坐标；offset 落在切除区间内时钳到该区间起点。
func mapOffset(cuts [][2]int, offset int) int {
	sort.Slice(cuts, func(i, j int) bool { return cuts[i][0] < cuts[j][0] })
	delta := 0
	for _, c := range cuts {
		switch {
		case c[1] <= offset:
			delta += c[1] - c[0]
		case c[0] < offset:
			return c[0] - delta // 钳到切除起点，整段延迟
		default:
			return offset - delta
		}
	}
	return offset - delta
}

// outputChunkFiltered 过滤一个输出 chunk，返回可立即下发的净化文本（可能为空）。
// 目标域过滤无条件生效；词库过滤受 CheckSensitiveOnCompletionEnabled 控制。
func outputChunkFiltered(c *gin.Context, data string) string {
	st := outputFilterState(c)
	safe := len(st.buf)
	scan := st.buf + data
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
	emitEnd := mapOffset(cuts, safe)
	out := cleaned[:emitEnd]
	st.buf = cleaned[emitEnd:]
	st.safe = len(st.buf)
	return out
}

// FlushOutputPending 流结束时调用：把剩余缓冲净化后全部放出。
func FlushOutputPending(c *gin.Context) string {
	st := outputFilterState(c)
	if st.buf == "" {
		return ""
	}
	buf := st.buf
	st.buf = ""
	st.safe = 0
	var cleaned string
	var labels []string
	if setting.ShouldCheckCompletionSensitive() {
		cleaned, labels = service.SanitizeSensitiveText(buf)
		if len(labels) > 0 {
			common.SysLog("output sanitized by sensitive filter: [" + strings.Join(labels, ",") + "]")
			service.RecordSensitiveBlock(c, "output", "sanitize:"+strings.Join(labels, ","), buf)
		}
		return cleaned
	}
	cleaned, _ = service.SanitizeTargetDomains(buf)
	return cleaned
}
