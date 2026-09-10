package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseSendMessageID 消息 ID 解析:message_id 优先,id 兜底,坏响应返回空。
func TestParseSendMessageID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"仅 message_id", `{"message_id":"MSG_1","id":"ID_1"}`, "MSG_1"},
		{"仅 id", `{"id":"ID_2"}`, "ID_2"},
		{"两者都有取 message_id", `{"message_id":"MSG_3","id":"ID_3"}`, "MSG_3"},
		{"空对象", `{}`, ""},
		{"空 body", ``, ""},
		{"坏 JSON", `not-json`, ""},
		{"无 id 字段", `{"code":0}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseSendMessageID([]byte(tc.body)))
		})
	}
}
