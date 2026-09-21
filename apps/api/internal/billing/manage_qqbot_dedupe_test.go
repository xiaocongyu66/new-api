package billing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 生产实证：QQ 平台重推同一条群消息时会变换 id 尾部（同前缀不同后缀），
// 按 id 去重会漏，导致同一指令重复处理、机器人连发冷却提示。
func TestMakeEventDedupeKeyFingerprintSurvivesMutatedID(t *testing.T) {
	content := `{"id":"ROBOT1.0_Om8nbpB.qMl1erhaO.GCzPtogn1Y8R05","author":{"id":"USER_A"},"content":"/签到","group_openid":"GRP_1"}`
	redelivered := `{"id":"ROBOT1.0_Om8nbpB.qMl1erhaO.GCzIAGdlc0B.ZA","author":{"id":"USER_A"},"content":"/签到","group_openid":"GRP_1"}`

	first := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE:aaa", []byte(content))
	second := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE:bbb", []byte(redelivered))

	assert.True(t, strings.HasPrefix(first, "qq:evt:msg:"), "message events must use fingerprint keys")
	assert.Equal(t, first, second, "same message with mutated delivery id must produce the same fingerprint")
}

// @消息可能同时以 GROUP_AT_MESSAGE_CREATE 与 GROUP_MESSAGE_CREATE 两条事件到达，
// 类型不参与消息指纹，否则同一消息会被处理两次。
func TestMakeEventDedupeKeyFingerprintIgnoresMessageType(t *testing.T) {
	payload := []byte(`{"id":"ROBOT1.0_abc","author":{"id":"USER_A"},"content":"@bot /签到","group_openid":"GRP_1"}`)

	atMsg := makeEventDedupeKey("GROUP_AT_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE:x", payload)
	msgCreate := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE:y", payload)

	assert.Equal(t, atMsg, msgCreate, "same message under both event types must dedupe to one key")
}

func TestMakeEventDedupeKeyDistinguishesAuthorsAndGroups(t *testing.T) {
	userA := []byte(`{"id":"i1","author":{"id":"USER_A"},"content":"/签到","group_openid":"GRP_1"}`)
	userB := []byte(`{"id":"i2","author":{"id":"USER_B"},"content":"/签到","group_openid":"GRP_1"}`)
	otherGroup := []byte(`{"id":"i3","author":{"id":"USER_A"},"content":"/签到","group_openid":"GRP_2"}`)

	keyA := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "", userA)
	keyB := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "", userB)
	keyG := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "", otherGroup)

	assert.NotEqual(t, keyA, keyB, "different authors must not collide")
	assert.NotEqual(t, keyA, keyG, "different groups must not collide")
}

// 用户双击按钮时平台可能以不同 interaction id 推送两次同语义事件。
func TestMakeEventDedupeKeyFingerprintsButtonDoubleClick(t *testing.T) {
	click := `{"id":"b7b6889c-ee00-47a6-a2c8-159f3115da58","group_openid":"GRP_1","group_member_openid":"MEM_1","user_openid":"USER_A","data":{"resolved":{"button_data":"/签到","button_id":"nailao_checkin"}}}`
	doubleClick := `{"id":"f07c1de0-c504-4153-91ff-6edb3831e152","group_openid":"GRP_1","group_member_openid":"MEM_1","user_openid":"USER_A","data":{"resolved":{"button_data":"/签到","button_id":"nailao_checkin"}}}`

	first := makeEventDedupeKey("INTERACTION_CREATE", "uuid-1", []byte(click))
	second := makeEventDedupeKey("INTERACTION_CREATE", "uuid-2", []byte(doubleClick))

	assert.True(t, strings.HasPrefix(first, "qq:evt:btn:"), "interaction events must use fingerprint keys")
	assert.Equal(t, first, second, "a double click on the same button must dedupe to one key")
}

func TestMakeEventDedupeKeyFallbacksToID(t *testing.T) {
	// 无语义内容（如空 content）时回退到 id 键，保持 24h TTL 语义。
	key := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE:zzz", []byte(`{"id":"ROBOT1.0_x"}`))
	assert.Equal(t, "qq:evt:GROUP_MESSAGE_CREATE:GROUP_MESSAGE_CREATE:zzz", key)

	assert.Equal(t, "qq:evt:INTERACTION_CREATE:uuid-9", makeEventDedupeKey("INTERACTION_CREATE", "uuid-9", []byte(`{}`)))

	assert.Equal(t, "", makeEventDedupeKey("GROUP_MESSAGE_CREATE", "", []byte(`{"id":"ROBOT1.0_x"}`)))
}

func TestMakeEventDedupeKeyHandlesMalformedJSON(t *testing.T) {
	key := makeEventDedupeKey("GROUP_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE:mal", []byte(`{not-json`))
	assert.Equal(t, "qq:evt:GROUP_MESSAGE_CREATE:GROUP_MESSAGE_CREATE:mal", key)
}
