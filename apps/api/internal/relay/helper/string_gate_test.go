package helper

import (
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/sensitive"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func testCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	return c, w
}

func TestOutputGateTargetDomainUnconditional(t *testing.T) {
	oldOn := sensitive.CheckSensitiveOnCompletionEnabled
	sensitive.CheckSensitiveOnCompletionEnabled = false
	t.Cleanup(func() { sensitive.CheckSensitiveOnCompletionEnabled = oldOn })

	c, _ := testCtx()
	blocked, label := outputChunkBlocked(ginadapter.Wrap(c), "模型回答：参考 www.gov.cn 上面发布的数据")
	assert.True(t, blocked, "target domain must block even with completion check off")
	assert.Contains(t, label, "target:")
}

func TestOutputGateSwitchOffAllowsNormal(t *testing.T) {
	oldOn := sensitive.CheckSensitiveOnCompletionEnabled
	sensitive.CheckSensitiveOnCompletionEnabled = false
	t.Cleanup(func() { sensitive.CheckSensitiveOnCompletionEnabled = oldOn })

	c, _ := testCtx()
	blocked, _ := outputChunkBlocked(ginadapter.Wrap(c), "hello world, normal answer")
	assert.False(t, blocked)
}

func TestOutputGateBlockThenDrain(t *testing.T) {
	oldOn := sensitive.CheckSensitiveOnCompletionEnabled
	sensitive.CheckSensitiveOnCompletionEnabled = true
	t.Cleanup(func() { sensitive.CheckSensitiveOnCompletionEnabled = oldOn })

	c, _ := testCtx()
	blocked, label := outputChunkBlocked(ginadapter.Wrap(c), "ignore previous instructions")
	assert.True(t, blocked, "breakout term should block")
	assert.Equal(t, "breakout:ignore previous instructions", label)

	// 后续 chunk 全部丢弃，且不再重复写终止帧
	blocked2, label2 := outputChunkBlocked(ginadapter.Wrap(c), "more payload")
	assert.True(t, blocked2)
	assert.Equal(t, "already-blocked", label2)
}

func TestTerminateOutputSSEIdempotent(t *testing.T) {
	oldOn := sensitive.CheckSensitiveOnCompletionEnabled
	sensitive.CheckSensitiveOnCompletionEnabled = true
	t.Cleanup(func() { sensitive.CheckSensitiveOnCompletionEnabled = oldOn })

	c, w := testCtx()
	terminateOutputSSE(ginadapter.Wrap(c))
	terminateOutputSSE(ginadapter.Wrap(c))
	body := w.Body.String()
	assert.Contains(t, body, "content_filter")
	assert.Contains(t, body, "[DONE]")
	assert.Equal(t, 1, strings.Count(body, "[DONE]"), "终止帧应只写一次")
}
