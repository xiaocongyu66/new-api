package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const outputFilterWindowSize = 512

// sensitiveOutputState 是请求级输出闸状态（挂在 gin context）。
type sensitiveOutputState struct {
	window  string // 最近窗口内容（用于跨 chunk 检测）
	blocked bool   // 已触发终止，后续所有 chunk 直接丢弃
}

// outputFilterState 获取/初始化请求级输出检测状态。
func outputFilterState(c *gin.Context) *sensitiveOutputState {
	if v, ok := c.Get(constant.ContextKeySensitiveOutputState); ok {
		if s, ok := v.(*sensitiveOutputState); ok {
			return s
		}
	}
	s := &sensitiveOutputState{}
	c.Set(constant.ContextKeySensitiveOutputState, s)
	return s
}

// outputChunkBlocked 检查一个输出 chunk 是否应被拦截。
// 已触发的流直接丢弃（blocked=true）；新 chunk 累积进窗口后再判。
func outputChunkBlocked(c *gin.Context, data string) (bool, string) {
	if !setting.ShouldCheckCompletionSensitive() {
		return false, ""
	}
	st := outputFilterState(c)
	if st.blocked {
		return true, "already-blocked"
	}
	if data == "" {
		return false, ""
	}
	st.window += data
	if len(st.window) > outputFilterWindowSize {
		st.window = st.window[len(st.window)-outputFilterWindowSize:]
	}
	if hit, label := service.CheckSensitiveOutput(st.window); hit {
		st.blocked = true
		common.SysLog(fmt.Sprintf("output blocked by sensitive filter: [%s]", label))
		return true, label
	}
	return false, ""
}

// terminateOutputSSE 输出命中敏感后向客户端写终止帧并标记截断。
// 写 OpenAI 风格 content_filter 终止事件 + [DONE]，任何格式客户端都会断流。
func terminateOutputSSE(c *gin.Context) {
	_ = c.Writer.WriteString("data: " + `{"choices":[{"delta":{},"finish_reason":"content_filter"}]}` + "\n\n")
	_ = c.Writer.WriteString("data: [DONE]\n\n")
	_ = FlushWriter(c)
}
