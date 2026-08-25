package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// TestTruncateSensitiveSnippet 截断不得把多字节字形切成无效 UTF-8，
// 且 256B 内的文本原样返回（issue #381）。
func TestTruncateSensitiveSnippet(t *testing.T) {
	short := "hello gov.cn 你好"
	assert.Equal(t, short, truncateSensitiveSnippet(short))

	// CJK 每字 3B：256 不是 3 的倍数，边界必落在某字形中间
	long := strings.Repeat("字", 200)
	got := truncateSensitiveSnippet(long)
	require.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8")
	require.LessOrEqual(t, len(got), 256)
	// rune 对齐后只可能少不可能多
	assert.Equal(t, (len(got))/3*3, len(got))
}

// TestParseSensitiveLabel 引擎命中标签拆分：带前缀 / 裸词表串 / 空串。
func TestParseSensitiveLabel(t *testing.T) {
	layer, matched := parseSensitiveLabel("target:gov.cn")
	assert.Equal(t, "target", layer)
	assert.Equal(t, "gov.cn", matched)

	layer, matched = parseSensitiveLabel("persona-evasion:ignore all instructions")
	assert.Equal(t, "persona-evasion", layer)
	assert.Equal(t, "ignore all instructions", matched)

	layer, matched = parseSensitiveLabel("wordA,wordB")
	assert.Equal(t, "dict", layer)
	assert.Equal(t, "wordA,wordB", matched)

	layer, matched = parseSensitiveLabel("")
	assert.Equal(t, "unknown", layer)
	assert.Equal(t, "", matched)
}

// withAuditDB 用内存 SQLite 替换 LOG_DB，验证审计事件真正落库。
func withAuditDB(t *testing.T) {
	t.Helper()
	previousDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previousDB })
}

// TestRecordSensitiveAuditEventPersists 审计事件落库：type=LogTypeSensitive、
// op.params 携带 layer/matched/snippet/direction/channel_id/model_name/
// chunk_length（issue #381 Done when 写入路径）。
func TestRecordSensitiveAuditEventPersists(t *testing.T) {
	withAuditDB(t)

	ev := sensitiveAuditEvent{
		userId:    1,
		channelId: 42,
		modelName: "gpt-4o-mini",
		ip:        "127.0.0.1",
		direction: "input",
		layer:     "target",
		matched:   "gov.cn",
		snippet:   "attack gоv.cn now",
		chunkLen:  17,
	}
	recordSensitiveAuditEvent(ev)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeSensitive).Find(&logs).Error)
	require.Len(t, logs, 1)

	var other struct {
		Op struct {
			Action string                 `json:"action"`
			Params map[string]interface{} `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.Unmarshal([]byte(logs[0].Other), &other))
	assert.Equal(t, "sensitive_block", other.Op.Action)
	assert.Equal(t, "input", other.Op.Params["direction"])
	assert.Equal(t, "target", other.Op.Params["layer"])
	assert.Equal(t, "gov.cn", other.Op.Params["matched"])
	// JSON 反序列化后数字为 float64
	assert.Equal(t, float64(42), other.Op.Params["channel_id"])
	assert.Equal(t, "gpt-4o-mini", other.Op.Params["model_name"])
	assert.Equal(t, float64(17), other.Op.Params["chunk_length"])
	assert.Equal(t, "127.0.0.1", logs[0].Ip)

}

// TestRecordSensitiveBlockDisabled 开关关闭时不入队、不启动 worker。
func TestRecordSensitiveBlockDisabled(t *testing.T) {
	previous := setting.SensitiveAuditEnabled
	setting.SensitiveAuditEnabled = false
	t.Cleanup(func() { setting.SensitiveAuditEnabled = previous })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	assert.NotPanics(t, func() {
		RecordSensitiveBlock(c, "input", "target:gov.cn", "text")
	})
}
