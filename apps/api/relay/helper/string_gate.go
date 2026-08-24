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
// 文本中切除后照常下发，客户端永远看不到系统配置的敏感内容。
//
// 流式边界处理：buf 保存全部已扫描未发出的文本；每步全量扫描后只发出
// 末尾 outputHoldbackLen() 字节之前的部分。任何长度 ≤ holdback 的命中单元
// 在其末字节被发出前必然完整落入某次扫描窗口（否则它跨越发射边界，长度
// 必须 > holdback），故不漏放。holdback 取词库词与目标域名的最大字节长度。
//
// ponytail: 引入至多 holdback 字节的额外出字延迟；JSON \uXXXX 转义形式的
// 内容不在折叠范围（上游几乎都发原始 UTF-8）。少数渠道适配器不经 Done()
// 自行渲染 [DONE]，其尾部缓冲不再补发（截断至多 holdback 字节）。

type sensitiveOutputState struct {
	buf string // 已接收未发出的文本（尾部 holdback 字节待后续确认）
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
// 下限 16 兜底动态配置，上限 256 防极端词条拖垮内存与延迟。
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

// outputChunkFiltered 过滤一个输出 chunk，返回可立即下发的净化文本（可能为空）。
// 目标域过滤无条件生效；词库过滤受 CheckSensitiveOnCompletionEnabled 控制。
func outputChunkFiltered(c *gin.Context, data string) string {
	st := outputFilterState(c)
	st.buf += data
	var cleaned string
	var labels []string
	if setting.ShouldCheckCompletionSensitive() {
		cleaned, labels = service.SanitizeSensitiveText(st.buf)
	} else {
		cleaned, labels = service.SanitizeTargetDomains(st.buf)
	}
	if len(labels) > 0 {
		common.SysLog("output sanitized by sensitive filter: [" + strings.Join(labels, ",") + "]")
		service.RecordSensitiveBlock(c, "output", "sanitize:"+strings.Join(labels, ","), st.buf)
	}
	keep := outputHoldbackLen()
	if len(cleaned) <= keep {
		st.buf = cleaned
		return ""
	}
	out := cleaned[:len(cleaned)-keep]
	st.buf = cleaned[len(cleaned)-keep:]
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
