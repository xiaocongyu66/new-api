package gateway

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

const OutputFilterWindowSize = 512

// SensitiveOutputState 是请求级输出闸状态（挂在 context）。
type SensitiveOutputState struct {
	Window    string // 最近窗口内容（用于跨 chunk 检测）
	Blocked   bool   // 已触发终止，后续所有 chunk 直接丢弃
	Terminate bool   // 终止帧已写，防止重复写 content_filter+[DONE]
}

// OutputFilterState 获取/初始化请求级输出检测状态。
func OutputFilterState(c contract.Context) *SensitiveOutputState {
	if v, ok := c.Get(string(constant.ContextKeySensitiveOutputState)); ok {
		if s, ok := v.(*SensitiveOutputState); ok {
			return s
		}
	}
	s := &SensitiveOutputState{}
	c.Set(string(constant.ContextKeySensitiveOutputState), s)
	return s
}

// OutputChunkBlocked 检查一个输出 chunk 是否应被拦截。
// 已触发的流直接丢弃（blocked=true）；新 chunk 累积进窗口后再判。
// 目标域名硬闸无条件生效（不受敏感词开关控制），其余检测受
// CheckSensitiveOnCompletionEnabled 控制（默认开）。
func OutputChunkBlocked(c contract.Context, data string) (bool, string) {
	st := OutputFilterState(c)
	if st.Blocked {
		return true, "already-blocked"
	}
	if data == "" {
		return false, ""
	}
	// 目标域无条件终止：任何输出包含攻击目标站点即断流（用户要求双向终止）。
	if d := service.CheckSensitiveTargets(data); d != "" {
		st.Blocked = true
		common.SysLog(fmt.Sprintf("output blocked by target domain: [%s]", d))
		return true, "target:" + d
	}
	if !setting.ShouldCheckCompletionSensitive() {
		return false, ""
	}
	st.Window += data
	if len(st.Window) > OutputFilterWindowSize {
		st.Window = st.Window[len(st.Window)-OutputFilterWindowSize:]
	}
	if hit, labels := service.CheckSensitiveText(st.Window); hit && len(labels) > 0 {
		st.Blocked = true
		common.SysLog(fmt.Sprintf("output blocked by sensitive filter: [%s]", labels[0]))
		return true, labels[0]
	}
	return false, ""
}

// TerminateOutputSSE 输出命中敏感后向客户端写终止帧并标记截断。
// 写 OpenAI 风格 content_filter 终止事件 + [DONE]，任何格式客户端都会断流。
// 幂等：已写入过的流不再重复写（后续 chunk 路过时 blocked 直接丢弃）。
func TerminateOutputSSE(c contract.Context) {
	st := OutputFilterState(c)
	if st.Terminate {
		return
	}
	st.Terminate = true // 先标记，再写入；partial-write 重入也不会重复写
	_, _ = c.ResponseWriter().Write([]byte("data: " + `{"choices":[{"delta":{},"finish_reason":"content_filter"}]}` + "\n\n"))
	_, _ = c.ResponseWriter().Write([]byte("data: [DONE]\n\n"))
	_ = FlushWriter(c)
}
